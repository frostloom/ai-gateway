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

func TestGatewayUnknownDefaultRetainsRealReserve(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
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
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, failure := range []string{"disconnect", "503"} {
			t.Run(fmt.Sprintf("stream=%v/%s", stream, failure), func(t *testing.T) {
				id := fmt.Sprintf("unknown-default-%d", time.Now().UnixNano())
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
				var first, second atomic.Int32
				primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if _, err := io.Copy(io.Discard, req.Body); err != nil {
						t.Errorf("read body: %v", err)
						return
					}
					first.Add(1)
					if failure == "503" {
						w.WriteHeader(503)
						return
					}
					h, ok := w.(http.Hijacker)
					if !ok {
						t.Error("no hijacker")
						return
					}
					conn, _, err := h.Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				}))
				defer primary.Close()
				secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					second.Add(1)
					_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
				}))
				defer secondary.Close()
				p1 := store.Provider{Name: id + "-1", BaseURL: primary.URL, Models: `[]`, Weight: 1}
				p2 := store.Provider{Name: id + "-2", BaseURL: secondary.URL, Models: `[]`, Weight: 1}
				for _, p := range []*store.Provider{&p1, &p2} {
					if err := db.Create(p).Error; err != nil {
						t.Fatal(err)
					}
				}
				route := &sequentialExecutionRouter{routes: []*gatewayclient.RouteResult{{ProviderID: p1.ID, BaseURL: primary.URL}, {ProviderID: p2.ID, BaseURL: secondary.URL}}}
				rid := id + "-request"
				engine := gin.New()
				engine.POST("/v1/chat/completions", func(c *gin.Context) {
					c.Set("tenant_id", int64(tenant.ID))
					c.Set("api_key_id", int64(1))
					c.Set("request_id", rid)
				}, handler.Chat(billing, route, slog.New(slog.DiscardHandler), time.Second))
				gateway := httptest.NewServer(engine)
				defer gateway.Close()
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":10,"stream":%t}`, id, stream)
				resp, err := (&http.Client{Timeout: 8 * time.Second}).Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != 502 || first.Load() != 1 || second.Load() != 0 || route.next != 1 {
					t.Fatalf("status=%d primary=%d secondary=%d routes=%d", resp.StatusCode, first.Load(), second.Load(), route.next)
				}
				var reserve store.Bill
				if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).Take(&reserve).Error; err != nil || reserve.Status != store.StatusPending || reserve.UsageReportedAt != nil || reserve.PreQuota <= 0 {
					t.Fatalf("reserve=%+v err=%v", reserve, err)
				}
				live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
				if err != nil {
					t.Fatal(err)
				}
				ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
				if err != nil || ledger != live || live != 1000-reserve.PreQuota {
					t.Fatalf("redis=%d ledger=%d pre=%d err=%v", live, ledger, reserve.PreQuota, err)
				}
				var bills int64
				if err := db.Model(&store.Bill{}).Where("request_id=?", rid).Count(&bills).Error; err != nil || bills != 1 {
					t.Fatalf("bills=%d err=%v", bills, err)
				}
				for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
					var n int64
					want := int64(0)
					if phase == "reserve" {
						want = 1
					}
					if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&n).Error; err != nil || n != want {
						t.Fatalf("phase=%s n=%d err=%v", phase, n, err)
					}
				}
				t.Logf("status=502 primary=1 secondary=0 pending=1 pre=%d redis=ledger=%d reserve_only=true", reserve.PreQuota, live)
			})
		}
	}
}
