package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestMonetaryPlanQuotaLine(t *testing.T) {
	plan := &store.Plan{RefreshHours: 5, ValidityDays: 30, WindowQuotaCent: 1234, WeekQuotaCent: 5678, CycleQuotaCent: 9000, TierQuota: `{}`}
	got := planQuotaLine(plan)
	for _, want := range []string{"5 小时", "12.34", "7 天", "56.78", "30 天", "90.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("quota line %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "token") {
		t.Errorf("money entitlement described as tokens: %q", got)
	}
}

func TestSubscriptionToolShowsMoneyAllowance(t *testing.T) {
	reset := time.Date(2026, 11, 11, 12, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	profile := store.MeView{Subscription: &store.Subscription{PlanID: 7, PlanType: store.PlanTypeRecurring}, RefreshHours: 5,
		Plans: []store.Plan{{ID: 7, RefreshHours: 5, ValidityDays: 30}},
		MoneyAllowance: []store.MoneyAllowance{
			{Period: "window", QuotaCent: 1000, ConsumedCent: 250, RemainingCent: 750, ResetsAt: &reset},
			{Period: "week", QuotaCent: 5000, ConsumedCent: 500, RemainingCent: 4500, ResetsAt: &reset},
			{Period: "cycle", QuotaCent: 10000, ConsumedCent: 1000, RemainingCent: 9000, ResetsAt: &reset},
		}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/me" || r.URL.Query().Get("tenant_id") != "42" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(profile)
	}))
	defer server.Close()
	tb := NewToolbox(NewAdminClientWithToken(server.URL, ""))
	got, err := tb.Run(context.Background(), "get_my_subscription", nil, 42, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"5 小时", "7 天", "30 天", "7.50", "10.00", "45.00", "50.00", "90.00", "100.00", "25.0%", "2026-11-11 12:30:00"} {
		if !strings.Contains(got, want) {
			t.Errorf("subscription %q missing %q", got, want)
		}
	}
}

func TestPlanCatalogToolHidesLegacyAndDisabledPlans(t *testing.T) {
	profile := store.MeView{Plans: []store.Plan{
		{ID: 1, Name: "金额月卡", PlanType: store.PlanTypeRecurring, ValidityDays: 30, RefreshHours: 5, WindowQuotaCent: 1000},
		{ID: 2, Name: "历史月卡", PlanType: store.PlanTypeRecurring, TierQuota: `{"文本":100000}`},
		{ID: 3, Name: "停用月卡", PlanType: store.PlanTypeRecurring, WindowQuotaCent: 1000, Status: 1},
		{ID: 4, Name: "一次性计划", PlanType: store.PlanTypeOneTime, WindowQuotaCent: 1000},
	}}
	for _, legacyOnly := range []bool{false, true} {
		catalog := profile
		if legacyOnly {
			catalog.Plans = profile.Plans[1:]
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(catalog)
		}))
		defer server.Close()
		tb := NewToolbox(NewAdminClientWithToken(server.URL, ""))
		got, err := tb.Run(context.Background(), "list_plans", nil, 42, "")
		if err != nil {
			t.Fatal(err)
		}
		if legacyOnly {
			if got != "暂无上架套餐。" {
				t.Fatalf("legacy-only catalog = %q", got)
			}
			continue
		}
		if !strings.Contains(got, "金额月卡") || !strings.Contains(got, "10.00") {
			t.Errorf("money plan missing: %q", got)
		}
		for _, hidden := range []string{"历史月卡", "停用月卡", "一次性计划", "10万", "token"} {
			if strings.Contains(got, hidden) {
				t.Errorf("catalog %q contains %q", got, hidden)
			}
		}
	}
}

func TestLegacySubscriptionToolShowsPercentWithoutTokenAmounts(t *testing.T) {
	profile := store.MeView{Subscription: &store.Subscription{PlanID: 7, PlanType: store.PlanTypeRecurring}, RefreshHours: 5,
		Allowance: []store.TierRemain{{Tier: "文本", Quota: 123456, Remaining: 30864}, {Tier: "推理", Quota: 0, Remaining: 0}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(profile) }))
	defer server.Close()
	tb := NewToolbox(NewAdminClientWithToken(server.URL, ""))
	got, err := tb.Run(context.Background(), "get_my_subscription", nil, 42, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"文本", "剩余 25.0%", "5 小时"} {
		if !strings.Contains(got, want) {
			t.Errorf("legacy subscription %q missing %q", got, want)
		}
	}
	for _, hidden := range []string{"123456", "30864", "12.3万", "3.1万", "NaN", "Inf", "token", "¥"} {
		if strings.Contains(got, hidden) {
			t.Errorf("legacy subscription %q contains %q", got, hidden)
		}
	}
}
