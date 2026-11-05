package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
)

func TestReconcilerOnceRealRecoveryAfterUnavailable(t *testing.T) {
	db, redis := isolatedDB(t)
	s := store.NewStore(db, redis)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("once-recovery-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	reserved, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserved.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve=%+v err=%v", reserved, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, BillId: reserved.BillId, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "reconciler")
	build := exec.Command("go", "build", "-o", binary, "./cmd/reconciler")
	build.Dir = filepath.Clean(filepath.Join(cwd, "../.."))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %v: %s", err, out)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatal(err)
	}
	run := func() (string, error) {
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(runCtx, binary, "-once", "-no-rebuild", "-grace=0")
		cmd.Env = append(os.Environ(), "MYSQL_DSN="+os.Getenv("COMPARISON_MYSQL_DSN"), "BILLING_ADDR="+addr, "CONTROL_PLANE_TOKEN=once-test", "RECONCILER_PORT=0")
		out, err := cmd.CombinedOutput()
		if runCtx.Err() != nil {
			t.Fatalf("process timeout %v: %s", runCtx.Err(), out)
		}
		return string(out), err
	}
	out, err := run()
	if err == nil || !strings.Contains(out, "recon settle") {
		t.Fatalf("first err=%v output=%s", err, out)
	}
	var bill store.Bill
	if err := db.Where("id=?", reserved.BillId).First(&bill).Error; err != nil || bill.Status != store.StatusPending || bill.UsageReportedAt == nil {
		t.Fatalf("pending=%+v err=%v", bill, err)
	}
	lis, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("rebind test endpoint: %v", err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("once-test")))
	pb.RegisterBillingServiceServer(rpc, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	out, err = run()
	if err != nil || !strings.Contains(out, "settled=1") {
		t.Fatalf("recovery err=%v output=%s", err, out)
	}
	out, err = run()
	if err != nil || !strings.Contains(out, "settled=0") {
		t.Fatalf("repeat err=%v output=%s", err, out)
	}
	if err := db.Where("id=?", reserved.BillId).First(&bill).Error; err != nil || bill.Status != store.StatusSettled {
		t.Fatalf("settled=%+v err=%v", bill, err)
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != 960 {
		t.Fatalf("balance=%d/%d err=%v", live, ledger, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		var count int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		expected := int64(1)
		if phase == "reverse" {
			expected = 0
		}
		if count != expected {
			t.Fatalf("event %s=%d", phase, count)
		}
	}
	t.Logf("unavailable -> nonzero and pending; recovered -> settled once; repeat -> no-op; balance=%d", live)
}
