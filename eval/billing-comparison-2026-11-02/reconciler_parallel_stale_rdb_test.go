package comparison

import (
	"context"
	"fmt"
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

func TestParallelSweepOldRDBStaleBalance(t *testing.T) {
	if os.Getenv("TEST_RECON_STALE_RDB") != "1" {
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
	tag := fmt.Sprintf("old-rdb-%d", time.Now().UnixNano())
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
	for _, tenant := range tenants {
		live, err := st.GetBalance(ctx, tenant.ID)
		ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || e != nil || live != 1000 || ledger != 800 {
			t.Fatalf("expected stale projection live=%d ledger=%d errors=%v/%v", live, ledger, err, e)
		}
	}
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
	t.Log("old RDB: three stale reads=1000 while ledger=800; 900-unit reservation denied and corrected first projection; six bills settled; three balances/ledgers=920")
}
