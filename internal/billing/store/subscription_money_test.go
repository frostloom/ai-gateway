package store

import (
	"context"
	"database/sql"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func TestSubscriptionMoneyCapsUseModelPrices(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	seedTierModel(t, s, "money-one", `[]`)
	seedTierModel(t, s, "money-two", `[]`)
	s.db.Model(&Model{}).Where("model_id = ?", "money-two").Update("input_price_cent", 2000000)
	plan := &Plan{Name: "money-plan", PlanType: PlanTypeRecurring, PriceMoney: 29, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`, WindowQuotaCent: 300, CycleQuotaCent: 350}
	if err := s.db.Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant, plan.ID); err != nil {
		t.Fatal(err)
	}
	s.db.Model(&Subscription{}).Where("tenant_id = ?", tenant).Update("cycle_start", time.Now().Add(-24*time.Hour))
	first, err := s.Reserve(ctx, reserveTierReq("money-a", tenant, "money-one", 100))
	if err != nil || first.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("first %+v %v", first, err)
	}
	// 100 cheap tokens reserve 120 cents; 100 expensive tokens need 240 cents.
	denied, err := s.Reserve(ctx, reserveTierReq("money-b", tenant, "money-two", 100))
	if err != nil || denied.Code != billingv1.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatalf("different model price ignored %+v %v", denied, err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: "money-a", PromptTokens: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: "money-a"}); err != nil {
		t.Fatal(err)
	}
	s.db.Model(&Bill{}).Where("request_id = ? AND phase = ?", "money-a", PhaseReserve).Update("created_at", time.Now().Add(-6*time.Hour))
	second, err := s.Reserve(ctx, reserveTierReq("money-c", tenant, "money-two", 100))
	if err != nil || second.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("window not restored %+v %v", second, err)
	}
	denied, err = s.Reserve(ctx, reserveTierReq("money-d", tenant, "money-one", 50))
	if err != nil || denied.Code != billingv1.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatalf("cycle cap ignored %+v %v", denied, err)
	}
	me, err := s.MeOverview(ctx, tenant)
	if err != nil || len(me.MoneyAllowance) != 2 || me.MoneyAllowance[1].ConsumedCent != 340 {
		t.Fatalf("money allowance %+v %v", me, err)
	}
	if balance, _ := s.getBalance(ctx, tenant); balance != 1000 {
		t.Fatalf("subscription spent wallet %d", balance)
	}
}

func TestSubscriptionWeeklyBudgetAndCurrentRights(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	seedTierModel(t, s, "money-week", `[]`)
	plan := &Plan{Name: "weekly-budget", PlanType: PlanTypeRecurring, PriceMoney: 29, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`, WindowQuotaCent: 200, WeekQuotaCent: 250, CycleQuotaCent: 1000}
	if err := s.db.Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant, plan.ID); err != nil {
		t.Fatal(err)
	}
	s.db.Model(&Subscription{}).Where("tenant_id = ?", tenant).Update("cycle_start", time.Now().Add(-10*24*time.Hour))
	first, err := s.Reserve(ctx, reserveTierReq("week-1", tenant, "money-week", 100))
	if err != nil || first.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("first %+v %v", first, err)
	}
	s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: "week-1", PromptTokens: 100})
	s.Settle(ctx, &billingv1.SettleRequest{RequestId: "week-1"})
	s.db.Model(&Bill{}).Where("request_id = ? AND phase = ?", "week-1", PhaseReserve).Update("created_at", time.Now().Add(-6*time.Hour))
	s.db.Model(&Plan{}).Where("id = ?", plan.ID).Update("status", 1)
	second, err := s.Reserve(ctx, reserveTierReq("week-2", tenant, "money-week", 100))
	if err != nil || second.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("current rights lost after disable %+v %v", second, err)
	}
	denied, err := s.Reserve(ctx, reserveTierReq("week-3", tenant, "money-week", 30))
	if err != nil || denied.Code != billingv1.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatalf("weekly cap ignored %+v %v", denied, err)
	}
}

// A committed reservation between the initial subscription read and allowance
// read must remain visible under MySQL's default REPEATABLE READ isolation.
func TestSubscriptionMoneyInitialReadDoesNotFreezeAllowanceSnapshot(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	s.EnableLedgerBatch(time.Hour)
	tenant := seedTenant(t, s, 1000)
	seedTierModel(t, s, "money-snapshot", `[]`)
	plan := &Plan{Name: "snapshot-plan", PlanType: PlanTypeRecurring, PriceMoney: 29, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`, WindowQuotaCent: 100}
	if err := s.db.Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant, plan.ID); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var injectedErr error
	const callback = "test:money_snapshot"
	if err := s.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "subscriptions" {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); !inTransaction {
			return
		}
		once.Do(func() {
			amount := int64(100)
			prior := &Bill{RequestID: "snapshot-prior", Phase: PhaseReserve, Status: StatusPending, TenantID: tenant, Model: "money-snapshot", Funding: FundingSubscription, ReservedCostCent: &amount, CreatedAt: time.Now()}
			injectedErr = s.assignBillID(s.db, prior)
			if injectedErr == nil {
				injectedErr = s.db.Create(prior).Error
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	defer s.db.Callback().Query().Remove(callback)
	result, err := s.Reserve(ctx, reserveTierReq("snapshot-next", tenant, "money-snapshot", 1))
	if injectedErr != nil {
		t.Fatal(injectedErr)
	}
	if err != nil || result.Code != billingv1.ReserveCode_RESERVE_SUB_QUOTA {
		t.Fatalf("stale allowance accepted reservation: %+v %v", result, err)
	}
}
