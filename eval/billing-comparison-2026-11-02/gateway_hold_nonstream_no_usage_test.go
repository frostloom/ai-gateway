package comparison

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	billingserver "github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

func TestGatewayHoldNonstreamMissingUsage(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("nonstream-hold-no-usage-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: `[]`}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, req.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hello"}}]}`)
	}))
	defer mock.Close()
	provider := store.Provider{Name: id, BaseURL: mock.URL, Models: `[]`, Weight: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	pb.RegisterBillingServiceServer(rpc, billingserver.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	route := &sequentialExecutionRouter{routes: []*gatewayclient.RouteResult{{ProviderID: provider.ID, BaseURL: mock.URL}}}
	rid := id + "-request"
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("tenant_id", int64(tenant.ID))
		c.Set("api_key_id", int64(1))
		c.Set("request_id", rid)
	}, handler.Chat(billing, route, slog.New(slog.DiscardHandler), time.Second, true))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":false}`, id)
	response, err := (&http.Client{Timeout: 8 * time.Second}).Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || calls.Load() != 1 || !strings.Contains(string(data), "hello") {
		t.Fatalf("status=%d calls=%d body=%q", response.StatusCode, calls.Load(), data)
	}
	var reserve store.Bill
	if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).Take(&reserve).Error; err != nil || reserve.Status != store.StatusPending || reserve.UsageReportedAt != nil {
		t.Fatal(reserve, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != live {
		t.Fatal(live, ledger, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		var count int64
		want := int64(0)
		if phase == "reserve" {
			want = 1
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&count).Error; err != nil || count != want {
			t.Fatalf("phase=%s count=%d err=%v", phase, count, err)
		}
	}
	t.Logf("http=200 mock_calls=%d bill=pending usage_absent=true balance=%d ledger=%d reserve_only=true", calls.Load(), live, ledger)
}
