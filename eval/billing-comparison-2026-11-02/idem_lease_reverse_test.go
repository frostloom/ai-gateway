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

// Test-only: only a database-expired pre-forward claim is eligible.
func markExpiredReversing(ctx context.Context, db *gorm.DB, id string) (won bool, err error) {
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
		result := tx.Model(&sqlIdemIntent{}).Where("request_id=? AND state=? AND lease_until <= CURRENT_TIMESTAMP(6)", id, "claimed").Update("state", "reversing")
		won = result.RowsAffected == 1
		return result.Error
	})
	return
}

func TestReconcileWaitsForExpiredClaim(t *testing.T) {
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
	id := fmt.Sprintf("lease-reverse-%d", time.Now().UnixNano())
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
	if state, e := g.claim(ctx, id, "same", "owner", 2*time.Second); e != nil || state != gateOwned {
		t.Fatal(state, e)
	}
	b, e := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, e)
	}
	if ok, e := markExpiredReversing(ctx, db, id); e != nil || ok {
		t.Fatalf("live claim marked reversing: %v %v", ok, e)
	}
	// Observe the database's own clock and wait for natural expiration.
	deadline := time.Now().Add(8 * time.Second)
	for {
		var live bool
		if e := db.Raw("SELECT lease_until > CURRENT_TIMESTAMP(6) FROM eval_idem_gate WHERE request_id=?", id).Scan(&live).Error; e != nil {
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
	var wins atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := markExpiredReversing(ctx, db, id)
			if e != nil {
				errs <- e
			} else if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if wins.Load() != 1 {
		t.Fatalf("mark winners=%d", wins.Load())
	}
	if ok, e := coordinatedForward(ctx, db, id, "owner"); e != nil || ok {
		t.Fatalf("expired owner forwarded=%v %v", ok, e)
	}
	if ok, e := finishReversing(ctx, db, s, id); e != nil || !ok {
		t.Fatal(ok, e)
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || live != 1000 || ledger != live {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, e)
	}
}
