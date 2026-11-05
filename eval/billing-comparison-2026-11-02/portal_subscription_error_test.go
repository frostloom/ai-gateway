package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	gatewayhandler "github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPortalSnapshotRejectsSubscriptionQueryError(t *testing.T) {
	if os.Getenv("TEST_PORTAL_SUB_ERROR") != "1" {
		t.Skip("isolated injection only")
	}
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("portal-sub-error-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	plan := store.Plan{Name: tag, PlanType: store.PlanTypeRecurring, PriceMoney: 1, ValidityDays: 30, RefreshHours: 5, TierQuota: `{"文本":300}`}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := st.SubscribePlan(ctx, tenant.ID, plan.ID); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(tag))
	if err := db.Create(&store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: tag}).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("synthetic subscription query unavailable")
	if err := db.Callback().Query().Before("gorm:query").Register("test:portal_subscription_error", func(tx *gorm.DB) {
		if tx.Statement.Table == "subscriptions" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:portal_subscription_error")
	view, err := st.MeOverview(ctx, tenant.ID)
	if err == nil {
		t.Fatalf("subscription query failure returned success: view=%+v", view)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("sub-error")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(lis.Addr().String(), "sub-error")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	adminServer := httptest.NewServer(admin.Handler(st, slog.New(slog.DiscardHandler), "sub-error"))
	defer adminServer.Close()
	gatewayhandler.SetControlToken("sub-error")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/portal")
	group.Use(middleware.APIKey(billing))
	gatewayhandler.RegisterPortal(group, adminServer.URL, "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	req := httptest.NewRequest(http.MethodGet, "/portal/me", nil)
	req.Header.Set("Authorization", "Bearer "+tag)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("portal status=%d body=%s", response.Code, response.Body.String())
	}
	t.Log("subscription query error propagated through authenticated portal as HTTP 500")
}
