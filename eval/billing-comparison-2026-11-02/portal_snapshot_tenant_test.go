package comparison

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

func TestPortalSnapshotTenantIsolation(t *testing.T) {
	if os.Getenv("TEST_PORTAL_SNAPSHOT_TENANT") != "1" {
		t.Skip("isolated tenant test")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("portal-tenant-%d", time.Now().UnixNano())
	a := store.Tenant{Name: tag + "-a", InitialQuota: 600}
	b := store.Tenant{Name: tag + "-b", InitialQuota: 1000}
	for _, tenant := range []*store.Tenant{&a, &b} {
		if err := db.Create(tenant).Error; err != nil {
			t.Fatal(err)
		}
		if err := redis.Set(t.Context(), store.BalanceKey(tenant.ID), tenant.InitialQuota, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	hash := sha256.Sum256([]byte(tag))
	key := store.APIKey{TenantID: a.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: tag}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("tenant-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	billing, err := gatewayclient.NewBilling(lis.Addr().String(), "tenant-test")
	if err != nil {
		t.Fatal(err)
	}
	defer billing.Close()
	adminServer := httptest.NewServer(admin.Handler(st, slog.New(slog.DiscardHandler), "tenant-test"))
	defer adminServer.Close()
	gatewayhandler.SetControlToken("tenant-test")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/portal")
	group.Use(middleware.APIKey(billing))
	gatewayhandler.RegisterPortal(group, adminServer.URL, "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/portal/me?tenant_id=%d", b.ID), nil)
	request.Header.Set("Authorization", "Bearer "+tag)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	var view store.MeView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || response.Code != 200 || view.Balance != 600 || view.LedgerBalance != 600 {
		t.Fatalf("status=%d view=%+v err=%v body=%s", response.Code, view, err, response.Body.String())
	}
	bad := httptest.NewRequest(http.MethodGet, "/portal/me", nil)
	bad.Header.Set("Authorization", "Bearer invalid")
	denied := httptest.NewRecorder()
	engine.ServeHTTP(denied, bad)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("invalid credential status=%d", denied.Code)
	}
	t.Log("tenant A balance=600 despite tenant B query parameter, invalid API key denied")
}
