package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	redis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type cashRedisHook struct {
	before func(context.Context, redis.Cmder) error
}

func (h cashRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h cashRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h cashRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.before(ctx, cmd); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}
func changesCash(cmd redis.Cmder, tenant uint64) bool {
	if cmd.Name() != "set" && cmd.Name() != "eval" && cmd.Name() != "evalsha" {
		return false
	}
	for _, arg := range cmd.Args()[1:] {
		if fmt.Sprint(arg) == BalanceKey(tenant) {
			return true
		}
	}
	return false
}

// Killing the session after its ledger read must not let an old rebuild
// overwrite a subsequent holder's debit.
func TestLostRebuildSessionCannotOverwriteNewReserve(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	s.rdb.AddHook(cashRedisHook{before: func(_ context.Context, cmd redis.Cmder) error {
		if changesCash(cmd, tenant) && once.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
		return nil
	}})
	rebuilt := make(chan error, 1)
	go func() { _, err := s.RebuildBalance(ctx, tenant); rebuilt <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("rebuild never reached Redis write")
	}
	var owner sql.NullInt64
	if err := s.db.Raw("SELECT IS_USED_LOCK(?)", fmt.Sprintf("billing:cash:%d", tenant)).Scan(&owner).Error; err != nil {
		close(resume)
		t.Fatal(err)
	}
	if owner.Valid {
		if err := s.db.Exec(fmt.Sprintf("KILL CONNECTION %d", owner.Int64)).Error; err != nil {
			close(resume)
			t.Fatal(err)
		}
	}
	rdb := redis.NewClient(&redis.Options{Addr: s.rdb.Options().Addr, DB: s.rdb.Options().DB})
	defer rdb.Close()
	_, err := NewStore(s.db, rdb).Reserve(ctx, reserveReq("reserve-after-lost-rebuild", tenant, 84))
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-rebuilt; err == nil {
		t.Error("stale rebuild reported success after its session was replaced")
	}
	if live, err := s.getBalance(ctx, tenant); err != nil || live != 900 {
		t.Fatalf("balance=%d err=%v, want 900 after replacement reserve", live, err)
	}
}

func TestCashMutationAfterRefundProjectionFailure(t *testing.T) {
	for _, operation := range []string{"reserve", "settle", "settle_debit", "reverse"} {
		t.Run(operation, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx := context.Background()
			seedMockModel(t, s)
			tenant := seedTenant(t, s, 1000)
			req := reserveReq("pending-before-refund", tenant, 84)
			if operation != "reserve" {
				if _, err := s.Reserve(ctx, req); err != nil {
					t.Fatal(err)
				}
				prompt := int64(50)
				if operation == "settle_debit" {
					prompt = 150
				}
				if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: prompt}); err != nil {
					t.Fatal(err)
				}
			}
			var fail atomic.Bool
			fail.Store(true)
			s.rdb.AddHook(cashRedisHook{before: func(_ context.Context, cmd redis.Cmder) error {
				if fail.Load() && changesCash(cmd, tenant) {
					return errors.New("injected refund projection failure")
				}
				return nil
			}})
			amount := int64(10)
			if operation != "reserve" {
				amount = 9
			}
			if _, err := s.RefundAmount(ctx, tenant, amount, "refund-before-next-mutation"); err == nil {
				t.Fatal("expected projection error")
			}
			if operation == "reserve" {
				if _, err := s.Reserve(ctx, reserveReq("blocked-during-projection-failure", tenant, 84)); err == nil {
					t.Fatal("reserve must fail while authoritative projection repair is unavailable")
				}
			}
			fail.Store(false)
			switch operation {
			case "reserve":
				r, err := s.Reserve(ctx, reserveReq("new-after-refund", tenant, 84))
				if err != nil || r.Code != billingv1.ReserveCode_RESERVE_INSUFFICIENT {
					t.Fatalf("reserve=%+v err=%v, refunded money must not be spendable", r, err)
				}
			case "settle", "settle_debit":
				if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId}); err != nil {
					t.Fatal(err)
				}
			case "reverse":
				if _, err := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId}); err != nil {
					t.Fatal(err)
				}
			}
			ledger, err := LedgerBalance(ctx, s.db, tenant)
			if err != nil {
				t.Fatal(err)
			}
			live, err := s.getBalance(ctx, tenant)
			if err != nil || live != ledger || ledger < 0 {
				t.Fatalf("live=%d ledger=%d err=%v", live, ledger, err)
			}
		})
	}
}

func TestCashProjectionFailureRetainsCommittedLedger(t *testing.T) {
	for _, operation := range []string{"pay", "refund_order", "refund_amount"} {
		t.Run(operation, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx := context.Background()
			tenant := seedTenant(t, s, 1000)
			o, err := s.CreateRechargeOrder(ctx, tenant, 100, "projection-failure-order")
			if err != nil {
				t.Fatal(err)
			}
			if operation != "pay" {
				if _, err := s.PayRecharge(ctx, o.OrderNo); err != nil {
					t.Fatal(err)
				}
			}
			var fail atomic.Bool
			fail.Store(true)
			s.rdb.AddHook(cashRedisHook{before: func(_ context.Context, cmd redis.Cmder) error {
				if fail.Load() && changesCash(cmd, tenant) {
					return errors.New("injected projection failure")
				}
				return nil
			}})
			execute := func() error {
				var err error
				switch operation {
				case "pay":
					_, err = s.PayRecharge(ctx, o.OrderNo)
				case "refund_order":
					_, err = s.RefundRecharge(ctx, tenant, o.OrderNo)
				case "refund_amount":
					_, err = s.RefundAmount(ctx, tenant, 100, "projection-failure-refund")
				}
				return err
			}
			if err := execute(); err == nil {
				t.Fatal("projection failure was not reported")
			}
			wantLedger := int64(1000)
			wantStatus := RechargePaid
			wantRows := int64(2)
			if operation == "pay" {
				wantLedger = 11000
				wantRows = 1
			}
			if operation == "refund_order" {
				wantStatus = RechargeRefunded
			}
			ledger, err := LedgerBalance(ctx, s.db, tenant)
			if err != nil || ledger != wantLedger {
				t.Fatalf("committed ledger=%d err=%v, want %d retained", ledger, err, wantLedger)
			}
			stored, err := s.rechargeByOrderNo(ctx, o.OrderNo)
			if err != nil || stored.Status != wantStatus {
				t.Fatalf("order=%+v err=%v, want committed status %s", stored, err, wantStatus)
			}
			fail.Store(false)
			if err := execute(); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if live, err := s.getBalance(ctx, tenant); err != nil || live != wantLedger {
				t.Fatalf("repaired balance=%d err=%v, want %d", live, err, wantLedger)
			}
			var count int64
			if err := s.db.Model(&Topup{}).Where("tenant_id = ?", tenant).Count(&count).Error; err != nil || count != wantRows {
				t.Fatalf("topups=%d err=%v, want %d without retry duplicates", count, err, wantRows)
			}
		})
	}
}

// Rebuilding between the Redis debit and its SQL commit must not restore money
// that is still reserved, or double-refund a debit whose insert later fails.
func TestRebuildWaitsForInFlightReserve(t *testing.T) {
	for _, failInsert := range []bool{false, true} {
		name := "committed"
		if failInsert {
			name = "compensated"
		}
		t.Run(name, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx := context.Background()
			seedMockModel(t, s)
			tenant := seedTenant(t, s, 1000)
			entered, resume := make(chan struct{}), make(chan struct{})
			var once atomic.Bool
			if err := s.db.Callback().Create().Before("gorm:create").Register("test:pause_cash_reserve", func(tx *gorm.DB) {
				if tx.Statement.Table == "bills" && once.CompareAndSwap(false, true) {
					close(entered)
					<-resume
					if failInsert {
						tx.AddError(errors.New("injected reserve insert failure"))
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.db.Callback().Create().Remove("test:pause_cash_reserve") })
			reserved := make(chan error, 1)
			go func() { _, err := s.Reserve(ctx, reserveReq("reserve-vs-rebuild", tenant, 84)); reserved <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(resume)
				t.Fatal("reserve never reached the SQL insert after Redis debit")
			}
			rebuilt := make(chan error, 1)
			go func() { _, err := NewStore(s.db, s.rdb).RebuildBalance(ctx, tenant); rebuilt <- err }()
			finished, waitErr := cashOperationFinishedOrWaiting(s, rebuilt)
			close(resume)
			if err := <-reserved; (err != nil) != failInsert {
				t.Fatalf("reserve err=%v, injected failure=%v", err, failInsert)
			}
			if !finished {
				waitErr = <-rebuilt
			}
			if waitErr != nil {
				t.Fatal(waitErr)
			}
			want := int64(900)
			if failInsert {
				want = 1000
			}
			live, err := s.getBalance(ctx, tenant)
			if err != nil || live != want {
				t.Fatalf("Redis balance=%d err=%v, want %d after reserve and rebuild", live, err, want)
			}
			ledger, err := LedgerBalance(ctx, s.db, tenant)
			if err != nil || ledger != want {
				t.Fatalf("ledger balance=%d err=%v, want %d", ledger, err, want)
			}
		})
	}
}

func TestRebuildWaitsForInFlightTerminalMutation(t *testing.T) {
	for _, operation := range []string{"settle", "reverse"} {
		t.Run(operation, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx := context.Background()
			seedMockModel(t, s)
			tenant := seedTenant(t, s, 1000)
			req := reserveReq("terminal-vs-rebuild", tenant, 84)
			if _, err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50}); err != nil {
				t.Fatal(err)
			}
			entered, resume := make(chan struct{}), make(chan struct{})
			var once atomic.Bool
			pause := func(tx *gorm.DB) {
				if tx.Statement.Table == "bills" && once.CompareAndSwap(false, true) {
					close(entered)
					<-resume
				}
			}
			if operation == "settle" {
				if err := s.db.Callback().Create().Before("gorm:create").Register("test:pause_terminal", pause); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.db.Callback().Create().Remove("test:pause_terminal") })
			} else {
				if err := s.db.Callback().Update().Before("gorm:update").Register("test:pause_terminal", pause); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.db.Callback().Update().Remove("test:pause_terminal") })
			}
			mutated := make(chan error, 1)
			go func() {
				var err error
				if operation == "settle" {
					_, err = s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
				} else {
					_, err = s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId})
				}
				mutated <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(resume)
				t.Fatal("terminal mutation never reached SQL after applying Redis delta")
			}
			rebuilt := make(chan error, 1)
			go func() { _, err := NewStore(s.db, s.rdb).RebuildBalance(ctx, tenant); rebuilt <- err }()
			finished, err := cashOperationFinishedOrWaiting(s, rebuilt)
			close(resume)
			if mutationErr := <-mutated; mutationErr != nil {
				t.Fatal(mutationErr)
			}
			if !finished {
				err = <-rebuilt
			}
			if err != nil {
				t.Fatal(err)
			}
			want := int64(950)
			if operation == "reverse" {
				want = 1000
			}
			live, err := s.getBalance(ctx, tenant)
			if err != nil || live != want {
				t.Fatalf("balance=%d err=%v, want %d after %s and rebuild", live, err, want, operation)
			}
		})
	}
}

// SQL is the durable idempotency authority, even after Redis's 24h guard expires.
func TestSettleReplayAfterGuardExpiryDoesNotDebitAgain(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	req := reserveReq("settle-expired-guard", tenant, 84)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 150}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId}); err != nil {
		t.Fatal(err)
	}
	if err := s.rdb.Del(ctx, DeltaGuardKey(req.RequestId)).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(s.db, s.rdb).Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId}); err != nil {
		t.Fatal(err)
	}
	if live, err := s.getBalance(ctx, tenant); err != nil || live != 850 {
		t.Fatalf("balance=%d err=%v, want 850 after durable settle replay", live, err)
	}
}

func TestConcurrentRefundsCannotSpendSameLedgerBalance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	if err := s.db.Callback().Create().Before("gorm:create").Register("test:pause_refund", func(tx *gorm.DB) {
		if tx.Statement.Table == "topups" && once.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Create().Remove("test:pause_refund") })
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := s.RefundAmount(ctx, tenant, 10, "refund-first"); first <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("first refund never reached insert")
	}
	go func() { _, err := NewStore(s.db, s.rdb).RefundAmount(ctx, tenant, 10, "refund-second"); second <- err }()
	finished, secondErr := cashOperationFinishedOrWaiting(s, second)
	close(resume)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !finished {
		secondErr = <-second
	}
	if secondErr == nil {
		t.Error("second refund spent money already refunded by the first")
	}
	if ledger, err := LedgerBalance(ctx, s.db, tenant); err != nil || ledger != 0 {
		t.Fatalf("ledger=%d err=%v, want 0", ledger, err)
	}
	if live, err := s.getBalance(ctx, tenant); err != nil || live != 0 {
		t.Fatalf("balance=%d err=%v, want 0", live, err)
	}
}

func TestCashOperationsWithSingleDatabaseConnection(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	db, err := s.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := reserveReq("single-connection-reserve", tenant, 84)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MeOverview(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateRechargeOrder(ctx, tenant, 10, "single-connection-pay")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PayRecharge(ctx, o.OrderNo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RefundRecharge(ctx, tenant, o.OrderNo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId}); err != nil {
		t.Fatal(err)
	}
	if live, err := s.RebuildBalance(ctx, tenant); err != nil || live != 1000 {
		t.Fatalf("balance=%d err=%v, want 1000", live, err)
	}
}

func TestCashCancellationBeforeMutationDoesNotCommit(t *testing.T) {
	for _, operation := range []string{"pay", "refund_order", "refund_amount", "settle", "reverse"} {
		t.Run(operation, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seedMockModel(t, s)
			tenant := seedTenant(t, s, 1000)
			o, err := s.CreateRechargeOrder(ctx, tenant, 10, "cancel-before-mutation")
			if err != nil {
				t.Fatal(err)
			}
			if operation == "refund_order" || operation == "refund_amount" {
				if _, err := s.PayRecharge(ctx, o.OrderNo); err != nil {
					t.Fatal(err)
				}
			}
			req := reserveReq("cancel-terminal", tenant, 84)
			if operation == "settle" || operation == "reverse" {
				if _, err := s.Reserve(ctx, req); err != nil {
					t.Fatal(err)
				}
				if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50}); err != nil {
					t.Fatal(err)
				}
			}
			want, err := LedgerBalance(ctx, s.db, tenant)
			if err != nil {
				t.Fatal(err)
			}
			table, nth := "recharge_orders", int32(1)
			switch operation {
			case "pay":
				nth = 2
			case "refund_amount":
				table = "topups"
			case "settle", "reverse":
				table = "bills"
				nth = 2
			}
			var calls atomic.Int32
			if err := s.db.Callback().Query().After("gorm:query").Register("test:cancel_cash", func(tx *gorm.DB) {
				if tx.Statement.Table == table && calls.Add(1) == nth {
					cancel()
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.db.Callback().Query().Remove("test:cancel_cash") })
			switch operation {
			case "pay":
				_, err = s.PayRecharge(ctx, o.OrderNo)
			case "refund_order":
				_, err = s.RefundRecharge(ctx, tenant, o.OrderNo)
			case "refund_amount":
				_, err = s.RefundAmount(ctx, tenant, 10, "cancel-amount")
			case "settle":
				_, err = s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
			case "reverse":
				_, err = s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId})
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("err=%v, want cancellation before financial mutation", err)
			}
			if ledger, err := LedgerBalance(context.Background(), s.db, tenant); err != nil || ledger != want {
				t.Fatalf("ledger=%d err=%v, want unchanged %d", ledger, err, want)
			}
			if live, err := s.getBalance(context.Background(), tenant); err != nil || live != want {
				t.Fatalf("balance=%d err=%v, want unchanged %d", live, err, want)
			}
		})
	}
}

func TestTerminalSQLFailureRebuildAndRetry(t *testing.T) {
	for _, operation := range []string{"settle", "reverse"} {
		t.Run(operation, func(t *testing.T) {
			s, done := newTestStore(t)
			defer done()
			ctx := context.Background()
			seedMockModel(t, s)
			tenant := seedTenant(t, s, 1000)
			req := reserveReq("failed-terminal-rebuild", tenant, 84)
			if _, err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50}); err != nil {
				t.Fatal(err)
			}
			fail := func(tx *gorm.DB) {
				if tx.Statement.Table == "bills" {
					tx.AddError(errors.New("injected terminal SQL failure"))
				}
			}
			if operation == "settle" {
				if err := s.db.Callback().Create().Before("gorm:create").Register("test:fail_terminal", fail); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.db.Callback().Update().Before("gorm:update").Register("test:fail_terminal", fail); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				s.db.Callback().Create().Remove("test:fail_terminal")
				s.db.Callback().Update().Remove("test:fail_terminal")
			})
			execute := func() error {
				var err error
				if operation == "settle" {
					_, err = s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
				} else {
					_, err = s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: req.RequestId})
				}
				return err
			}
			if err := execute(); err == nil {
				t.Fatal("injected SQL failure was ignored")
			}
			s.db.Callback().Create().Remove("test:fail_terminal")
			s.db.Callback().Update().Remove("test:fail_terminal")
			if bal, err := s.RebuildBalance(ctx, tenant); err != nil || bal != 900 {
				t.Fatalf("pending ledger repair=%d err=%v, want900", bal, err)
			}
			if err := execute(); err != nil {
				t.Fatal(err)
			}
			want := int64(950)
			if operation == "reverse" {
				want = 1000
			}
			if bal, err := s.getBalance(ctx, tenant); err != nil || bal != want {
				t.Fatalf("retry balance=%d err=%v, want%d", bal, err, want)
			}
			if bal, err := LedgerBalance(ctx, s.db, tenant); err != nil || bal != want {
				t.Fatalf("retry ledger=%d err=%v, want%d", bal, err, want)
			}
		})
	}
}

func TestEnsureBalanceKeysUsesLedgerAndPreservesExistingKey(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	payOrder(t, s, ctx, tenant, 10, "startup-rebuild-topup")
	req := reserveReq("startup-rebuild-reserve", tenant, 84)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId}); err != nil {
		t.Fatal(err)
	}
	if err := s.rdb.Del(ctx, BalanceKey(tenant)).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBalanceKeys(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, err := s.getBalance(ctx, tenant); err != nil || bal != 1950 {
		t.Fatalf("restored balance=%d err=%v, want1950 including topup and charge", bal, err)
	}
	if err := s.rdb.Set(ctx, BalanceKey(tenant), 1234, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBalanceKeys(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, err := s.getBalance(ctx, tenant); err != nil || bal != 1234 {
		t.Fatalf("existing balance=%d err=%v, want1234 untouched", bal, err)
	}
}

func TestEnsureBalanceKeysRechecksAfterConcurrentReserve(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	if err := s.rdb.Del(ctx, BalanceKey(tenant)).Err(); err != nil {
		t.Fatal(err)
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	// Pause after EXISTS returned missing, before startup may acquire the lock.
	s.rdb.AddHook(afterCashRedisHook{after: func(cmd redis.Cmder) {
		if cmd.Name() == "exists" && fmt.Sprint(cmd.Args()[1]) == BalanceKey(tenant) && once.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
	}})
	ensured := make(chan error, 1)
	go func() { ensured <- s.EnsureBalanceKeys(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("startup never checked missing balance")
	}
	rdb := redis.NewClient(&redis.Options{Addr: s.rdb.Options().Addr, DB: s.rdb.Options().DB})
	defer rdb.Close()
	other := NewStore(s.db, rdb)
	_, rebuildErr := other.RebuildBalance(ctx, tenant)
	_, reserveErr := other.Reserve(ctx, reserveReq("reserve-during-startup", tenant, 84))
	close(resume)
	if rebuildErr != nil || reserveErr != nil {
		t.Fatalf("rebuild=%v reserve=%v", rebuildErr, reserveErr)
	}
	if err := <-ensured; err != nil {
		t.Fatal(err)
	}
	if bal, err := s.getBalance(ctx, tenant); err != nil || bal != 900 {
		t.Fatalf("balance=%d err=%v, want900 after concurrent startup", bal, err)
	}
}

type afterCashRedisHook struct{ after func(redis.Cmder) }

func (h afterCashRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h afterCashRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h afterCashRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil {
			h.after(cmd)
		}
		return err
	}
}

// Observe actual server lock contention rather than relying on a scheduling
// sleep. A broken implementation finishes before the paused operation resumes.
func cashOperationFinishedOrWaiting(s *Store, result <-chan error) (bool, error) {
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return true, err
		case <-ticker.C:
			var waiting int64
			if err := s.db.Raw("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE STATE = 'User lock' AND INFO LIKE '%GET_LOCK%'").Scan(&waiting).Error; err != nil {
				return true, err
			}
			if waiting > 0 {
				return false, nil
			}
		case <-deadline.C:
			return true, errors.New("cash operation neither finished nor waited on its database lock")
		}
	}
}
