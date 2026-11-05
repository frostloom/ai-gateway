package comparison

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Test-only marker blocks takeovers while a reservation outcome is uncertain.
func beginReserving(ctx context.Context, db *gorm.DB, id, owner string) (bool, error) {
	q := db.WithContext(ctx).Model(&sqlIdemIntent{}).Where("request_id=? AND owner=? AND state=? AND lease_until > CURRENT_TIMESTAMP(6)", id, owner, "claimed").Update("state", "reserving")
	return q.RowsAffected == 1, q.Error
}
func finishReserving(ctx context.Context, db *gorm.DB, id, owner string) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var intent sqlIdemIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=?", id).First(&intent).Error; e != nil {
			return e
		}
		if intent.State != "reserving" || intent.Owner != owner {
			return nil
		}
		var bill store.Bill
		if e := tx.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; e != nil {
			return e
		}
		if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			return nil
		}
		q := tx.Model(&sqlIdemIntent{}).Where("request_id=? AND owner=? AND state=? AND lease_until > CURRENT_TIMESTAMP(6)", id, owner, "reserving").Update("state", "reserved")
		won = q.RowsAffected == 1
		return q.Error
	})
	return
}

func forwardReserved(ctx context.Context, db *gorm.DB, id, owner string) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var intent sqlIdemIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=?", id).First(&intent).Error; e != nil {
			return e
		}
		if intent.State != "reserved" || intent.Owner != owner {
			return nil
		}
		var live bool
		if e := tx.Raw("SELECT lease_until > CURRENT_TIMESTAMP(6) FROM eval_idem_gate WHERE request_id=?", id).Scan(&live).Error; e != nil {
			return e
		}
		if !live {
			return nil
		}
		var bill store.Bill
		if e := tx.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; e != nil {
			return e
		}
		if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			return nil
		}
		if e := tx.Model(&intent).Update("state", "forwarding").Error; e != nil {
			return e
		}
		won = true
		return nil
	})
	return
}

func TestReservingIntentSafety(t *testing.T) {
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
	base := fmt.Sprintf("reserving-%d", time.Now().UnixNano())
	m := store.Model{Vendor: "test", ModelID: base, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&m).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: base, InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	id := base + "-ok"
	if state, e := g.claim(ctx, id, "same", "owner", time.Minute); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := beginReserving(ctx, db, id, "owner")
			if e != nil {
				errs <- e
			} else if ok {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if winners.Load() != 1 {
		t.Fatalf("reserve intent winners=%d", winners.Load())
	}
	if ok, e := forwardReserved(ctx, db, id, "owner"); e != nil || ok {
		t.Fatalf("forward before reserve=%v %v", ok, e)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: base, EstPromptTokens: 84}
	b, e := s.Reserve(ctx, req)
	if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, e)
	}
	if ok, e := finishReserving(ctx, db, id, "owner"); e != nil || !ok {
		t.Fatalf("finish valid=%v %v", ok, e)
	}
	if ok, e := beginReserving(ctx, db, id, "owner"); e != nil || ok {
		t.Fatalf("reserve intent replay=%v %v", ok, e)
	}
	if ok, e := forwardReserved(ctx, db, id, "owner"); e != nil || !ok {
		t.Fatalf("forward ready=%v %v", ok, e)
	}
	if e := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 50}); e != nil {
		t.Fatal(e)
	}
	if z, e := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); e != nil || !z.Settled {
		t.Fatal(z, e)
	}
	// An operation may have completed while its owner was delayed. Preserve the
	// uncertain reserving state after natural lease expiry, and deny new sends.
	late := base + "-late"
	if state, e := g.claim(ctx, late, "same", "old", 2*time.Second); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	if ok, e := beginReserving(ctx, db, late, "old"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	req.RequestId = late
	b, e = s.Reserve(ctx, req)
	if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, e)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		var live bool
		if e := db.Raw("SELECT lease_until > CURRENT_TIMESTAMP(6) FROM eval_idem_gate WHERE request_id=?", late).Scan(&live).Error; e != nil {
			t.Fatal(e)
		}
		if !live {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lease not expired")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ok, e := finishReserving(ctx, db, late, "old"); e != nil || ok {
		t.Fatalf("expired owner completed=%v %v", ok, e)
	}
	if ok, e := forwardReserved(ctx, db, late, "old"); e != nil || ok {
		t.Fatalf("late forward=%v %v", ok, e)
	}
	if ok, e := markExpiredReversing(ctx, db, late); e != nil || ok {
		t.Fatalf("uncertain refund=%v %v", ok, e)
	}
	var row store.Bill
	if e := db.Where("request_id=? AND phase=?", late, store.PhaseReserve).First(&row).Error; e != nil || row.Status != store.StatusPending {
		t.Fatalf("row=%v err=%v", row.Status, e)
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || live != 850 || ledger != live {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, e)
	}
}
