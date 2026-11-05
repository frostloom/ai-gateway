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
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Experimental scan only: no scheduler, batching, ownership, or retry backoff.
func recoverReversingIntents(ctx context.Context, db *gorm.DB, s *store.Store) (int, error) {
	var rows []sqlIdemIntent
	if err := db.WithContext(ctx).Where("state=?", "reversing").Find(&rows).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		ok, err := finishReversing(ctx, db, s, row.RequestID)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

func TestReversingIntentNeedsSeparateRecoveryScan(t *testing.T) {
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
	id := fmt.Sprintf("intent-scan-%d", time.Now().UnixNano())
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
	bill, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	if ok, err := finishReserving(ctx, db, id, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := markExpiredReserved(ctx, db, id); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// Simulate process interruption after Store committed but before intent completion.
	reversed, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(bill.BillId), Reason: "test:known-not-forwarded"})
	if err != nil || !reversed.Reversed {
		t.Fatal(reversed, err)
	}
	runner := reconciler.New(db, gateBypassBilling{store: s}, slog.New(slog.DiscardHandler), 0, reconciler.WithAutoReverse())
	stats, err := runner.SweepOnce(ctx)
	if err != nil || stats.Scanned != 0 {
		t.Fatal(stats, err)
	}
	var intent sqlIdemIntent
	if err := db.Where("request_id=?", id).First(&intent).Error; err != nil || intent.State != "reversing" {
		t.Fatal(intent, err)
	}
	recovered, err := recoverReversingIntents(ctx, db, s)
	if err != nil || recovered != 1 {
		t.Fatal(recovered, err)
	}
	recovered, err = recoverReversingIntents(ctx, db, s)
	if err != nil || recovered != 0 {
		t.Fatal(recovered, err)
	}
	if err := db.Where("request_id=?", id).First(&intent).Error; err != nil || intent.State != "reversed" {
		t.Fatal(intent, err)
	}
	balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || balance != 1000 || ledger != balance {
		t.Fatal(balance, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse"}).Count(&events).Error; err != nil || events != 2 {
		t.Fatal(events, err)
	}
	t.Logf("ordinary_scanned=%d intent=%s balance=%d ledger=%d events=%d repeated_recover=%d", stats.Scanned, intent.State, balance, ledger, events, recovered)
}
