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

func TestLedgerSingleCorrectness(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, nil)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("ledger-single-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: prefix, InitialQuota: 1000}
	other := store.Tenant{Name: prefix + "-other", InitialQuota: 400}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	topups := []store.Topup{{TenantID: tenant.ID, Source: "recharge", Amount: 100, RefType: "test", RefID: prefix + "-topup"}, {TenantID: tenant.ID, Source: "refund", Amount: -30, RefType: "test", RefID: prefix + "-refund"}, {TenantID: other.ID, Source: "recharge", Amount: 999, RefType: "test", RefID: prefix + "-other"}}
	if err := db.Create(&topups).Error; err != nil {
		t.Fatal(err)
	}
	actual := int64(40)
	ignored := int64(300)
	bills := []store.Bill{{RequestID: prefix + "-a", TenantID: tenant.ID, Phase: store.PhaseReserve, Status: store.StatusPending, PreQuota: 80}, {RequestID: prefix + "-b", TenantID: tenant.ID, Phase: store.PhaseSettle, Status: store.StatusSettled, ActualQuota: &actual}, {RequestID: prefix + "-c", TenantID: tenant.ID, Phase: store.PhaseReserve, Status: store.StatusReversed, PreQuota: 200}, {RequestID: prefix + "-d", TenantID: tenant.ID, Phase: store.PhaseSettle, Status: store.StatusPending, ActualQuota: &ignored}, {RequestID: prefix + "-e", TenantID: other.ID, Phase: store.PhaseReserve, Status: store.StatusPending, PreQuota: 99}}
	if err := db.Create(&bills).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   uint64
		want int64
	}{{tenant.ID, 950}, {other.ID, 1300}, {1000000000000, 0}} {
		got, err := store.LedgerBalance(ctx, db, tc.id)
		if err != nil || got != tc.want {
			t.Fatalf("tenant=%d got=%d want=%d err=%v", tc.id, got, tc.want, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.LedgerBalance(canceled, db, tenant.ID); err == nil {
		t.Fatal("canceled query succeeded")
	}
	overflow := store.Tenant{Name: prefix + "-overflow", InitialQuota: 1}
	if err := db.Create(&overflow).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.Topup{TenantID: overflow.ID, Source: "test", Amount: math.MaxInt64, RefType: "test", RefID: prefix + "-overflow"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.LedgerBalance(ctx, db, overflow.ID); err == nil {
		t.Fatal("final int64 overflow accepted")
	}
	sumOverflow := store.Tenant{Name: prefix + "-sum-overflow", InitialQuota: 0}
	if err := db.Create(&sumOverflow).Error; err != nil {
		t.Fatal(err)
	}
	rows := []store.Topup{{TenantID: sumOverflow.ID, Source: "test", Amount: math.MaxInt64, RefType: "test", RefID: prefix + "-sum1"}, {TenantID: sumOverflow.ID, Source: "test", Amount: 1, RefType: "test", RefID: prefix + "-sum2"}}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.LedgerBalance(ctx, db, sumOverflow.ID); err == nil {
		t.Fatal("aggregated topup overflow accepted")
	}
	t.Log("cash formula, unrelated tenant, empty tenant, cancellation, final and aggregate overflow verified")
}
