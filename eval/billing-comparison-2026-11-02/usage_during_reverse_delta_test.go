package comparison

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Observe the actual Store with a controlled usage commit after the Redis
// refund but before the reverse SQL UPDATE. PASS means the risk reproduced.
func TestUsageCommitDuringReverseDelta(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, redis), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("delta-usage-race-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	reserved, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || reserved.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(reserved, err)
	}
	var injected atomic.Bool
	var callbackErr error
	var during int64
	if err := db.Callback().Update().Before("gorm:update").Register("test:usage_after_refund", func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" || !injected.CompareAndSwap(false, true) {
			return
		}
		during, callbackErr = redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if callbackErr == nil && os.Getenv("TEST_DROP_DELTA_GUARD") == "1" {
			callbackErr = redis.Del(ctx, store.DeltaGuardKey(id)).Err()
		}
		if callbackErr == nil {
			callbackErr = s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40})
		}
		if callbackErr == nil && os.Getenv("TEST_DROP_CASH_FENCE") == "1" {
			callbackErr = redis.Del(ctx, fmt.Sprintf("billing:cash-owner:%d", tenant.ID)).Err()
		}
		if callbackErr != nil {
			tx.AddError(callbackErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove("test:usage_after_refund")
	reversed, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "controlled-test"})
	if os.Getenv("TEST_EXPECT_USAGE_GUARD") == "1" {
		expected := codes.FailedPrecondition
		if os.Getenv("TEST_DROP_CASH_FENCE") == "1" {
			expected = codes.Unavailable
		}
		if status.Code(err) != expected || callbackErr != nil || !injected.Load() {
			t.Fatalf("expected guarded rejection: reverse=%v err=%v callback=%v", reversed, err, callbackErr)
		}
		var bill store.Bill
		if err := db.First(&bill, reserved.BillId).Error; err != nil {
			t.Fatal(err)
		}
		live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil {
			t.Fatal(err)
		}
		var reverseEvents int64
		if err := db.Model(&store.OutboxEvent{}).Where("event_key = ?", id+":reverse").Count(&reverseEvents).Error; err != nil {
			t.Fatal(err)
		}
		if during != 1000 || live != ledger || live >= 1000 || bill.Status != store.StatusPending || bill.UsageReportedAt == nil || reverseEvents != 0 {
			t.Fatalf("during=%d balance=%d ledger=%d bill=%+v reverse_events=%d", during, live, ledger, bill, reverseEvents)
		}
		settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
		if err != nil || !settled.Settled {
			t.Fatalf("settle=%v err=%v", settled, err)
		}
		live, err = redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || live != 960 || ledger != live {
			t.Fatalf("settled balance=%d ledger=%d err=%v", live, ledger, err)
		}
		t.Logf("refund_rejected=true projection_repaired=true usage_settled=true balance=%d ledger=%d", live, ledger)
		return
	}
	if err != nil || !reversed.Reversed || callbackErr != nil || !injected.Load() {
		t.Fatalf("reverse=%v err=%v callback=%v", reversed, err, callbackErr)
	}
	var bill store.Bill
	if err := db.First(&bill, reserved.BillId).Error; err != nil {
		t.Fatal(err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":reverse", id + ":settle"}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
	if err != nil {
		t.Fatal(err)
	}
	if during != 1000 || live != 1000 || ledger != 1000 || bill.Status != store.StatusReversed || bill.UsageReportedAt == nil || events != 3 || settled.Settled {
		t.Fatalf("during=%d live=%d ledger=%d bill=%+v events=%d settled=%v", during, live, ledger, bill, events, settled)
	}
	t.Logf("redis_refund_before_sql=%d usage_committed=true reversed=true settled=false balance=%d ledger=%d events=%d", during, live, ledger, events)
}

// Verify rollback after Redis refund even when the associated guard disappears.
func TestReverseRollbackMissingGuard(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, redis), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reverse-rollback-guard-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(res, err)
	}
	var injected atomic.Bool
	if err := db.Callback().Update().Before("gorm:update").Register("test:rollback_guard", func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" || !injected.CompareAndSwap(false, true) {
			return
		}
		if os.Getenv("TEST_DROP_DELTA_GUARD") == "1" {
			if err := redis.Del(ctx, store.DeltaGuardKey(id)).Err(); err != nil {
				tx.AddError(err)
				return
			}
		}
		tx.AddError(errors.New("injected SQL rollback"))
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
	if status.Code(err) != codes.Unavailable || !injected.Load() {
		t.Fatalf("reverse error=%v injected=%v", err, injected.Load())
	}
	if err := db.Callback().Update().Remove("test:rollback_guard"); err != nil {
		t.Fatal(err)
	}
	var bill store.Bill
	if err := db.First(&bill, res.BillId).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 900 || ledger != 900 || bill.Status != store.StatusPending || count != 0 {
		t.Fatalf("balance=%d ledger=%d status=%s reverse_events=%d err=%v", live, ledger, bill.Status, count, err)
	}
	got, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id})
	if err != nil || !got.Reversed {
		t.Fatal(got, err)
	}
	live, err = redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 1000 || ledger != 1000 {
		t.Fatalf("retry balance=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Log("rollback_projection_consistent=true retry_reversed=true balance=1000 ledger=1000")
}

func TestSettleRollbackMissingGuard(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, redis), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("settle-rollback-guard-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(res, err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	var injected atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("test:settle_rollback_guard", func(tx *gorm.DB) {
		if tx.Statement.Table != "bills" || !injected.CompareAndSwap(false, true) {
			return
		}
		if os.Getenv("TEST_DROP_DELTA_GUARD") == "1" {
			if err := redis.Del(ctx, store.DeltaGuardKey(id)).Err(); err != nil {
				tx.AddError(err)
				return
			}
		}
		tx.AddError(errors.New("injected SQL rollback"))
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Settle(ctx, &pb.SettleRequest{RequestId: id})
	if status.Code(err) != codes.Unavailable || !injected.Load() {
		t.Fatalf("settle error=%v injected=%v", err, injected.Load())
	}
	if err := db.Callback().Create().Remove("test:settle_rollback_guard"); err != nil {
		t.Fatal(err)
	}
	var bill store.Bill
	if err := db.First(&bill, res.BillId).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":settle").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 900 || ledger != 900 || bill.Status != store.StatusPending || count != 0 {
		t.Fatalf("balance=%d ledger=%d status=%s settle_events=%d err=%v", live, ledger, bill.Status, count, err)
	}
	got, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
	if err != nil || !got.Settled {
		t.Fatal(got, err)
	}
	live, err = redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 960 || ledger != 960 {
		t.Fatalf("retry balance=%d ledger=%d err=%v", live, ledger, err)
	}
	t.Log("rollback_projection_consistent=true retry_settled=true balance=960 ledger=960")
}

func TestUsageAtomicNormalReverse(t *testing.T) {
	db, redis := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, redis), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("atomic-normal-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := redis.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || res.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(res, err)
	}
	for i := 0; i < 2; i++ {
		got, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "test"})
		if err != nil || !got.Reversed || got.RefundQuota != 100 {
			t.Fatal(got, err)
		}
	}
	var bill store.Bill
	if err := db.First(&bill, res.BillId).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reverse").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	live, err := redis.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 1000 || ledger != live || bill.Status != store.StatusReversed || count != 1 {
		t.Fatalf("balance=%d ledger=%d status=%s events=%d err=%v", live, ledger, bill.Status, count, err)
	}
	t.Log("normal_reverse_and_replay=true reverse_events=1 balance=1000 ledger=1000")
}
