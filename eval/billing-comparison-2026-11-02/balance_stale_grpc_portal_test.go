package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	gatewayclient "github.com/frostloom/ai-gateway/internal/gateway/client"
	gatewayhandler "github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestOldRDBBalanceGRPCAndPortalPaths(t *testing.T) {
	if os.Getenv("TEST_BALANCE_PATH_RDB") != "1" {
		t.Skip("isolated stale RDB only")
	}
	container := os.Getenv("COMPARISON_REDIS_CONTAINER")
	address := os.Getenv("COMPARISON_REDIS_ADDR")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") || !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatal("owned loopback Redis required")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("balance-path-rdb-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: tag, Name: tag, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 3)
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", tag, i), InitialQuota: 1000}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := redis.Set(ctx, store.BalanceKey(tenants[i].ID), 1000, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	portalKey := tag + "-portal-key"
	hash := sha256.Sum256([]byte(portalKey))
	key := store.APIKey{TenantID: tenants[1].ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: "synthetic", Name: tag}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.ConfigSet(ctx, "save", "").Err(); err != nil {
		t.Fatal(err)
	}
	config, err := redis.ConfigGet(ctx, "appendonly").Result()
	if err != nil || config["appendonly"] != "no" {
		t.Fatalf("appendonly config=%v err=%v", config, err)
	}
	if err := redis.Save(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%d", tag, i)
		res, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: ids[i], TenantId: int64(tenants[i%3].ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 84})
		if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatalf("reserve=%v err=%v", res, err)
		}
		if err := st.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: ids[i], BillId: res.BillId, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&store.Bill{}).Where("id=?", res.BillId).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tenant := range tenants {
		live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || e != nil || live != 800 || ledger != 800 {
			t.Fatalf("before restart live=%d ledger=%d errors=%v/%v", live, ledger, err, e)
		}
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
	action("start")
	started = true
	port, err := exec.Command("docker", "port", container, "6379/tcp").Output()
	if err != nil || strings.TrimSpace(string(port)) != address {
		t.Fatalf("port=%q expected=%q err=%v", port, address, err)
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
		t.Fatal("Redis did not recover")
	}
	expectedRead := int64(1000)
	if os.Getenv("TEST_MYSQL_READ_CURRENT") == "1" {
		expectedRead = 800
	}
	for _, tenant := range tenants {
		live, err := st.GetBalance(ctx, tenant.ID)
		raw, re := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || re != nil || e != nil || live != expectedRead || raw != 1000 || ledger != 800 {
			t.Fatalf("expected read=%d got=%d raw=%d ledger=%d errors=%v/%v/%v", expectedRead, live, raw, ledger, err, re, e)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("rdb-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "rdb-test")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	balance, err := pb.NewBillingServiceClient(conn).GetBalance(ctx, &pb.GetBalanceRequest{TenantId: int64(tenants[0].ID)})
	if err != nil || balance.Balance != expectedRead {
		t.Fatalf("gRPC balance=%+v err=%v", balance, err)
	}
	billingHTTP := httptest.NewServer(admin.Handler(st, slog.New(slog.DiscardHandler), "rdb-test"))
	defer billingHTTP.Close()
	billingClient, err := gatewayclient.NewBilling(listener.Addr().String(), "rdb-test")
	if err != nil {
		t.Fatal(err)
	}
	defer billingClient.Close()
	gatewayhandler.SetControlToken("rdb-test")
	defer gatewayhandler.SetControlToken("")
	engine := gin.New()
	group := engine.Group("/portal")
	group.Use(middleware.APIKey(billingClient))
	gatewayhandler.RegisterPortal(group, billingHTTP.URL, "http://127.0.0.1:1", slog.New(slog.DiscardHandler))
	send := func(key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/portal/me?tenant_id=%d", tenants[0].ID), nil)
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	if res := send(""); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized portal=%d", res.Code)
	}
	response := send(portalKey)
	var view store.MeView
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil || view.Balance != 800 || view.LedgerBalance != 800 || !view.Consistent {
		t.Fatalf("portal status=%d body=%s", response.Code, response.Body.String())
	}
	raw, err := redis.Get(ctx, store.BalanceKey(tenants[1].ID)).Int64()
	if err != nil || raw != 800 {
		t.Fatalf("portal projection=%d err=%v", raw, err)
	}
	t.Logf("gRPC balance=%d raw=1000 ledger=800; authenticated portal for second tenant=800 and repaired its projection", balance.Balance)

	// One account attempts a 900-unit reservation against a ledger balance
	// of 800 while the restored projection still says 1000.
	overspendID := tag + "-overspend"
	denied, err := st.Reserve(ctx, &pb.ReserveRequest{RequestId: overspendID, TenantId: int64(tenants[0].ID), ApiKeyId: 1, Model: tag, EstPromptTokens: 750})
	if err != nil || denied == nil || denied.Code != pb.ReserveCode_RESERVE_INSUFFICIENT {
		t.Fatalf("expected insufficient after stale read: response=%+v err=%v", denied, err)
	}
	var deniedRows, deniedEvents int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", overspendID).Count(&deniedRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", overspendID+":reserve").Count(&deniedEvents).Error; err != nil {
		t.Fatal(err)
	}
	corrected, err := st.GetBalance(ctx, tenants[0].ID)
	if err != nil || corrected != 800 || deniedRows != 0 || deniedEvents != 0 {
		t.Fatalf("denied rows=%d events=%d corrected balance=%d err=%v", deniedRows, deniedEvents, corrected, err)
	}

	sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stats, err := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), time.Minute).SweepOnce(sweepCtx)
	if err != nil || stats.Settled != 6 || stats.SettleErrors != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	for _, id := range ids {
		var count int64
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if phase == "reverse" {
				want = 0
			}
			if count != want {
				t.Fatalf("id=%s phase=%s count=%d", id, phase, count)
			}
		}
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("settle rows=%d err=%v", count, err)
		}
	}
	for _, tenant := range tenants {
		live, err := st.GetBalance(ctx, tenant.ID)
		ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || e != nil || live != 920 || ledger != 920 {
			t.Fatalf("after sweep live=%d ledger=%d errors=%v/%v", live, ledger, err, e)
		}
	}
	t.Log("RDB path test: six bills settled once, three balances/ledgers=920")
}
