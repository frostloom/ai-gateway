package store

import (
	"context"
	"testing"
)

func TestConsolePlanAndPaymentOwnership(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	first := seedTenant(t, s, 0)
	second := seedTenant(t, s, 0)
	order, err := s.CreateRechargeOrder(ctx, first, 100, "console-test-request")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PayTenantRecharge(ctx, second, order.OrderNo); err == nil {
		t.Fatal("another tenant paid order")
	}
	untouched, err := s.rechargeByOrderNo(ctx, order.OrderNo)
	if err != nil || untouched.Status != RechargePending {
		t.Fatalf("unauthorized mutation: %+v %v", untouched, err)
	}
	if paid, err := s.PayTenantRecharge(ctx, first, order.OrderNo); err != nil || paid.Status != RechargePaid {
		t.Fatalf("owner payment failed: %+v %v", paid, err)
	}
	plan := &Plan{Name: "console-test-plan", PlanType: PlanTypeRecurring, PriceMoney: 29, ValidityDays: 30, RefreshHours: 5, TierQuota: `{}`, WindowQuotaCent: 1000, CycleQuotaCent: 10000}
	if err := s.SaveConsolePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	plan.PriceMoney = 30
	if err := s.SaveConsolePlan(ctx, plan); err == nil {
		t.Fatal("saved terms mutated")
	}
	plan.PriceMoney = 29
	plan.Status = 1
	if err := s.SaveConsolePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	saved, err := s.GetPlan(ctx, plan.ID)
	if err != nil || saved.Status != 1 || saved.PriceMoney != 29 {
		t.Fatalf("saved plan %+v %v", saved, err)
	}
}

func TestConfiguredCatalogExcludesUnconfiguredSeedAndWildcard(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	for _, m := range []*Model{
		{Vendor: "test", ModelID: "priced-configured", Name: "Configured", Tags: "[]", Status: 0},
		{Vendor: "test", ModelID: "seed-only", Name: "Seed", Tags: "[]", Status: 0},
		{Vendor: "test", ModelID: "priced-disabled", Name: "Disabled", Tags: "[]", Status: 1},
	} {
		if err := s.CreateModel(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	p := &Provider{Name: "catalog-source", BaseURL: "https://example.invalid", Models: `["priced-configured","priced-disabled","unpriced-configured","*"]`, Weight: 1, Status: 0}
	if err := s.UpsertProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer s.db.WithContext(ctx).Delete(p)
	managed, err := s.ListConfiguredModels(ctx, "", nil)
	if err != nil || len(managed) != 2 {
		t.Fatalf("managed=%+v err=%v", managed, err)
	}
	public, err := s.ListSupportedModels(ctx, "")
	if err != nil || len(public) != 1 || public[0].ModelID != "priced-configured" {
		t.Fatalf("public=%+v err=%v", public, err)
	}
	configured, err := s.ModelConfigured(ctx, "unpriced-configured")
	if err != nil || !configured {
		t.Fatalf("configured missing: %v %v", configured, err)
	}
	configured, err = s.ModelConfigured(ctx, "seed-only")
	if err != nil || configured {
		t.Fatalf("unconfigured model accepted: %v %v", configured, err)
	}
}
