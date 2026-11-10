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

// Keep the legacy selection contract covered independently of paid operations,
// whose production preview and execution entry points remain closed.
func TestPlanToolsResolveAndFreeze(t *testing.T) {
	for _, tool := range []string{"subscribe_plan", "change_plan"} {
		for _, name := range []string{"月度·旗舰", " 月度 旗舰 ", "旗舰"} {
			t.Run(tool+"/"+name, func(t *testing.T) {
				plans := []store.Plan{{ID: 47, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring, PriceMoney: 320, RefreshHours: 6}}
				reads, writes := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						reads++
						if r.URL.Path != "/admin/me" || r.URL.Query().Get("tenant_id") != "19" {
							t.Errorf("wrong read scope: %s", r.URL)
						}
						_ = json.NewEncoder(w).Encode(store.MeView{Plans: plans})
						return
					}
					writes++
					t.Errorf("selection or closed operation unexpectedly wrote %s", r.URL.Path)
					w.WriteHeader(http.StatusForbidden)
				}))
				defer server.Close()
				tb := NewToolbox(NewAdminClient(server.URL))
				args := map[string]any{"plan_name": name, "tenant_id": float64(999)}
				plan, err := tb.findPlan(context.Background(), 19, args)
				if err != nil {
					t.Fatalf("selection by user name: %v", err)
				}
				if !strings.Contains(plan.Name, "月度·旗舰") || args["plan_id"] != int64(47) || args["plan_name"] != "月度·旗舰" {
					t.Fatalf("selection must freeze catalog identity: %q %#v", plan.Name, args)
				}
				if writes != 0 {
					t.Fatalf("selection wrote %d times", writes)
				}
				// A previously frozen confirmation cannot open the closed operation.
				if _, err := tb.Run(context.Background(), tool, args, 19, "confirmed"); err == nil || !strings.Contains(err.Error(), "尚未开放") {
					t.Fatalf("closed operation run: %v", err)
				}
				if writes != 0 || reads != 1 {
					t.Fatalf("closed operation contacted billing: reads=%d writes=%d", reads, writes)
				}
			})
		}
	}
}

func TestPlanToolsRejectUnsafeSelection(t *testing.T) {
	active := store.Plan{ID: 47, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring}
	disabled := store.Plan{ID: 61, Name: "月度·标准", PlanType: store.PlanTypeRecurring, Status: 1}
	oneTime := store.Plan{ID: 73, Name: "一次性·标准", PlanType: store.PlanTypeOneTime}
	for _, tool := range []string{"subscribe_plan", "change_plan"} {
		for _, tc := range []struct {
			name  string
			args  map[string]any
			plans []store.Plan
		}{
			{"missing", map[string]any{}, []store.Plan{active}},
			{"unknown", map[string]any{"plan_name": "不存在"}, []store.Plan{active}},
			{"empty", map[string]any{"plan_name": " · "}, []store.Plan{active}},
			{"ambiguous", map[string]any{"plan_name": "旗舰"}, []store.Plan{active, {ID: 89, Name: "年度·旗舰", PlanType: store.PlanTypeRecurring}}},
			{"duplicate normalized name", map[string]any{"plan_name": "月度旗舰"}, []store.Plan{active, {ID: 89, Name: "月度 旗舰", PlanType: store.PlanTypeRecurring}}},
			{"conflicting ID", map[string]any{"plan_name": "旗舰", "plan_id": float64(61)}, []store.Plan{active, disabled}},
			{"ambiguous with ID", map[string]any{"plan_name": "旗舰", "plan_id": float64(47)}, []store.Plan{active, {ID: 89, Name: "年度·旗舰", PlanType: store.PlanTypeRecurring}}},
			{"unknown name with ID", map[string]any{"plan_name": "不存在", "plan_id": float64(47)}, []store.Plan{active}},
			{"disabled name", map[string]any{"plan_name": "标准"}, []store.Plan{disabled}},
			{"disabled ID", map[string]any{"plan_id": float64(61)}, []store.Plan{disabled}},
			{"one time", map[string]any{"plan_name": "标准"}, []store.Plan{oneTime}},
			{"one time ID", map[string]any{"plan_id": float64(73)}, []store.Plan{oneTime}},
			{"fractional ID", map[string]any{"plan_id": float64(47.9)}, []store.Plan{active}},
			{"invalid ID with name", map[string]any{"plan_id": "47", "plan_name": "旗舰"}, []store.Plan{active}},
			{"negative ID", map[string]any{"plan_id": float64(-1)}, []store.Plan{active}},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				writes := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						writes++
					}
					_ = json.NewEncoder(w).Encode(store.MeView{Plans: tc.plans})
				}))
				defer server.Close()
				if _, err := NewToolbox(NewAdminClient(server.URL)).findPlan(context.Background(), 19, tc.args); err == nil {
					t.Fatalf("unsafe selection accepted: %#v", tc.args)
				}
				if writes != 0 {
					t.Fatalf("rejected selection wrote %d times", writes)
				}
			})
		}
	}
}

func TestPlanToolsKeepLegacyIDAndIgnoreUnavailablePlans(t *testing.T) {
	for _, tool := range []string{"subscribe_plan", "change_plan"} {
		for _, args := range []map[string]any{
			{"plan_id": float64(47)},
			{"plan_id": int64(47), "plan_name": "月度旗舰"},
			{"plan_name": "月度旗舰"},
			{"plan_name": "旗舰"},
		} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(store.MeView{Plans: []store.Plan{
					{ID: 47, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring},
					{ID: 61, Name: "升级月度·旗舰", PlanType: store.PlanTypeRecurring, Status: 1},
					{ID: 73, Name: "一次性·旗舰", PlanType: store.PlanTypeOneTime},
				}})
			}))
			if _, err := NewToolbox(NewAdminClient(server.URL)).findPlan(context.Background(), 19, args); err != nil {
				t.Errorf("%s valid selection %#v: %v", tool, args, err)
			}
			server.Close()
		}
	}
}

func TestPlanToolsPreferExactName(t *testing.T) {
	for _, tool := range []string{"subscribe_plan", "change_plan"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(store.MeView{Plans: []store.Plan{
				{ID: 47, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring},
				{ID: 89, Name: "升级月度·旗舰", PlanType: store.PlanTypeRecurring},
			}})
		}))
		args := map[string]any{"plan_name": "月度旗舰"}
		if _, err := NewToolbox(NewAdminClient(server.URL)).findPlan(context.Background(), 19, args); err != nil {
			t.Errorf("%s exact name rejected due to partial match: %v", tool, err)
		} else if args["plan_id"] != int64(47) {
			t.Errorf("%s selected partial match: %#v", tool, args)
		}
		server.Close()
	}
}

func TestPlanToolsRunRejectsUnresolvedName(t *testing.T) {
	for _, tool := range []string{"subscribe_plan", "change_plan"} {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writes++
			_, _ = w.Write([]byte(`{}`))
		}))
		if _, err := NewToolbox(NewAdminClient(server.URL)).Run(context.Background(), tool, map[string]any{"plan_name": "旗舰"}, 19, ""); err == nil {
			t.Errorf("%s ran without a resolved preview", tool)
		}
		if writes != 0 {
			t.Errorf("%s unresolved run contacted billing", tool)
		}
		server.Close()
	}
}

func TestCancelledSubscriptionReadPreservesCurrentCycle(t *testing.T) {
	end := time.Now().Add(24 * time.Hour)
	pending := uint64(99)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.MeView{Subscription: &store.Subscription{PlanID: 47, Status: store.SubCancelled, CycleEnd: &end, AutoRenew: false, PendingPlanID: &pending}})
	}))
	defer server.Close()
	reply, err := NewToolbox(NewAdminClient(server.URL)).Run(context.Background(), "get_my_subscription", map[string]any{}, 19, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "已停止续费") || !strings.Contains(reply, "到期前可继续使用") || strings.Contains(reply, "下期将切换") {
		t.Fatalf("misleading cancelled subscription reply: %s", reply)
	}
}

func TestCancelRenewalRemainsAvailableWhileActivationClosed(t *testing.T) {
	for _, flag := range []string{"", "true"} {
		t.Run("demo="+flag, func(t *testing.T) {
			t.Setenv("ENABLE_DEMO_BILLING", flag)
			end := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
			reads, writes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/me":
					reads++
					if r.Method != http.MethodGet || r.URL.Query().Get("tenant_id") != "42" {
						t.Errorf("invalid cancellation preview request %s %s", r.Method, r.URL)
					}
					_ = json.NewEncoder(w).Encode(store.MeView{Subscription: &store.Subscription{PlanID: 7, AutoRenew: true, CycleEnd: &end}})
				case "/admin/cancel-subscription":
					writes++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if r.Method != http.MethodPost || body["tenant_id"] != float64(42) {
						t.Errorf("invalid cancellation write %s %#v", r.Method, body)
					}
					_ = json.NewEncoder(w).Encode(store.CancelResult{PlanName: "已有月卡", CurrentCycleEnd: &end})
				default:
					t.Errorf("unexpected cancellation request %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			tb := NewToolbox(NewAdminClientWithToken(server.URL, ""))
			preview, err := tb.Preview(context.Background(), "cancel_subscription", nil, 42)
			if err != nil || !strings.Contains(preview, "只取消下期续费") || writes != 0 {
				t.Fatalf("preview=%q writes=%d err=%v", preview, writes, err)
			}
			result, err := tb.Run(context.Background(), "cancel_subscription", map[string]any{"tenant_id": float64(999)}, 42, "confirm")
			if err != nil || !strings.Contains(result, "下期不再续费") || reads != 1 || writes != 1 {
				t.Fatalf("result=%q reads=%d writes=%d err=%v", result, reads, writes, err)
			}
		})
	}
}
