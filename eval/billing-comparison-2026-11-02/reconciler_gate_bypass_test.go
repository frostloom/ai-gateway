package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// Only the transport is replaced: the real reconciler calls the real Store.
type gateBypassBilling struct {
	pb.BillingServiceClient
	store *store.Store
}

func (b gateBypassBilling) Reverse(ctx context.Context, req *pb.ReverseRequest, _ ...grpc.CallOption) (*pb.ReverseResponse, error) {
	return b.store.Reverse(ctx, req)
}

func TestExistingReconcilerBypassesForwardingGate(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	g := sqlIdemGate{db: db}
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := g.migrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reconciler-gate-bypass-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
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
	if state, err := g.claim(ctx, id, "same", "owner", time.Minute); err != nil || state != gateOwned {
		t.Fatal(state, err)
	}
	if ok, err := beginReserving(ctx, db, id, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	b, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, err)
	}
	if ok, err := finishReserving(ctx, db, id, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := forwardReserved(ctx, db, id, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// Simulate an old reservation without waiting: no model service is invoked.
	if err := db.Model(&store.Bill{}).Where("id=?", b.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	runner := reconciler.New(db, gateBypassBilling{store: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse())
	stats, err := runner.SweepOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var bill store.Bill
	if err := db.Where("id=?", b.BillId).First(&bill).Error; err != nil {
		t.Fatal(err)
	}
	var gate sqlIdemIntent
	if err := db.Where("request_id=?", id).First(&gate).Error; err != nil {
		t.Fatal(err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse"}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("scanned=%d reversed=%d gate=%s bill=%s balance=%d ledger=%d events=%d", stats.Scanned, stats.Reversed, gate.State, bill.Status, live, ledger, events)
	if stats.Reversed != 1 || gate.State != "forwarding" || bill.Status != store.StatusReversed || live != 1000 || ledger != 1000 || events != 2 {
		t.Fatal("expected existing reconciler to bypass the test-only forwarding gate")
	}
	// A synthetic late usage report models a response arriving after the sweep.
	// This test does not invoke an upstream service.
	err = s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 84, CompletionTokens: 12})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late usage error = %v, want FailedPrecondition", err)
	}
	usageCode := status.Code(err)
	settle, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: int64(b.BillId)})
	if err != nil || settle.Settled {
		t.Fatalf("late settle = %+v, err = %v", settle, err)
	}
	var usageEvents int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key = ?", id+":usage").Count(&usageEvents).Error; err != nil || usageEvents != 0 {
		t.Fatalf("usage events = %d, err = %v", usageEvents, err)
	}
	t.Logf("late synthetic usage=%s settle=%v usage_events=%d", usageCode, settle.Settled, usageEvents)
}
