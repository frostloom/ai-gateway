package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRechargeRemainsClosedWithDemoFlag(t *testing.T) {
	for _, flag := range []string{"", "true"} {
		t.Run("demo="+flag, func(t *testing.T) {
			t.Setenv("ENABLE_DEMO_BILLING", flag)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
			defer server.Close()
			tb := NewToolbox(NewAdminClientWithToken(server.URL, ""))
			args := map[string]any{"amount_money": float64(100)}
			for _, operation := range []string{"preview", "run"} {
				var result string
				var err error
				if operation == "preview" {
					result, err = tb.Preview(context.Background(), "recharge", args, 42)
				} else {
					result, err = tb.Run(context.Background(), "recharge", args, 42, "test-only")
				}
				if err == nil || !strings.Contains(err.Error(), "关闭") || result != "" || calls != 0 {
					t.Fatalf("%s result=%q calls=%d err=%v", operation, result, calls, err)
				}
			}
		})
	}
}

func TestSubscriptionOperationsRemainClosedWithDemoFlag(t *testing.T) {
	for _, flag := range []string{"", "true"} {
		t.Run("demo="+flag, func(t *testing.T) {
			t.Setenv("ENABLE_DEMO_BILLING", flag)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
			defer server.Close()
			tb := NewToolbox(NewAdminClientWithToken(server.URL, ""))
			for _, tool := range []string{"subscribe_plan", "change_plan"} {
				args := map[string]any{"plan_id": float64(7), "plan_name": "金额月卡"}
				preview, err := tb.Preview(context.Background(), tool, args, 42)
				if err == nil || !strings.Contains(err.Error(), "尚未开放") || preview != "" {
					t.Errorf("%s preview=%q err=%v", tool, preview, err)
				}
				result, err := tb.Run(context.Background(), tool, args, 42, "prior-confirmation")
				if err == nil || !strings.Contains(err.Error(), "尚未开放") || result != "" {
					t.Errorf("%s run=%q err=%v", tool, result, err)
				}
			}
			if calls != 0 {
				t.Fatalf("closed operations contacted billing %d times", calls)
			}
		})
	}
}
