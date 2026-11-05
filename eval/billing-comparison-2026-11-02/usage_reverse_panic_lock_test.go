package comparison

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReversePanicReleasesRequestLock(t *testing.T) {
	if os.Getenv("TEST_USAGE_REVERSE_PANIC") != "1" {
		t.Skip("test-only candidate")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reverse-panic-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("test:panic_reverse_update", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" {
			panic("injected reverse panic")
		}
	}); err != nil {
		t.Fatal(err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "recon:no_usage"})
	}()
	if err := db.Callback().Update().Remove("test:panic_reverse_update"); err != nil {
		t.Fatal(err)
	}
	if recovered != "injected reverse panic" {
		t.Fatalf("panic=%v", recovered)
	}
	lockName := fmt.Sprintf("billing:reserve:%x", sha256.Sum256([]byte(id)))[:64]
	var owner sql.NullInt64
	if err := db.Raw("SELECT IS_USED_LOCK(?)", lockName).Scan(&owner).Error; err != nil || owner.Valid {
		t.Fatalf("request lock owner=%v err=%v", owner, err)
	}
	// The failed operation did not commit. A later legitimate usage/settle repairs the orphaned Redis guard.
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
	if err != nil || !result.Settled {
		t.Fatal(result, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	ledger, le := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || le != nil || live != 460 || ledger != 460 {
		t.Fatalf("balance=%d ledger=%d err=%v/%v", live, ledger, err, le)
	}
	t.Log("panic_observed=true request_lock_released=true usage_and_settlement=success balance=460 ledger=460")
}

func TestUsageReverseCandidateDifferentReasonBypassesGuard(t *testing.T) {
	if os.Getenv("TEST_USAGE_REVERSE_PANIC") != "1" {
		t.Skip("test-only candidate")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s, ctx := store.NewStore(db, r), context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("reverse-reason-bypass-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 500, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Reverse(ctx, &pb.ReverseRequest{RequestId: id, Reason: "manual"})
	if err != nil || !result.Reversed {
		t.Fatal(result, err)
	}
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil || bill.Status != store.StatusReversed || bill.UsageReportedAt == nil {
		t.Fatal(bill.Status, err)
	}
	settled, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id})
	if err != nil || settled.Settled {
		t.Fatal(settled, err)
	}
	t.Log("usage_committed=true reason=manual reverse=true subsequent_settle=false; demonstrates bypass")
}
