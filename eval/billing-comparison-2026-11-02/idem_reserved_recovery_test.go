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

// Test-only: reserve completed, no forwarding allowed, lease expired.
func markExpiredReserved(ctx context.Context, db *gorm.DB, id string) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var intent sqlIdemIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=?", id).First(&intent).Error; e != nil {
			return e
		}
		if intent.State != "reserved" {
			return nil
		}
		var bill store.Bill
		if e := tx.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; e != nil {
			return e
		}
		if bill.Status != store.StatusPending || bill.UsageReportedAt != nil {
			return nil
		}
		q := tx.Model(&sqlIdemIntent{}).Where("request_id=? AND state=? AND lease_until <= CURRENT_TIMESTAMP(6)", id, "reserved").Update("state", "reversing")
		won = q.RowsAffected == 1
		return q.Error
	})
	return
}
func TestExpiredReservedRecovery(t *testing.T) {
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
	id := fmt.Sprintf("reserved-recovery-%d", time.Now().UnixNano())
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
	if ok, e := beginReserving(ctx, db, id, "owner"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	b, e := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(b, e)
	}
	if ok, e := finishReserving(ctx, db, id, "owner"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if ok, e := markExpiredReserved(ctx, db, id); e != nil || ok {
		t.Fatalf("live reserved marked=%v %v", ok, e)
	}
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
			ok, e := markExpiredReserved(ctx, db, id)
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
		t.Fatalf("recovery winners=%d", wins.Load())
	}
	if ok, e := forwardReserved(ctx, db, id, "owner"); e != nil || ok {
		t.Fatalf("stale owner forwarded=%v %v", ok, e)
	}
	if ok, e := finishReversing(ctx, db, s, id); e != nil || !ok {
		t.Fatalf("finish=%v %v", ok, e)
	}
	var bill store.Bill
	if e := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; e != nil {
		t.Fatal(e)
	}
	var events int64
	if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":reverse"}).Count(&events).Error; e != nil {
		t.Fatal(e)
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || bill.Status != store.StatusReversed || live != 1000 || live != ledger || events != 2 {
		t.Fatalf("status=%s live=%d ledger=%d events=%d err=%v", bill.Status, live, ledger, events, e)
	}
	// Once forwarding is committed, force an expired timestamp to exercise
	// the classification; expiration cannot establish a safe refund.
	sent := id + "-forwarding"
	if state, err := g.claim(ctx, sent, "same", "owner", time.Minute); err != nil || state != gateOwned {
		t.Fatal(state, err)
	}
	if ok, err := beginReserving(ctx, db, sent, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	reserved, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: sent, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserved.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserved, err)
	}
	if ok, err := finishReserving(ctx, db, sent, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := forwardReserved(ctx, db, sent, "owner"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := db.Exec("UPDATE eval_idem_gate SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE request_id=?", sent).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := markExpiredReserved(ctx, db, sent); err != nil || ok {
		t.Fatalf("forwarding was refundable: %v %v", ok, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: sent, PromptTokens: 50}); err != nil {
		t.Fatal(err)
	}
	if z, err := s.Settle(ctx, &pb.SettleRequest{RequestId: sent}); err != nil || !z.Settled {
		t.Fatal(z, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 950 || ledger != live {
		t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
	}

}
