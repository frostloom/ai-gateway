package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Test-only state transition; no row lock is held across Store or Redis calls.
func markReversing(ctx context.Context, db *gorm.DB, id string) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var intent sqlIdemIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=?", id).First(&intent).Error; e != nil {
			return e
		}
		if intent.State != "claimed" {
			return nil
		}
		var bill store.Bill
		if e := tx.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; e != nil {
			return e
		}
		if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			return nil
		}
		if e := tx.Model(&intent).Update("state", "reversing").Error; e != nil {
			return e
		}
		won = true
		return nil
	})
	return
}
func finishReversing(ctx context.Context, db *gorm.DB, s *store.Store, id string) (bool, error) {
	var intent sqlIdemIntent
	if e := db.WithContext(ctx).Where("request_id=?", id).First(&intent).Error; e != nil {
		return false, e
	}
	if intent.State == "reversed" {
		return true, nil
	}
	if intent.State != "reversing" {
		return false, nil
	}
	r, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "test:known-not-forwarded"})
	if e != nil {
		return false, e
	}
	if r == nil || !r.Reversed {
		return false, fmt.Errorf("reverse did not complete")
	}
	update := db.WithContext(ctx).Model(&sqlIdemIntent{}).Where("request_id=? AND state=?", id, "reversing").Update("state", "reversed")
	if update.Error != nil {
		return false, update.Error
	}
	if update.RowsAffected == 1 {
		return true, nil
	}
	if e = db.WithContext(ctx).Where("request_id=?", id).First(&intent).Error; e != nil {
		return false, e
	}
	return intent.State == "reversed", nil
}

func TestReversingIntentRecovery(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	g := sqlIdemGate{db: db}
	if e := s.AutoMigrate(); e != nil {
		t.Fatal(e)
	}
	if e := g.migrate(); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	id := fmt.Sprintf("reversing-%d", time.Now().UnixNano())
	m := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&m).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	if state, e := g.claim(ctx, id, "same", "owner", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	b, e := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, e)
	}
	if ok, e := markReversing(ctx, db, id); e != nil || !ok {
		t.Fatalf("mark=%v %v", ok, e)
	}
	// Simulated stop after durable mark: no refund has been attempted yet.
	if ok, e := coordinatedForward(ctx, db, id, "owner"); e != nil || ok {
		t.Fatalf("forward after reversing=%v %v", ok, e)
	}
	// Simulate the worker disappearing after the financial reverse committed,
	// but before it could mark the durable intent complete.
	if reversed, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "test:known-not-forwarded"}); err != nil || reversed == nil || !reversed.Reversed {
		t.Fatalf("reverse=%v err=%v", reversed, err)
	}
	var before sqlIdemIntent
	if err := db.Where("request_id=?", id).First(&before).Error; err != nil || before.State != "reversing" {
		t.Fatalf("intent=%v err=%v", before, err)
	}
	if ok, e := finishReversing(ctx, db, s, id); e != nil || !ok {
		t.Fatalf("finish=%v %v", ok, e)
	}
	if ok, e := finishReversing(ctx, db, s, id); e != nil || !ok {
		t.Fatalf("duplicate finish=%v %v", ok, e)
	}
	var row store.Bill
	if e := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; e != nil {
		t.Fatal(e)
	}
	var ev int64
	if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse"}).Count(&ev).Error; e != nil {
		t.Fatal(e)
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || row.Status != store.StatusReversed || live != 1000 || ledger != live || ev != 2 {
		t.Fatalf("status=%s live=%d ledger=%d events=%d err=%v", row.Status, live, ledger, ev, e)
	}
}
