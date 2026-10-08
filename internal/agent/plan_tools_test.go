package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// Removing name resolution, uniqueness checks, or frozen IDs must break these
// tests. HTTP is the external boundary; previews and tool execution are real.
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
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					wantPath := "/admin/subscribe"
					if tool == "change_plan" {
						wantPath = "/admin/change-plan"
					}
					if r.URL.Path != wantPath || body["plan_id"] != float64(47) || body["tenant_id"] != float64(19) {
						t.Errorf("wrong confirmed write: %s %#v", r.URL.Path, body)
					}
					_, _ = w.Write([]byte(`{}`))
				}))
				defer server.Close()
				tb := NewToolbox(NewAdminClient(server.URL))
				args := map[string]any{"plan_name": name, "tenant_id": float64(999)}
				preview, err := tb.Preview(context.Background(), tool, args, 19)
				if err != nil {
					t.Fatalf("preview by user name: %v", err)
				}
				if !strings.Contains(preview, "月度·旗舰") || args["plan_id"] != int64(47) || args["plan_name"] != "月度·旗舰" {
					t.Fatalf("preview must freeze catalog identity: %q %#v", preview, args)
				}
				if writes != 0 {
					t.Fatalf("preview wrote %d times", writes)
				}
				// The same name now identifies another ID; confirmation stays on 47.
				plans = []store.Plan{{ID: 83, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring}}
				if _, err := tb.Run(context.Background(), tool, args, 19, "confirmed"); err != nil {
					t.Fatalf("confirmed run: %v", err)
				}
				if writes != 1 || reads != 1 {
					t.Fatalf("confirmation must use frozen identity: reads=%d writes=%d", reads, writes)
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
				if _, err := NewToolbox(NewAdminClient(server.URL)).Preview(context.Background(), tool, tc.args, 19); err == nil {
					t.Fatalf("unsafe selection accepted: %#v", tc.args)
				}
				if writes != 0 {
					t.Fatalf("rejected preview wrote %d times", writes)
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
			if _, err := NewToolbox(NewAdminClient(server.URL)).Preview(context.Background(), tool, args, 19); err != nil {
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
		if _, err := NewToolbox(NewAdminClient(server.URL)).Preview(context.Background(), tool, args, 19); err != nil {
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
