package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func TestEarlyUsageSweepDoesNotHandleYoungOrUnknown(t *testing.T) {
	if os.Getenv("TEST_RECON_EARLY_ASSERT") != "1" {
		t.Skip("explicit early-sweep assertion only")
	}
	db, redis := isolatedDB(t)
	s := store.NewStore(db, redis)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("early-usage-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: prefix, InitialQuota: 10000}
	model := store.Model{Vendor: "test", ModelID: prefix, Name: prefix, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 10000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	ids := []string{prefix + "-aged", prefix + "-fresh", prefix + "-unknown"}
	for _, id := range ids {
		res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: prefix, EstPromptTokens: 84})
		if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(res, err)
		}
	}
	for _, id := range ids[:2] {
		if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", ids[0], store.PhaseReserve).UpdateColumn("usage_reported_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	stats, err := reconciler.New(db, directSweepStore{s: s}, slog.New(slog.DiscardHandler), 10*time.Minute).SweepOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		var b store.Bill
		if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&b).Error; err != nil {
			t.Fatal(err)
		}
		want := store.StatusPending
		if i == 0 {
			want = store.StatusSettled
		}
		if b.Status != want {
			t.Fatalf("%s status=%s want=%s stats=%+v", id, b.Status, want, stats)
		}
		var settles int64
		if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&settles).Error; err != nil {
			t.Fatal(err)
		}
		expected := int64(0)
		if i == 0 {
			expected = 1
		}
		if settles != expected {
			t.Fatalf("%s settle rows=%d", id, settles)
		}
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != ledger {
		t.Fatalf("balance=%d ledger=%d err=%v", live, ledger, err)
	}
	if stats.Scanned != 1 || stats.Settled != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}
