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

// Characterizes an unsafe migration, not a test of the old middleware process.
func TestScopedIdempotencyLegacyMigrationCanDuplicate(t *testing.T) {
	if os.Getenv("TEST_TENANT_IDEM") != "1" {
		t.Skip("requires scoped test-only overlay")
	}
	for _, deleteLegacyKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_cache_deleted_%t", deleteLegacyKey), func(t *testing.T) {
			db, r := isolatedDB(t)
			db.Logger = db.Logger.LogMode(logger.Silent)
			s := store.NewStore(db, r)
			if err := s.AutoMigrate(); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			id := fmt.Sprintf("idem-migration-%t-%d", deleteLegacyKey, time.Now().UnixNano())
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
			apiKey := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("%x", hash), Name: "ordinary"}
			if err := db.Create(&apiKey).Error; err != nil {
				t.Fatal(err)
			}
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("tenant-migration-test")))
			pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler), server.RequireReserveV2()))
			go func() { _ = rpc.Serve(lis) }()
			defer rpc.Stop()
			cli, err := gateway.NewBilling(lis.Addr().String(), "tenant-migration-test")
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
			candidate := gin.New()
			candidate.Use(middleware.APIKey(cli))
			candidate.Use(middleware.Idempotency(r))
			candidate.POST("/", handler.Chat(cli, usageRetryRouter{p: &gateway.RouteResult{ProviderID: provider.ID, BaseURL: up.URL}}, slog.New(slog.DiscardHandler), time.Second, true))
			request := func(e *gin.Engine) int {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, id)))
				req.Header.Set("Authorization", "Bearer "+rawKey)
				req.Header.Set("Idempotency-Key", shared)
				w := httptest.NewRecorder()
				e.ServeHTTP(w, req)
				return w.Code
			}
			oldStatus := request(legacy)
			if deleteLegacyKey {
				if err := r.Del(ctx, legacyID).Err(); err != nil {
					t.Fatal(err)
				}
			}
			newStatus := request(candidate)
			if oldStatus != 200 || newStatus != 200 || calls.Load() != 2 {
				t.Fatalf("migration fixture: old/new=%d/%d upstream=%d", oldStatus, newStatus, calls.Load())
			}
			var bills []store.Bill
			if err := db.Where("tenant_id=? AND phase=?", tenant.ID, store.PhaseReserve).Order("id").Find(&bills).Error; err != nil || len(bills) != 2 {
				t.Fatalf("bills=%d err=%v", len(bills), err)
			}
			if bills[0].RequestID != legacyID || bills[1].RequestID == legacyID || bills[0].Status != store.StatusSettled || bills[1].Status != store.StatusSettled {
				t.Fatalf("bills %+v", bills)
			}
			live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || live != 988 || ledger != 988 {
				t.Fatalf("balance=%d/%d err=%v", live, ledger, err)
			}
			for _, bill := range bills {
				for _, phase := range []string{"reserve", "usage", "settle"} {
					var n int64
					if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", bill.RequestID+":"+phase).Count(&n).Error; err != nil || n != 1 {
						t.Fatalf("event=%s count=%d err=%v", phase, n, err)
					}
				}
			}
			t.Logf("legacy_cache_deleted=%t status=200/200 upstream=2 settled_bills=2 balance=988", deleteLegacyKey)
		})
	}
}
