package comparison

import (
	"context"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
	"log/slog"
	"testing"
	"time"
)

func TestReconcilerDefaultRetainsUnknown(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("settle-only-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{id + "-unknown", id + "-known"}
	bills := make([]*pb.ReserveResponse, 2)
	for i, rid := range ids {
		bill, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
		if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(bill, err)
		}
		bills[i] = bill
		if err := db.Model(&store.Bill{}).Where("id=?", bill.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: ids[1], PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	// Default must retain no-usage bills while settling recorded usage.
	runner := reconciler.New(db, directSweepStore{s: s}, slog.New(slog.DiscardHandler), 10*time.Minute)
	first, err := runner.SweepOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var unknown, known store.Bill
	if err := db.First(&unknown, bills[0].BillId).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&known, bills[1].BillId).Error; err != nil {
		t.Fatal(err)
	}
	if unknown.Status != store.StatusPending || unknown.UsageReportedAt != nil || known.Status != store.StatusSettled || first.Settled < 1 || first.Reversed != 0 || first.Held < 1 {
		t.Fatalf("stats=%+v unknown=%s known=%s", first, unknown.Status, known.Status)
	}
	second, err := runner.SweepOnce(ctx)
	if err != nil || second.Reversed != 0 || second.Settled != 0 || second.Held < 1 {
		t.Fatalf("second sweep=%+v err=%v", second, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger || live != 860 {
		t.Fatal(live, ledger, err)
	}
	for _, tc := range []struct {
		key  string
		want int64
	}{{ids[0] + ":reserve", 1}, {ids[0] + ":reverse", 0}, {ids[1] + ":reserve", 1}, {ids[1] + ":usage", 1}, {ids[1] + ":settle", 1}} {
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", tc.key).Count(&n).Error; err != nil || n != tc.want {
			t.Fatal(tc.key, n, err)
		}
	}
	t.Logf("unknown=pending known=settled balance=%d ledger=%d", live, ledger)
}
