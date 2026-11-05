package comparison

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestLedgerOverflowFailsBeforeRedisProjectionWrite(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("ledger-overflow-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: math.MaxInt64 - 10}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	topup := store.Topup{TenantID: tenant.ID, Source: "test", Amount: 100, RefType: "test", RefID: id}
	if err := db.Create(&topup).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 123, 0).Err(); err != nil {
		t.Fatal(err)
	}
	ledger, ledgerErr := store.LedgerBalance(ctx, db, tenant.ID)
	rebuilt, rebuildErr := s.RebuildBalance(ctx, tenant.ID)
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	if ledgerErr == nil || rebuildErr == nil || live != 123 {
		t.Fatalf("ledger=%d ledgerErr=%v rebuilt=%d rebuildErr=%v redis=%d", ledger, ledgerErr, rebuilt, rebuildErr, live)
	}
	t.Logf("out-of-range ledger rejected; pre-existing projection unchanged=%d", live)
}

func TestLedgerIntermediateOverflowFinalInRange(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("ledger-intermediate-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: math.MaxInt64 - 10}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.Topup{TenantID: tenant.ID, Source: "test", Amount: 100, RefType: "test", RefID: id}).Error; err != nil {
		t.Fatal(err)
	}
	row := store.Bill{RequestID: id, Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: store.FundingBalance, PreQuota: 100}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != tenant.InitialQuota {
		t.Fatalf("ledger=%d want=%d err=%v", ledger, tenant.InitialQuota, err)
	}
	t.Logf("intermediate overflow but exact final balance=%d", ledger)
}
