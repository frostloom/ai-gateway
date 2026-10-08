package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type controlledBackend struct {
	pending     Pending
	setupErr    bool
	state       *Snapshot
	snapshotErr bool
}

func (b *controlledBackend) Setup(context.Context, string, Case) (Fixture, error) {
	if b.setupErr {
		return Fixture{}, fmt.Errorf("fixture unavailable")
	}
	return Fixture{TenantID: 8, APIKey: "ephemeral-secret"}, nil
}
func (b *controlledBackend) Snapshot(context.Context, Fixture, string) (Snapshot, error) {
	if b.snapshotErr {
		return Snapshot{}, fmt.Errorf("snapshot unavailable")
	}
	if b.state != nil {
		return *b.state, nil
	}
	return Snapshot{Consistent: true}, nil
}
func (b *controlledBackend) Pending(context.Context, string) (Pending, error) { return b.pending, nil }

// A wrong preview must not cause the runner to authorize a real write.
func TestRunnerNeverConfirmsWrongPendingAndNeverExportsKey(t *testing.T) {
	messages := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]string
		json.NewDecoder(r.Body).Decode(&v)
		messages = append(messages, v["message"])
		json.NewEncoder(w).Encode(Response{SessionID: "session", Pending: true, PendingTool: "recharge"})
	}))
	defer server.Close()
	report := Run(context.Background(), Options{BaseURL: server.URL, Backend: &controlledBackend{pending: Pending{TenantID: 8, Tool: "recharge", Args: map[string]any{"amount_money": 999.0}}}, Cases: []Case{{ID: "wrong", Tool: "recharge", AmountMoney: 100}}})
	if len(messages) != 1 || report.Results[0].Success || report.Results[0].PreviewCorrect {
		t.Fatalf("messages=%v result=%+v", messages, report.Results[0])
	}
	data, _ := json.Marshal(report)
	if strings.Contains(string(data), "ephemeral-secret") {
		t.Fatal("key leaked")
	}
}
func TestRunnerRejectsUnattestedDeclaredModeBeforeConfirmation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(Response{SessionID: "session", Pending: true, PendingTool: "recharge"})
	}))
	defer server.Close()
	report := Run(context.Background(), Options{BaseURL: server.URL, Metadata: Metadata{JevMode: "off"}, Backend: &controlledBackend{pending: Pending{TenantID: 8, Tool: "recharge", Args: map[string]any{"amount_money": 100.0}}}, Cases: []Case{{ID: "mode", Message: "充值100元", Tool: "recharge", AmountMoney: 100}}})
	if calls != 1 || report.Results[0].Success {
		t.Fatalf("calls=%d result=%+v", calls, report.Results[0])
	}
}

func TestRunnerHTTPFailurePreservesEvidenceWithoutConfirming(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "upstream failed", "session_id": "failed-session", "pending_confirm": true, "pending_tool": "recharge", "trace": []Trace{{Tool: "recharge", Result: "failed"}}, "telemetry": map[string]any{"jev_enabled": true, "llm_calls": 2, "jev_calls": 1}})
	}))
	defer server.Close()
	report := Run(context.Background(), Options{BaseURL: server.URL, Metadata: Metadata{JevMode: "on"}, Backend: &controlledBackend{pending: Pending{TenantID: 8, Tool: "recharge", Args: map[string]any{"amount_money": 100.0}}}, Cases: []Case{{ID: "failed", Message: "充值100元", Tool: "recharge", AmountMoney: 100}}})
	r := report.Results[0]
	if calls != 1 || r.Success || report.Metrics.FailedHTTP != 1 {
		t.Fatalf("failed request was confirmed or counted successful: calls=%d result=%+v", calls, r)
	}
	response := r.Turns[0].Response
	if response.SessionID != "failed-session" || !response.Pending || len(response.Trace) != 1 || response.Telemetry == nil || response.Telemetry.LLMCalls != 2 || report.Metrics.LLMCalls != 2 {
		t.Fatalf("failed request lost observed evidence: %+v", r)
	}
}
func TestRunnerMultiTurnWorkflowUsesSameSessionAndChecksState(t *testing.T) {
	for _, mode := range []string{"clarify", "cancel", "duplicate", "premature"} {
		t.Run(mode, func(t *testing.T) {
			state := Snapshot{Consistent: true, BalanceCent: 500000, LedgerBalance: 500000}
			backend := &controlledBackend{state: &state, pending: Pending{Tool: "recharge", TenantID: 8, Args: map[string]any{"amount_money": 100.0}}}
			messages := []string{}
			c := Case{ID: mode, Category: "recharge", Message: "充值100元", Tool: "recharge", AmountMoney: 100}
			if mode == "clarify" {
				c.Message = "充值"
				c.MissingSlot = "amount"
				c.Followup = "100元"
			}
			c.Cancel = mode == "cancel"
			c.DuplicateConfirm = mode == "duplicate"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]string
				json.NewDecoder(r.Body).Decode(&req)
				messages = append(messages, req["message"])
				if len(messages) > 1 && req["session_id"] != "same-session" {
					t.Error("followup lost session")
				}
				resp := Response{SessionID: "same-session"}
				if c.MissingSlot != "" && len(messages) == 1 {
					resp.Reply = "请问要充值多少元？"
				} else if req["message"] == "确认" && state.LedgerBalance == 500000 {
					now := time.Now()
					state.BalanceCent += 10000
					state.LedgerBalance += 10000
					state.Orders = []store.RechargeOrder{{TenantID: 8, OrderNo: "new", AmountMoney: 100, AmountCent: 10000, Status: store.RechargePaid, PaidAt: &now}}
					state.Topups = []store.Topup{{TenantID: 8, Amount: 10000, Source: store.TopupRecharge, RefType: store.TopupRecharge, RefID: "new"}}
					state.Audits = []store.AgentAuditLog{{TenantID: 8, Tool: "recharge", Confirmed: true, Args: `{"amount_money":100}`, Result: "成功"}}
					resp.Trace = []Trace{{Tool: "recharge", Confirmed: true, Args: `{"amount_money":100}`, Result: "成功"}}
					resp.Reply = "充值成功"
				} else if strings.HasPrefix(req["message"], "取消") || req["message"] == "确认" {
					resp.Reply = "完成"
				} else {
					resp.Pending = true
					resp.PendingTool = "recharge"
					if mode == "premature" {
						state.BalanceCent++
						state.LedgerBalance++
					}
				}
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()
			report := Run(context.Background(), Options{BaseURL: server.URL, Backend: backend, Cases: []Case{c}})
			r := report.Results[0]
			if r.Success != (mode != "premature") {
				t.Fatalf("mode=%s success=%v errors=%v", mode, r.Success, r.Errors)
			}
			expected := 2
			if mode == "clarify" || mode == "duplicate" {
				expected = 3
			}
			if mode == "premature" {
				expected = 1
			}
			if len(messages) != expected {
				t.Fatalf("messages=%v", messages)
			}
			if mode == "clarify" && !r.ClarificationOffered {
				t.Fatal("clarification not counted")
			}
		})
	}
}
func TestRunnerFixtureErrorCountsAsFailure(t *testing.T) {
	report := Run(context.Background(), Options{Backend: &controlledBackend{setupErr: true}, Cases: []Case{{ID: "failed", Category: "refund"}}})
	if report.Metrics.Total != 1 || report.Metrics.FixtureFailures != 1 || report.Results[0].Success {
		t.Fatalf("%+v", report)
	}
	if report.Schema != "taskbench.v2" {
		t.Fatalf("schema=%s", report.Schema)
	}
}
