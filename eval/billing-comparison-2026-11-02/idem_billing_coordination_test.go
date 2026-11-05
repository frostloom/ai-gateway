package comparison

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// Test-only design: callers must all use this gate, including reconciliation.
// A database row lock is held across a Store reverse; this is deliberately a
// safety experiment, not a production-ready or distributed-transaction proof.
func coordinatedForward(ctx context.Context, db *gorm.DB, id, owner string) (won bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var intent sqlIdemIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=?", id).First(&intent).Error; e != nil {
			return e
		}
		if intent.State != "claimed" || intent.Owner != owner {
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
func coordinatedReconcile(ctx context.Context, db *gorm.DB, s *store.Store, id string) (won bool, err error) {
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
		r, e := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, BillId: int64(bill.ID), Reason: "test:known-not-forwarded"})
		if e != nil {
			return e
		}
		if r == nil || !r.Reversed {
			return fmt.Errorf("reverse did not complete")
		}
		if e = tx.Model(&intent).Update("state", "reversed").Error; e != nil {
			return e
		}
		won = true
		return nil
	})
	return
}

func TestCoordinatedForwardAndReconcile(t *testing.T) {
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
	name := fmt.Sprintf("coord-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: name, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&model).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: name, InitialQuota: 10000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 10000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer upstream.Close()
	var sent, refunded int
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("%s-%d", name, i)
		if state, e := g.claim(ctx, id, "fixed", "owner", time.Minute); e != nil || state != gateOwned {
			t.Fatal(state, e)
		}
		b, e := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: name, EstPromptTokens: 84})
		if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(b, e)
		}
		var fwd, rev bool
		var fe, re error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; fwd, fe = coordinatedForward(ctx, db, id, "owner") }()
		go func() { defer wg.Done(); <-start; rev, re = coordinatedReconcile(ctx, db, s, id) }()
		close(start)
		wg.Wait()
		if fe != nil || re != nil || fwd == rev {
			t.Fatalf("forward=%v (%v), reverse=%v (%v)", fwd, fe, rev, re)
		}
		if fwd {
			sent++
			if ok, e := coordinatedReconcile(ctx, db, s, id); e != nil || ok {
				t.Fatalf("forwarding auto-refund: %v %v", ok, e)
			}
			if ok, e := coordinatedForward(ctx, db, id, "owner"); e != nil || ok {
				t.Fatalf("forwarding replay: %v %v", ok, e)
			}
			res, e := http.Get(upstream.URL)
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if e := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 50}); e != nil {
				t.Fatal(e)
			}
			if z, e := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); e != nil || !z.Settled {
				t.Fatal(z, e)
			}
		} else {
			refunded++
		}
		var bills, events int64
		if e := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle", id + ":reverse"}).Count(&events).Error; e != nil {
			t.Fatal(e)
		}
		wantBills, wantEvents := int64(1), int64(2)
		if fwd {
			wantBills, wantEvents = 2, 3
		}
		if bills != wantBills || events != wantEvents {
			t.Fatalf("forward=%v bills=%d events=%d", fwd, bills, events)
		}
	}
	var pending int64
	if e := db.Model(&store.Bill{}).Where("tenant_id=? AND phase=? AND status=?", tenant.ID, store.PhaseReserve, store.StatusPending).Count(&pending).Error; e != nil {
		t.Fatal(e)
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || live != ledger || live != 10000-int64(sent)*50 || pending != 0 || calls.Load() != int64(sent) {
		t.Fatalf("sent=%d reversed=%d calls=%d live=%d ledger=%d pending=%d err=%v", sent, refunded, calls.Load(), live, ledger, pending, e)
	}
	t.Logf("sent=%d reversed=%d mock_calls=%d live=%d ledger=%d", sent, refunded, calls.Load(), live, ledger)
}

// Test-only staged transition makes recovery retryable without holding gate lock across Redis.
func stagedReconcile(ctx context.Context, db *gorm.DB, s *store.Store, id string) (bool, error) {
	marked, err := markReversing(ctx, db, id)
	if err != nil || !marked {
		return false, err
	}
	return finishReversing(ctx, db, s, id)
}

func TestStagedForwardAndReconcile(t *testing.T) {
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
	name := fmt.Sprintf("coord-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: name, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if e := db.Create(&model).Error; e != nil {
		t.Fatal(e)
	}
	tenant := store.Tenant{Name: name, InitialQuota: 10000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e := r.Set(ctx, store.BalanceKey(tenant.ID), 10000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer upstream.Close()
	var sent, refunded int
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("%s-%d", name, i)
		if state, e := g.claim(ctx, id, "fixed", "owner", time.Minute); e != nil || state != gateOwned {
			t.Fatal(state, e)
		}
		b, e := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: name, EstPromptTokens: 84})
		if e != nil || b.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(b, e)
		}
		var fwd, rev bool
		var fe, re error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; fwd, fe = coordinatedForward(ctx, db, id, "owner") }()
		go func() { defer wg.Done(); <-start; rev, re = stagedReconcile(ctx, db, s, id) }()
		close(start)
		wg.Wait()
		if fe != nil || re != nil || fwd == rev {
			t.Fatalf("forward=%v (%v), reverse=%v (%v)", fwd, fe, rev, re)
		}
		if fwd {
			sent++
			if ok, e := stagedReconcile(ctx, db, s, id); e != nil || ok {
				t.Fatalf("forwarding auto-refund: %v %v", ok, e)
			}
			if ok, e := coordinatedForward(ctx, db, id, "owner"); e != nil || ok {
				t.Fatalf("forwarding replay: %v %v", ok, e)
			}
			res, e := http.Get(upstream.URL)
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if e := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 50}); e != nil {
				t.Fatal(e)
			}
			if z, e := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); e != nil || !z.Settled {
				t.Fatal(z, e)
			}
		} else {
			refunded++
		}
		var bills, events int64
		if e := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle", id + ":reverse"}).Count(&events).Error; e != nil {
			t.Fatal(e)
		}
		wantBills, wantEvents := int64(1), int64(2)
		if fwd {
			wantBills, wantEvents = 2, 3
		}
		if bills != wantBills || events != wantEvents {
			t.Fatalf("forward=%v bills=%d events=%d", fwd, bills, events)
		}
	}
	var pending int64
	if e := db.Model(&store.Bill{}).Where("tenant_id=? AND phase=? AND status=?", tenant.ID, store.PhaseReserve, store.StatusPending).Count(&pending).Error; e != nil {
		t.Fatal(e)
	}
	live, e := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if e != nil {
		t.Fatal(e)
	}
	ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
	if e != nil || live != ledger || live != 10000-int64(sent)*50 || pending != 0 || calls.Load() != int64(sent) {
		t.Fatalf("sent=%d reversed=%d calls=%d live=%d ledger=%d pending=%d err=%v", sent, refunded, calls.Load(), live, ledger, pending, e)
	}
	t.Logf("sent=%d reversed=%d mock_calls=%d live=%d ledger=%d", sent, refunded, calls.Load(), live, ledger)
}
