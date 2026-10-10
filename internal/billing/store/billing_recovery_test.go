package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
)

// The first attempt fails after Redis debits. A second Store (another process's
// equivalent) must not commit until the first attempt has finished compensation.
func TestReserveConcurrentFailureCannotRefundCommittedReplay(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	req := reserveReq("rid-concurrent-failure", tenant, 84)
	entered, resume := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	if err := s.db.Callback().Create().Before("gorm:create").Register("test:reserve_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" && calls.Add(1) == 1 {
			close(entered)
			<-resume
			tx.AddError(errors.New("first insert fails"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Create().Remove("test:reserve_failure") })
	first := make(chan error, 1)
	go func() { _, err := s.Reserve(context.Background(), req); first <- err }()
	<-entered
	second := make(chan error, 1)
	other := NewStore(s.db, s.rdb)
	go func() { _, err := other.Reserve(context.Background(), req); second <- err }()
	// Wait for the second caller either to commit (old defect) or for MySQL to
	// report its advisory-lock wait. No scheduling sleep determines the outcome.
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	secondDone := false
waiting:
	for {
		select {
		case err := <-second:
			if err != nil {
				close(resume)
				t.Fatalf("second reserve: %v", err)
			}
			secondDone = true
			break waiting
		case <-ticker.C:
			var n int64
			if err := s.db.Raw("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE STATE = 'User lock' AND INFO LIKE '%GET_LOCK%'").Scan(&n).Error; err != nil {
				close(resume)
				t.Fatal(err)
			}
			if n > 0 {
				break waiting
			}
		case <-deadline:
			close(resume)
			t.Fatal("second caller neither committed nor waited for request lock")
		}
	}
	close(resume)
	if err := <-first; err == nil {
		t.Fatal("first insert should fail")
	}
	if !secondDone {
		if err := <-second; err != nil {
			t.Fatalf("second reserve: %v", err)
		}
	}
	if bal, err := s.getBalance(context.Background(), tenant); err != nil || bal != 900 {
		t.Fatalf("balance=%d err=%v, want 900 for committed reserve", bal, err)
	}
	ledger, err := LedgerBalance(context.Background(), s.db, tenant)
	if err != nil || ledger != 900 {
		t.Fatalf("ledger=%d err=%v, want 900", ledger, err)
	}
}

func TestReserveCanceledInsertStillCompensates(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.db.Callback().Create().Before("gorm:create").Register("test:cancel_reserve", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" {
			cancel()
			tx.AddError(context.Canceled)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Create().Remove("test:cancel_reserve") })
	req := reserveReq("rid-canceled-insert", tenant, 84)
	if _, err := s.Reserve(ctx, req); err == nil {
		t.Fatal("canceled reserve should fail")
	}
	if bal, err := s.getBalance(context.Background(), tenant); err != nil || bal != 1000 {
		t.Fatalf("balance=%d err=%v, want compensated 1000", bal, err)
	}
	if n, err := s.rdb.Exists(context.Background(), ReqGuardKey(req.RequestId)).Result(); err != nil || n != 0 {
		t.Fatalf("guard exists=%d err=%v", n, err)
	}
}

func TestSettleLegacyGuardRecoversPendingLedger(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		prompt, balance, delta, actual int64
	}{
		{"debit", 150, 850, 50, 150}, {"refund", 50, 950, -50, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx := context.Background()
			seedMockModel(t, s)
			tenant := seedTenant(t, s, 1000)
			req := reserveReq("rid-legacy-"+tc.name, tenant, 84)
			if _, err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: tc.prompt}); err != nil {
				t.Fatal(err)
			}
			// Old Lua completed, but its SQL transaction failed. Its marker was '1',
			// regardless of whether it deducted 50 or refunded 50.
			if err := s.rdb.Set(ctx, BalanceKey(tenant), tc.balance, 0).Err(); err != nil {
				t.Fatal(err)
			}
			if err := s.rdb.Set(ctx, DeltaGuardKey(req.RequestId), "1", time.Hour).Err(); err != nil {
				t.Fatal(err)
			}
			resp, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
			if err != nil || resp.GetDeltaQuota() != tc.delta {
				t.Fatalf("settle delta=%d err=%v want %d", resp.GetDeltaQuota(), err, tc.delta)
			}
			ledger, err := LedgerBalance(ctx, s.db, tenant)
			if err != nil || ledger != tc.balance {
				t.Fatalf("ledger=%d err=%v want %d", ledger, err, tc.balance)
			}
			if bal, _ := s.getBalance(ctx, tenant); bal != tc.balance {
				t.Fatalf("balance=%d want %d (no second delta)", bal, tc.balance)
			}
		})
	}
}

func TestDeltaOneReplayPreservesAppliedAmount(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1)
	// New Lua collects only one cent for a requested fifty. Replaying must
	// retain the collected one cent, even when the retry requests fifty.
	if bal, applied, err := s.applyDelta(ctx, tenant, "rid-new-one", 50); err != nil || bal != 0 || applied != 1 {
		t.Fatalf("first bal=%d applied=%d err=%v", bal, applied, err)
	}
	if bal, applied, err := s.applyDelta(ctx, tenant, "rid-new-one", 50); err != nil || bal != 0 || applied != 1 {
		t.Fatalf("replay bal=%d applied=%d err=%v", bal, applied, err)
	}
}

// A lost MySQL session releases GET_LOCK even while its caller is still
// handling the failed insert. Compensation must check under a fresh lock.
func TestReserveLostConnectionCannotRefundCommittedReplay(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	req := reserveReq("rid-killed-reserve", tenant, 84)
	entered, resume := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	if err := s.db.Callback().Create().Before("gorm:create").Register("test:kill_reserve", func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" || calls.Add(1) != 1 {
			return
		}
		var id uint64
		if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
			tx.AddError(err)
			close(entered)
			return
		}
		if err := s.db.Exec(fmt.Sprintf("KILL CONNECTION %d", id)).Error; err != nil {
			tx.AddError(err)
			close(entered)
			return
		}
		close(entered)
		<-resume
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Create().Remove("test:kill_reserve") })
	first := make(chan error, 1)
	go func() { _, err := s.Reserve(ctx, req); first <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first reserve did not reach insert")
	}
	second, err := NewStore(s.db, s.rdb).Reserve(ctx, req)
	close(resume)
	if err != nil || second.GetCode() != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("replay=%v err=%v", second, err)
	}
	if err := <-first; err == nil {
		t.Fatal("killed first insert should fail")
	}
	if bal, err := s.getBalance(ctx, tenant); err != nil || bal != 900 {
		t.Fatalf("balance=%d err=%v want900, replay reserve committed", bal, err)
	}
	if ledger, err := LedgerBalance(ctx, s.db, tenant); err != nil || ledger != 900 {
		t.Fatalf("ledger=%d err=%v want900", ledger, err)
	}
}

// Recovery itself can lose its session after verifying that the ledger is
// empty. A replay then commits before the stale compensator reaches Redis.
func TestReserveRecoveryLostConnectionCannotRefundNewOwner(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	req := reserveReq("rid-recovery-killed", tenant, 84)
	var inserts, queries atomic.Int32
	if err := s.db.Callback().Create().Before("gorm:create").Register("test:fail_first_owner", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" && inserts.Add(1) == 1 {
			tx.AddError(errors.New("first insert fails"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Create().Remove("test:fail_first_owner") })
	replay := make(chan error, 1)
	if err := s.db.Callback().Query().After("gorm:query").Register("test:kill_recovery", func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" || queries.Add(1) != 3 {
			return
		}
		var id uint64
		if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
			replay <- err
			return
		}
		if err := s.db.Exec(fmt.Sprintf("KILL CONNECTION %d", id)).Error; err != nil {
			replay <- err
			return
		}
		resp, err := NewStore(s.db, s.rdb).Reserve(ctx, req)
		if err == nil && resp.GetCode() != billingv1.ReserveCode_RESERVE_OK {
			err = fmt.Errorf("replay code %v", resp.GetCode())
		}
		replay <- err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Query().Remove("test:kill_recovery") })
	if _, err := s.Reserve(ctx, req); err == nil {
		t.Fatal("first insert must fail")
	}
	select {
	case err := <-replay:
		if err != nil {
			t.Fatalf("recovery replay: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery lookup was not intercepted")
	}
	if bal, err := s.getBalance(ctx, tenant); err != nil || bal != 900 {
		t.Fatalf("balance=%d err=%v want900 after replay committed", bal, err)
	}
	if ledger, err := LedgerBalance(ctx, s.db, tenant); err != nil || ledger != 900 {
		t.Fatalf("ledger=%d err=%v want900", ledger, err)
	}
}
