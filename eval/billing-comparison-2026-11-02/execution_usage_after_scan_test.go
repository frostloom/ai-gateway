package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm/logger"
)

type usageThenStateBridge struct{ *stateSweepBridge }

func (b usageThenStateBridge) Reverse(ctx context.Context, req *pb.ReverseRequest, opts ...grpc.CallOption) (*pb.ReverseResponse, error) {
	if err := b.s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 40}); err != nil {
		return nil, err
	}
	return b.stateSweepBridge.Reverse(ctx, req, opts...)
}
func TestExecutionUsageCommittedAfterScan(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STATE") != "1" {
		t.Skip("requires test overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	candidate, ok := any(s).(experimentalAtomicReserve)
	if !ok {
		t.Fatal("candidate missing")
	}
	id := fmt.Sprintf("state-usage-scan-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("synthetic-owner"))
	reserve, err := candidate.ReserveWithExecutionIntent(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}, hex.EncodeToString(digest[:]), 30*time.Second)
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", reserve.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("test-token")))
	server.RegisterService(&executionDesc, &capabilityServer{db: db, recoverySecret: "synthetic-recovery"})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, err := controlplane.Dial(listener.Addr().String(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var permit wrapperspb.Int64Value
	if err := conn.Invoke(ctx, "/eval.execution.v1.Execution/Transition", wrapperspb.String(id+"|forwarding|synthetic-owner"), &permit); err != nil || permit.Value != 1 {
		t.Fatal(permit.Value, err)
	}
	if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id=?", reserve.BillId).Error; err != nil {
		t.Fatal(err)
	}
	bridge := &stateSweepBridge{db: db, conn: conn, s: s}
	first, err := reconciler.New(db, usageThenStateBridge{bridge}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse()).SweepOnce(ctx)
	if err != nil || first.Scanned != 1 || first.Reversed != 0 {
		t.Fatal(first, err)
	}
	var intermediate store.Bill
	if err := db.First(&intermediate, reserve.BillId).Error; err != nil || intermediate.Status != store.StatusPending || intermediate.UsageReportedAt == nil || intermediate.PromptTokens == nil || *intermediate.PromptTokens != 40 {
		t.Fatal(intermediate, err)
	}
	// The second real scan sees the committed usage and reaches Store.Settle.
	// The test-only bridge must also implement Settle for this path.
	second, err := reconciler.New(db, usageSettlementBridge{bridge}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse()).SweepOnce(ctx)
	if err != nil || second.Scanned != 1 || second.Settled != 1 {
		t.Fatal(second, err)
	}
	var bill store.Bill
	if err := db.First(&bill, reserve.BillId).Error; err != nil || bill.Status != store.StatusSettled {
		t.Fatal(bill, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != 960 || live != ledger {
		t.Fatal(live, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle", id + ":reverse"}).Count(&events).Error; err != nil || events != 3 {
		t.Fatal(events, err)
	}
	for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
		var count int64
		want := int64(1)
		if phase == "reverse" {
			want = 0
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&count).Error; err != nil || count != want {
			t.Fatalf("phase=%s count=%d err=%v", phase, count, err)
		}
	}
	t.Logf("first_scanned=%d reversed=%d next_settled=%d balance=%d ledger=%d events=%d", first.Scanned, first.Reversed, second.Settled, live, ledger, events)
}

type usageSettlementBridge struct{ *stateSweepBridge }

func (b usageSettlementBridge) Settle(ctx context.Context, req *pb.SettleRequest, _ ...grpc.CallOption) (*pb.SettleResponse, error) {
	return b.s.Settle(ctx, req)
}
