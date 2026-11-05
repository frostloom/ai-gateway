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

func TestParallelSweepRedisOutageRecovery(t *testing.T) {
	if os.Getenv("TEST_RECON_REDIS_OUTAGE") != "1" {
		t.Skip("isolated Redis outage only")
	}
	container := os.Getenv("COMPARISON_REDIS_CONTAINER")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") {
		t.Fatal("owned Redis container required")
	}
	address := os.Getenv("COMPARISON_REDIS_ADDR")
	if !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatal("loopback Redis required")
	}
	db, redis := isolatedDB(t)
	st := store.NewStore(db, redis)
	ctx := context.Background()
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("parallel-redis-%d", time.Now().UnixNano())
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("outage-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(st, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "outage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
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
	outageCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	runner := reconciler.New(db, pb.NewBillingServiceClient(conn), slog.New(slog.DiscardHandler), time.Minute)
	stats, err := runner.SweepOnce(outageCtx)
	cancel()
	if err != nil || stats.Scanned != 6 || stats.SettleErrors != 6 || stats.Settled != 0 {
		t.Fatalf("outage stats=%+v err=%v", stats, err)
	}
	for _, id := range ids {
		var b store.Bill
		if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).Take(&b).Error; err != nil || b.Status != store.StatusPending || b.UsageReportedAt == nil {
			t.Fatalf("outage bill=%+v err=%v", b, err)
		}
	}
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
	retryCtx, retryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer retryCancel()
	after, err := runner.SweepOnce(retryCtx)
	if err != nil || after.Settled != 6 || after.SettleErrors != 0 {
		t.Fatalf("recovery stats=%+v err=%v", after, err)
	}
	for _, id := range ids {
		var n int64
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if phase == "reverse" {
				want = 0
			}
			if n != want {
				t.Fatalf("id=%s phase=%s count=%d", id, phase, n)
			}
		}
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("id=%s settle=%d err=%v", id, n, err)
		}
	}
	for _, tenant := range tenants {
		live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != ledger || live != 920 {
			t.Fatalf("tenant=%d live=%d ledger=%d err=%v", tenant.ID, live, ledger, err)
		}
	}
	t.Logf("Redis killed: errors=%d pending=6; restarted: settled=%d; three balances/ledgers=920", stats.SettleErrors, after.Settled)
}
