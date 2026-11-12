package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm"
)

func scopedTestPlan(scope string) *Plan {
	return &Plan{Name: "scope-plan", PlanType: PlanTypeRecurring, PriceMoney: 100, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`, WindowQuotaCent: 200, CycleQuotaCent: 20000, AllowedModels: scope}
}

func TestPlanModelScopeValidation(t *testing.T) {
	for _, scope := range []string{`null`, `{}`, `[1]`, `[""]`, `["*"]`, `[" a "]`, `["a","a"]`, `broken`} {
		p := scopedTestPlan(scope)
		if ValidateConsolePlan(p) == nil {
			t.Errorf("accepted scope %q", scope)
		}
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("model-%d", i)
	}
	raw, _ := json.Marshal(ids)
	if ValidateConsolePlan(scopedTestPlan(string(raw))) == nil {
		t.Fatal("accepted more than 100 model IDs")
	}
	for _, scope := range []string{"", `[]`, `["b","a"]`} {
		p := scopedTestPlan(scope)
		if err := ValidateConsolePlan(p); err != nil {
			t.Fatal(err)
		}
		if scope == `["b","a"]` && p.AllowedModels != `["a","b"]` {
			t.Fatalf("noncanonical scope %q", p.AllowedModels)
		}
	}
}

func TestConsolePlanModelScopeCatalogAndImmutable(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedTierModel(t, s, "scope-in", `[]`)
	seedTierModel(t, s, "scope-out", `[]`)
	seedTierModel(t, s, "scope-disabled", `[]`)
	if err := s.db.Model(&Model{}).Where("model_id = ?", "scope-disabled").Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	provider := &Provider{Name: "scope-provider", BaseURL: "https://example.invalid", Models: `["scope-in","scope-disabled"]`, Weight: 1}
	if err := s.UpsertProvider(ctx, provider); err != nil {
		t.Fatal(err)
	}
	defer s.db.Delete(provider)
	for _, scope := range []string{`["missing"]`, `["scope-out"]`, `["scope-disabled"]`} {
		if err := s.SaveConsolePlan(ctx, scopedTestPlan(scope)); err == nil {
			t.Fatalf("accepted unconfigured scope %s", scope)
		}
	}
	p := scopedTestPlan(`["scope-in"]`)
	if err := s.SaveConsolePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.AllowedModels = `[]`
	if err := s.SaveConsolePlan(ctx, p); err == nil {
		t.Fatal("scope mutated")
	}
	p.AllowedModels = `["scope-in"]`
	p.Status = 1
	// Delisting an upstream must not prevent disabling an existing plan.
	if err := s.db.Delete(provider).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConsolePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	saved, err := s.GetPlan(ctx, p.ID)
	if err != nil || saved.Status != 1 || saved.AllowedModels != p.AllowedModels {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestSubscriptionModelScopeFundingAndMalformed(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	seedTierModel(t, s, "scope-in", `[]`)
	seedTierModel(t, s, "scope-out", `[]`)
	p := scopedTestPlan(`["scope-in"]`)
	if err := s.db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant, p.ID); err != nil {
		t.Fatal(err)
	}
	// Disabling catalog sales preserves stored subscribers' scoped current rights.
	if err := s.db.Model(&Plan{}).Where("id = ?", p.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id, model string
		code      billingv1.ReserveCode
	}{
		{"scope-1", "scope-in", billingv1.ReserveCode_RESERVE_OK},
		{"scope-2", "scope-in", billingv1.ReserveCode_RESERVE_SUB_QUOTA},
		{"scope-3", "scope-out", billingv1.ReserveCode_RESERVE_OK},
	} {
		got, err := s.Reserve(ctx, reserveTierReq(v.id, tenant, v.model, 100))
		if err != nil || got.Code != v.code {
			t.Fatalf("%s got=%+v err=%v", v.id, got, err)
		}
	}
	for id, funding := range map[string]string{"scope-1": FundingSubscription, "scope-3": FundingBalance} {
		bill, err := s.getReserve(ctx, id)
		if err != nil || bill == nil || bill.Funding != funding {
			t.Fatalf("%s bill=%+v err=%v", id, bill, err)
		}
	}
	if balance, err := s.getBalance(ctx, tenant); err != nil || balance != 880 {
		t.Fatalf("balance=%d err=%v", balance, err)
	}
	// The SQL text storage permits diagnosing malformed legacy data; never charge cash for it.
	if err := s.db.Model(&Plan{}).Where("id = ?", p.ID).Update("allowed_models", "malformed").Error; err != nil {
		t.Fatal(err)
	}
	if got, err := s.Reserve(ctx, reserveTierReq("scope-bad", tenant, "scope-out", 1)); err == nil {
		t.Fatalf("malformed scope accepted %+v", got)
	}
	if balance, _ := s.getBalance(ctx, tenant); balance != 880 {
		t.Fatalf("malformed scope charged wallet %d", balance)
	}
}

func TestSubscriptionModelScopeTransactionRecheck(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	tenant := seedTenant(t, s, 1000)
	seedTierModel(t, s, "scope-in", `[]`)
	p := scopedTestPlan(`["scope-in"]`)
	if err := s.db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubscribePlan(ctx, tenant, p.ID); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var injectedErr error
	const callback = "test:scope_change"
	if err := s.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "subscriptions" {
			return
		}
		if _, ok := tx.Statement.ConnPool.(*sql.Tx); !ok {
			return
		}
		once.Do(func() {
			injectedErr = s.db.Model(&Plan{}).Where("id = ?", p.ID).Update("allowed_models", `["other"]`).Error
		})
	}); err != nil {
		t.Fatal(err)
	}
	defer s.db.Callback().Query().Remove(callback)
	got, err := s.Reserve(ctx, reserveTierReq("scope-race", tenant, "scope-in", 1))
	if injectedErr != nil {
		t.Fatal(injectedErr)
	}
	if err == nil {
		t.Fatalf("changed scope accepted %+v", got)
	}
	if bill, _ := s.getReserve(ctx, "scope-race"); bill != nil {
		t.Fatalf("unexpected bill %+v", bill)
	}
	if balance, _ := s.getBalance(ctx, tenant); balance != 1000 {
		t.Fatalf("wallet charged %d", balance)
	}
}

func TestConsolePlanRequiresNewScopePreservesLegacy(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	for i, scope := range []string{"", "[]"} {
		p := scopedTestPlan(scope)
		p.Name = fmt.Sprintf("new-empty-%d", i)
		if err := s.SaveConsolePlan(ctx, p); err == nil {
			t.Errorf("new scope %q accepted", scope)
		}
		if p.ID != 0 {
			t.Errorf("rejected plan persisted ID=%d", p.ID)
		}
	}
	// Old unrestricted terms remain readable and may be disabled/re-enabled.
	legacy := scopedTestPlan("")
	legacy.Name = "legacy-unrestricted"
	if err := s.db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPlan(ctx, legacy.ID)
	if err != nil || got == nil || got.AllowedModels != "" {
		t.Fatalf("legacy=%+v err=%v", got, err)
	}
	for _, status := range []int8{1, 0} {
		got.Status = status
		if err := s.SaveConsolePlan(ctx, got); err != nil {
			t.Fatal(err)
		}
		saved, err := s.GetPlan(ctx, legacy.ID)
		if err != nil || saved == nil || saved.Status != status || saved.AllowedModels != "" {
			t.Fatalf("legacy status=%+v err=%v", saved, err)
		}
	}
}
