package comparison

import (
	"context"
	"crypto/sha256"
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
	"gorm.io/gorm/logger"
)

func TestTenantScopedIdempotencyRealBilling(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("tenant-idem-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var upstreamCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 2)
	rawKeys := make([]string, 2)
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", id, i), InitialQuota: 1000}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
		rawKeys[i] = fmt.Sprintf("%s-key-%d", id, i)
		hash := sha256.Sum256([]byte(rawKeys[i]))
		key := store.APIKey{TenantID: tenants[i].ID, KeyHash: fmt.Sprintf("%x", hash), Name: "ordinary"}
		if err := db.Create(&key).Error; err != nil {
			t.Fatal(err)
		}
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("tenant-idem-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "tenant-idem-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(middleware.APIKey(cli))
	e.Use(middleware.Idempotency(r))
	e.POST("/", handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), time.Second, true))
	shared := id + "-shared"
	request := func(i int) int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
		req.Header.Set("Authorization", "Bearer "+rawKeys[i])
		req.Header.Set("Idempotency-Key", shared)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	first, second := request(0), request(1)
	if first != 200 || second != 200 {
		t.Fatalf("distinct tenants with same key: status=%d/%d upstream=%d", first, second, upstreamCalls.Load())
	}
	if got := request(0); got != 409 {
		t.Fatalf("same tenant duplicate: %d", got)
	}
	bills := make([]store.Bill, 2)
	for i, tenant := range tenants {
		if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).First(&bills[i]).Error; err != nil {
			t.Fatal(err)
		}
		if bills[i].Status != store.StatusSettled {
			t.Fatalf("status=%s", bills[i].Status)
		}
		var n int64
		if err := db.Model(&store.Bill{}).Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("reserve=%d err=%v", n, err)
		}
	}
	if bills[0].RequestID == bills[1].RequestID || len(bills[0].RequestID) > 64 || len(bills[1].RequestID) > 64 {
		t.Fatalf("request ids not distinct/bounded: %q / %q", bills[0].RequestID, bills[1].RequestID)
	}
	if err := r.Del(ctx, bills[0].RequestID).Err(); err != nil {
		t.Fatal(err)
	}
	if got := request(0); got != 409 {
		t.Fatalf("after deleting Redis scoped key: %d", got)
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls=%d", upstreamCalls.Load())
	}
	for _, tenant := range tenants {
		live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 994 || ledger != 994 {
			t.Fatalf("balance %d/%d err=%v", live, ledger, err)
		}
	}
	for _, bill := range bills {
		for _, phase := range []string{"reserve", "usage", "settle"} {
			var n int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", bill.RequestID+":"+phase).Count(&n).Error; err != nil || n != 1 {
				t.Fatalf("event %s %s=%d err=%v", bill.RequestID, phase, n, err)
			}
		}
	}
	t.Log("same client key: two tenants each succeed and settle; same-tenant replay rejected before and after Redis deletion")
}
