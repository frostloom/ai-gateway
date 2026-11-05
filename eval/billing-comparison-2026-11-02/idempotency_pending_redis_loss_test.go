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

func TestIdempotencyPendingAfterRedisLoss(t *testing.T) {
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
	started := make(chan struct{})
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if upstreamCalls.Add(1) == 1 {
			close(started)
			<-release
		}
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
	e.POST("/", handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), 10*time.Second, true))
	shared := id + "-shared"
	request := func(content string) int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}]}`, id, content)))
		req.Header.Set("Authorization", "Bearer "+rawKeys[0])
		req.Header.Set("Idempotency-Key", shared)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	first := make(chan int, 1)
	go func() { first <- request("hello") }()
	select {
	case <-started:
	case <-time.After(8 * time.Second):
		close(release)
		t.Fatal("upstream not reached")
	}
	var bill store.Bill
	if err := db.Where("tenant_id=? AND phase=?", tenants[0].ID, store.PhaseReserve).First(&bill).Error; err != nil {
		close(release)
		t.Fatal(err)
	}
	if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
		close(release)
		t.Fatalf("expected pending without usage: %+v", bill)
	}
	if err := r.Del(ctx, bill.RequestID).Err(); err != nil {
		close(release)
		t.Fatal(err)
	}
	same := request("hello")
	if err := r.Del(ctx, bill.RequestID).Err(); err != nil {
		close(release)
		t.Fatal(err)
	}
	different := request("different payload")
	close(release)
	var original int
	select {
	case original = <-first:
	case <-time.After(12 * time.Second):
		t.Fatal("first request did not finish")
	}
	if original != 200 || same != 409 || different != 409 || upstreamCalls.Load() != 1 {
		t.Fatalf("original=%d same=%d different=%d upstream=%d", original, same, different, upstreamCalls.Load())
	}
	var n int64
	if err := db.Model(&store.Bill{}).Where("tenant_id=? AND phase=?", tenants[0].ID, store.PhaseReserve).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("reserve=%d err=%v", n, err)
	}
	if err := db.First(&bill, bill.ID).Error; err != nil || bill.Status != store.StatusSettled {
		t.Fatalf("bill=%+v err=%v", bill, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenants[0].ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenants[0].ID)
	if err != nil || live != 994 || ledger != 994 {
		t.Fatalf("balance=%d/%d err=%v", live, ledger, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle"} {
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", bill.RequestID+":"+phase).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("event %s=%d err=%v", phase, n, err)
		}
	}
	t.Logf("pending key deleted twice: original=%d same=%d different=%d upstream=%d balance=%d", original, same, different, upstreamCalls.Load(), live)
}
