package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
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

func TestBalancePathsRedisOutage(t *testing.T) {
	if os.Getenv("TEST_BALANCE_PATH_OUTAGE") != "1" {
		t.Skip("isolated outage only")
	}
	container := os.Getenv("COMPARISON_REDIS_CONTAINER")
	addr := os.Getenv("COMPARISON_REDIS_ADDR")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatal("owned loopback Redis required")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("balance-outage-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(tag))
	key := store.APIKey{TenantID: tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: tag}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("outage-read")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(lis.Addr().String(), "outage-read")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	billingHTTP := httptest.NewServer(admin.Handler(st, slog.New(slog.DiscardHandler), "outage-read"))
	defer billingHTTP.Close()
	billingClient, err := gatewayclient.NewBilling(lis.Addr().String(), "outage-read")
	if err != nil {
		t.Fatal(err)
	}
	defer billingClient.Close()
	gatewayhandler.SetControlToken("outage-read")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/portal")
	group.Use(middleware.APIKey(billingClient))
	gatewayhandler.RegisterPortal(group, billingHTTP.URL, "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	hc := &http.Client{Timeout: 15 * time.Second}
	defer hc.CloseIdleConnections()
	portal := func() (int, *store.MeView, error) {
		req, err := http.NewRequest(http.MethodGet, gateway.URL+"/portal/me", nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tag)
		response, err := hc.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		var view store.MeView
		err = json.NewDecoder(response.Body).Decode(&view)
		return response.StatusCode, &view, err
	}
	action := func(arg string) {
		t.Helper()
		out, err := exec.Command("docker", arg, container).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v %s", arg, err, out)
		}
	}
	action("kill")
	started := false
	defer func() {
		if !started {
			_ = exec.Command("docker", "start", container).Run()
		}
	}()
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	balance, readErr := pb.NewBillingServiceClient(conn).GetBalance(readCtx, &pb.GetBalanceRequest{TenantId: int64(tenant.ID)})
	cancel()
	candidate := os.Getenv("TEST_MYSQL_READ_CURRENT") == "1"
	if candidate {
		if readErr != nil || balance == nil || balance.Balance != 1000 {
			t.Fatalf("snapshot while Redis down=%+v err=%v", balance, readErr)
		}
	} else if readErr == nil {
		t.Fatalf("expected Redis-based gRPC failure, got %+v", balance)
	}
	code, _, portalErr := portal()
	if portalErr != nil || code == http.StatusOK {
		t.Fatalf("expected portal HTTP error, status=%d err=%v", code, portalErr)
	}
	outageCode := code
	action("start")
	started = true
	port, err := exec.Command("docker", "port", container, "6379/tcp").Output()
	if err != nil || strings.TrimSpace(string(port)) != addr {
		t.Fatalf("port=%q expected=%q err=%v", port, addr, err)
	}
	ready := false
	for i := 0; i < 60; i++ {
		ping, cancel := context.WithTimeout(ctx, time.Second)
		err := redis.Ping(ping).Err()
		cancel()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatal("Redis not ready")
	}
	code, view, err := portal()
	if err != nil || code != http.StatusOK || view.Balance != 1000 || view.LedgerBalance != 1000 {
		t.Fatalf("portal recovery status=%d view=%+v err=%v", code, view, err)
	}
	t.Logf("mysql_snapshot=%v: gRPC error during outage=%v; portal outage HTTP=%d after restart HTTP=200 balance=1000", candidate, readErr != nil, outageCode)
}
