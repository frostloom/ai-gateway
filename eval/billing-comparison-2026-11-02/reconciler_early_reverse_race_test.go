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

func TestEarlyUsageExplicitReverseWinsBeforeSettlement(t *testing.T) {
	if os.Getenv("TEST_RECON_EARLY_ASSERT") != "1" {
		t.Skip("explicit candidate only")
	}
	db, r := isolatedDB(t)
	s := store.NewStore(db, r)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("early-reverse-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	reserve, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserve.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserve, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", reserve.BillId).UpdateColumn("usage_reported_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	release := make(chan struct{})
	cli := synchronizedSettlement{directSweepStore{s: s}, ready, release}
	type result struct {
		stats reconciler.Stats
		err   error
	}
	done := make(chan result, 1)
	go func() {
		stats, err := reconciler.New(db, cli, slog.New(slog.DiscardHandler), 10*time.Minute).SweepOnce(ctx)
		done <- result{stats, err}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		close(release)
		t.Fatal("no scan", ctx.Err())
	}
	reverse, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: reserve.BillId, Reason: "explicit-test"})
	close(release)
	if err != nil || !reverse.Reversed {
		t.Fatal(reverse, err)
	}
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	var row store.Bill
	if err := db.First(&row, reserve.BillId).Error; err != nil || row.Status != store.StatusReversed || row.UsageReportedAt == nil {
		t.Fatal(row, err)
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseSettle).Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 1000 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	for _, phase := range []string{"reserve", "usage", "reverse", "settle"} {
		var n int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		want := int64(1)
		if phase == "settle" {
			want = 0
		}
		if n != want {
			t.Fatalf("%s=%d", phase, n)
		}
	}
	t.Logf("explicit reverse won after scan; usage retained, no settlement, balance=%d stats=%+v", live, outcome.stats)
}
