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
	"os"
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

// A lookup followed by a scoped reserve is not an atomic migration fence.
func TestScopedIdempotencyLegacyLookupMixedWriterRace(t *testing.T) {
	if os.Getenv("TEST_TENANT_IDEM") != "1" {
		t.Skip("requires scoped test overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("idem-mixed-%d", time.Now().UnixNano())
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
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer up.Close()
	provider := store.Provider{Name: id, BaseURL: up.URL, Models: "[]"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	rawKey := id + "-api"
	hash := sha256.Sum256([]byte(rawKey))
	key := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("%x", hash), Name: "ordinary"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("tenant-mixed-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	cli, err := gateway.NewBilling(lis.Addr().String(), "tenant-mixed-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gin.SetMode(gin.ReleaseMode)
	shared := id + "-client"
	legacyID := "idem:" + shared
	legacy := gin.New()
	legacy.Use(middleware.APIKey(cli))
	legacy.Use(func(c *gin.Context) {
		ok, err := r.SetNX(c.Request.Context(), legacyID, "old-fixture-owner", 5*time.Minute).Result()
		if err != nil || !ok {
			c.AbortWithStatus(409)
			return
		}
		c.Set("request_id", legacyID)
		c.Next()
	})
	legacy.POST("/", handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), time.Second, true))
	checked := make(chan struct{})
	resume := make(chan struct{})
	var emptyRead atomic.Bool
	candidate := gin.New()
	candidate.Use(middleware.APIKey(cli))
	candidate.Use(middleware.Idempotency(r))
	candidate.Use(func(c *gin.Context) {
		var n int64
		if err := db.WithContext(c.Request.Context()).Model(&store.Bill{}).Where("request_id=? AND phase=?", legacyID, store.PhaseReserve).Count(&n).Error; err != nil || n != 0 {
			c.AbortWithStatus(409)
			close(checked)
			return
		}
		emptyRead.Store(true)
		close(checked)
		<-resume
		c.Next()
	})
	candidate.POST("/", handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), time.Second, true))
	request := func(e *gin.Engine) int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("Idempotency-Key", shared)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	result := make(chan int, 1)
	go func() { result <- request(candidate) }()
	select {
	case <-checked:
	case <-time.After(10 * time.Second):
		close(resume)
		t.Fatal("candidate lookup did not finish")
	}
	if !emptyRead.Load() {
		close(resume)
		t.Fatal("legacy lookup did not observe absence")
	}
	oldStatus := request(legacy)
	close(resume)
	var newStatus int
	select {
	case newStatus = <-result:
	case <-time.After(15 * time.Second):
		t.Fatal("candidate did not finish")
	}
	if oldStatus != 200 || newStatus != 200 || calls.Load() != 2 {
		t.Fatalf("old/new=%d/%d upstream=%d", oldStatus, newStatus, calls.Load())
	}
	var bills []store.Bill
	if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Order("id").Find(&bills).Error; err != nil || len(bills) != 2 {
		t.Fatalf("bills=%d err=%v", len(bills), err)
	}
	for _, b := range bills {
		if b.Status != store.StatusSettled {
			t.Fatalf("bill status %s", b.Status)
		}
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 988 || ledger != 988 {
		t.Fatalf("balance=%d/%d err=%v", live, ledger, err)
	}
	t.Log("legacy lookup saw no row; legacy writer completed before scoped reserve; upstream=2 settled=2 balance=988")
}
