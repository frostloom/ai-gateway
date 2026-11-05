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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

type usageRetryBridge struct {
	*gateway.Billing
	mode  string
	calls atomic.Int32
}

func (b *usageRetryBridge) ReportUsage(ctx context.Context, req *pb.ReportUsageRequest) (*pb.ReportUsageResponse, error) {
	n := b.calls.Add(1)
	if b.mode == "permanent" {
		return nil, status.Error(codes.FailedPrecondition, "injected conflict")
	}
	if b.mode == "reject" {
		return &pb.ReportUsageResponse{Accepted: false}, nil
	}
	if b.mode == "always" {
		return nil, status.Error(codes.Unavailable, "injected persistent outage")
	}
	if n == 1 && b.mode == "timeout" {
		<-ctx.Done()
		return nil, status.Error(codes.DeadlineExceeded, "injected attempt timeout")
	}
	if n == 1 && b.mode == "before" {
		return nil, status.Error(codes.Unavailable, "injected before RPC")
	}
	if n == 1 && b.mode == "after" {
		resp, err := b.Billing.ReportUsage(ctx, req)
		if err != nil {
			return resp, err
		}
		return nil, status.Error(codes.Unavailable, "injected after commit")
	}
	return b.Billing.ReportUsage(ctx, req)
}

type usageRetryRouter struct{ p *gateway.RouteResult }

func (r usageRetryRouter) Route(context.Context, string, ...uint64) (*gateway.RouteResult, error) {
	return r.p, nil
}
func (r usageRetryRouter) Report(context.Context, uint64, bool) {}

func TestUsageRetryRealGatewayStore(t *testing.T) {
	for _, mode := range []string{"before", "after", "timeout", "permanent", "always", "reject"} {
		t.Run(mode, func(t *testing.T) {
			db, r := isolatedDB(t)
			db.Logger = db.Logger.LogMode(logger.Silent)
			s := store.NewStore(db, r)
			if err := s.AutoMigrate(); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			id := fmt.Sprintf("usage-retry-%s-%d", mode, time.Now().UnixNano())
			tenant := store.Tenant{Name: id, InitialQuota: 1000}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
				t.Fatal(err)
			}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
			}))
			defer upstream.Close()
			provider := store.Provider{Name: id, BaseURL: upstream.URL, Models: "[]"}
			if err := db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("retry-test")))
			pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
			go func() { _ = rpc.Serve(listener) }()
			defer rpc.Stop()
			cli, err := gateway.NewBilling(listener.Addr().String(), "retry-test")
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			bridge := &usageRetryBridge{Billing: cli, mode: mode}
			gin.SetMode(gin.ReleaseMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
			c.Set("tenant_id", int64(tenant.ID))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", id)
			handler.Chat(bridge, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: upstream.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
			if w.Code != 200 {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			var bill store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil {
				t.Fatal(err)
			}
			var settle, usage, reverse int64
			for phase, dest := range map[string]*int64{"settle": &settle, "usage": &usage, "reverse": &reverse} {
				if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(dest).Error; err != nil {
					t.Fatal(err)
				}
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "permanent" || mode == "always" || mode == "reject" {
				wantCalls := int32(1)
				if mode == "always" {
					wantCalls = 2
				}
				if bridge.calls.Load() != wantCalls || bill.Status != store.StatusPending || usage != 0 || settle != 0 || reverse != 0 || live != ledger {
					t.Fatalf("permanent calls=%d bill=%s usage=%d settle=%d reverse=%d balance=%d/%d", bridge.calls.Load(), bill.Status, usage, settle, reverse, live, ledger)
				}
				return
			}
			if bridge.calls.Load() != 2 || bill.Status != store.StatusSettled || usage != 1 || settle != 1 || reverse != 0 || live != 994 || ledger != 994 {
				t.Fatalf("mode=%s calls=%d bill=%s usage=%d settle=%d reverse=%d balance=%d/%d", mode, bridge.calls.Load(), bill.Status, usage, settle, reverse, live, ledger)
			}
		})
	}
}

func TestUsageRetryConcurrentRealGatewayStore(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("usage-retry-concurrent-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	provider := store.Provider{Name: id, BaseURL: upstream.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 8)
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
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("retry-concurrent")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "retry-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	const n = 32
	var wg sync.WaitGroup
	results := make(chan string, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			bridge := &usageRetryBridge{Billing: cli, mode: "after"}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
			c.Set("tenant_id", int64(tenants[i%len(tenants)].ID))
			c.Set("api_key_id", int64(1))
			c.Set("request_id", fmt.Sprintf("%s-%d", id, i))
			handler.Chat(bridge, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: upstream.URL}}, slog.New(slog.DiscardHandler), time.Second, true)(c)
			if w.Code != 200 || bridge.calls.Load() != 2 {
				results <- fmt.Sprintf("i=%d http=%d reports=%d", i, w.Code, bridge.calls.Load())
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	for msg := range results {
		t.Error(msg)
	}
	if t.Failed() {
		t.FailNow()
	}
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("%s-%d", id, i)
		var bill store.Bill
		if err := db.Where("request_id=? AND phase=?", rid, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
			t.Fatalf("bill %d status=%s err=%v", i, bill.Status, err)
		}
		for _, phase := range []string{"reserve", "usage", "settle"} {
			var count int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", rid+":"+phase).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("event %d %s count=%d err=%v", i, phase, count, err)
			}
		}
	}
	for _, tenant := range tenants {
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 976 || ledger != 976 {
			t.Fatalf("tenant %d balance=%d ledger=%d err=%v", tenant.ID, live, ledger, err)
		}
	}
	t.Logf("concurrent=%d tenants=%d reports=%d settled=%d balance=976", n, len(tenants), 2*n, n)
}
