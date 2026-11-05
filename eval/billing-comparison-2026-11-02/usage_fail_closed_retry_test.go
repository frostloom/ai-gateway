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

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

type unavailableUsageBilling struct{ *gateway.Billing }

func (b unavailableUsageBilling) ReportUsage(context.Context, *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	return nil, status.Error(codes.Unavailable, "injected usage storage failure")
}

func TestUsageFailureErrorResponseRetrySemantics(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("usage-error-retry-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var modelCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 2)
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", id, i), InitialQuota: 1000}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("usage-error-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "usage-error-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	makeRouter := func(tenant uint64) *gin.Engine {
		e := gin.New()
		e.Use(func(c *gin.Context) { c.Set("tenant_id", int64(tenant)); c.Set("api_key_id", int64(1)); c.Next() })
		e.Use(middleware.Idempotency(r))
		e.POST("/", handler.Chat(unavailableUsageBilling{cli}, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), time.Second, true))
		return e
	}
	request := func(e *gin.Engine, key string) int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	keyed := makeRouter(tenants[0].ID)
	idemKey := id + "-key"
	first := request(keyed, idemKey)
	second := request(keyed, idemKey)
	if err := r.Del(ctx, "idem:"+idemKey).Err(); err != nil {
		t.Fatal(err)
	}
	third := request(keyed, idemKey)
	if first != 503 || second != 409 || third != 409 || modelCalls.Load() != 1 {
		t.Fatalf("same key: status=%d/%d/%d upstream=%d", first, second, third, modelCalls.Load())
	}
	unkeyed := makeRouter(tenants[1].ID)
	fourth := request(unkeyed, "")
	fifth := request(unkeyed, "")
	if fourth != 503 || fifth != 503 || modelCalls.Load() != 3 {
		t.Fatalf("no key retry: status=%d/%d total_upstream=%d", fourth, fifth, modelCalls.Load())
	}
	for idx, tenant := range tenants {
		var bills []store.Bill
		if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Find(&bills).Error; err != nil {
			t.Fatal(err)
		}
		want := 1
		if idx == 1 {
			want = 2
		}
		if len(bills) != want {
			t.Fatalf("tenant %d reserves=%d want=%d", tenant.ID, len(bills), want)
		}
		for _, bill := range bills {
			if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
				t.Fatalf("bill %+v", bill)
			}
			for _, phase := range []string{"usage", "settle", "reverse"} {
				var n int64
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", bill.RequestID+":"+phase).Count(&n).Error; err != nil || n != 0 {
					t.Fatalf("unexpected event %s=%d err=%v", phase, n, err)
				}
			}
		}
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != ledger {
			t.Fatalf("tenant balance=%d/%d err=%v", live, ledger, err)
		}
	}
	t.Logf("same_key=503,409,409 upstream=1; no_key=503,503 upstream=2; reserves=1,2 pending_no_usage=3")
}
