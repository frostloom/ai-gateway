package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReconcilerBacklogBoundedWithoutStarvingUsage(t *testing.T) {
	if os.Getenv("TEST_RECON_BACKLOG") != "1" {
		t.Skip("isolated backlog only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("recon-backlog-%d", time.Now().UnixNano())
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
	backlog := 1025
	if raw := os.Getenv("TEST_RECON_BACKLOG_SIZE"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 257 || parsed > 100000 {
			t.Fatalf("invalid backlog size %q", raw)
		}
		backlog = parsed
	}
	rows := make([]store.Bill, backlog)
	for i := range rows {
		rows[i] = store.Bill{RequestID: fmt.Sprintf("%s-u%d", id, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: id, Funding: store.FundingSubscription, CreatedAt: time.Now().Add(-time.Hour)}
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatal(err)
	}
	known := id + "-known"
	if _, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: known, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: known, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", known, store.PhaseReserve).UpdateColumn("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	var maxRead atomic.Int64
	if err := db.Callback().Query().After("gorm:query").Register("test:recon_page_size", func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" {
			return
		}
		if got, ok := tx.Statement.Dest.(*[]store.Bill); ok {
			n := int64(len(*got))
			for {
				old := maxRead.Load()
				if n <= old || maxRead.CompareAndSwap(old, n) {
					break
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:recon_page_size")
	started := time.Now()
	stats, err := reconciler.New(db, directSweepStore{s: s}, slog.New(slog.DiscardHandler), time.Minute).SweepOnce(ctx)
	elapsed := time.Since(started)
	if err != nil || stats.Held < backlog || stats.Settled < 1 || stats.Reversed != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if maxRead.Load() > 256 {
		t.Fatalf("largest bill result=%d, want <=256", maxRead.Load())
	}
	var knownRow store.Bill
	if err := db.Where("request_id=? AND phase=?", known, store.PhaseReserve).First(&knownRow).Error; err != nil || knownRow.Status != store.StatusSettled {
		t.Fatal(knownRow.Status, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	ledger, le := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || le != nil || live != 960 || ledger != 960 {
		t.Fatalf("live=%d ledger=%d err=%v/%v", live, ledger, err, le)
	}
	t.Logf("backlog=%d scanned=%d held=%d settled=%d max_bill_rows=%d elapsed_ms=%d live=%d ledger=%d", backlog, stats.Scanned, stats.Held, stats.Settled, maxRead.Load(), elapsed.Milliseconds(), live, ledger)
}

func TestReconcilerSweepBoundsNewIDs(t *testing.T) {
	if os.Getenv("TEST_RECON_BACKLOG") != "1" {
		t.Skip("isolated backlog only")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("recon-fence-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]store.Bill, 300)
	for i := range rows {
		rows[i] = store.Bill{RequestID: fmt.Sprintf("%s-%d", id, i), Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingSubscription, CreatedAt: time.Now().Add(-time.Hour)}
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.Model(&store.Bill{}).Where("phase=? AND status=? AND created_at<?", store.PhaseReserve, store.StatusPending, time.Now().Add(-time.Minute)).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	late := store.Bill{RequestID: id + "-late", Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingSubscription, CreatedAt: time.Now().Add(-time.Hour)}
	var inserted atomic.Bool
	var insertErr error
	if err := db.Callback().Query().After("gorm:query").Register("test:recon_late_insert", func(tx *gorm.DB) {
		rows, ok := tx.Statement.Dest.(*[]store.Bill)
		if tx.Statement.Table != "bills" || !ok || len(*rows) == 0 || !inserted.CompareAndSwap(false, true) {
			return
		}
		insertErr = db.Create(&late).Error
	}); err != nil {
		t.Fatal(err)
	}
	recon := reconciler.New(db, directSweepStore{s: s}, slog.New(slog.DiscardHandler), time.Minute)
	first, err := recon.SweepOnce(ctx)
	if removeErr := db.Callback().Query().Remove("test:recon_late_insert"); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err != nil || insertErr != nil || !inserted.Load() || int64(first.Scanned) != before || int64(first.Held) != before {
		t.Fatalf("before=%d first=%+v inserted=%v err=%v/%v", before, first, inserted.Load(), err, insertErr)
	}
	second, err := recon.SweepOnce(ctx)
	if err != nil || int64(second.Scanned) != before+1 || int64(second.Held) != before+1 {
		t.Fatalf("before=%d second=%+v err=%v", before, second, err)
	}
	t.Logf("first_scanned=%d new_id_deferred=true second_scanned=%d", first.Scanned, second.Scanned)
}
