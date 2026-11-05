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
	"gorm.io/gorm/logger"
)

// Inject a successful usage commit after SweepOnce has chosen Reverse, before
// Reverse reaches the real Store. This controls the race without relying on timing.
type usageBeforeReverse struct {
	pb.BillingServiceClient
	store *store.Store
}

func (b usageBeforeReverse) Reverse(ctx context.Context, req *pb.ReverseRequest, _ ...grpc.CallOption) (*pb.ReverseResponse, error) {
	if err := b.store.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 40}); err != nil {
		return nil, err
	}
	return b.store.Reverse(ctx, req)
}

func TestExistingReconcilerReversesCommittedUsageAfterScan(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("scan-usage-race-%d", time.Now().UnixNano())
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
	reserve, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", reserve.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	stats, err := reconciler.New(db, usageBeforeReverse{store: s}, slog.New(slog.DiscardHandler), 10*time.Minute, reconciler.WithAutoReverse()).SweepOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var bill store.Bill
	if err := db.First(&bill, reserve.BillId).Error; err != nil {
		t.Fatal(err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":reverse", id + ":settle"}).Count(&events).Error; err != nil {
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
	if stats.Scanned != 1 || stats.Reversed != 1 || bill.Status != store.StatusReversed || bill.UsageReportedAt == nil || bill.PromptTokens == nil || *bill.PromptTokens != 40 || events != 3 || live != 1000 || ledger != live {
		t.Fatalf("stats=%+v bill=%+v events=%d balance=%d ledger=%d", stats, bill, events, live, ledger)
	}
	settle, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id, BillId: int64(reserve.BillId)})
	if err != nil || settle.Settled {
		t.Fatalf("settle=%+v err=%v", settle, err)
	}
	t.Logf("scan=1 usage_committed=40 reversed=1 settle=false balance=%d ledger=%d events=%d", live, ledger, events)
}
