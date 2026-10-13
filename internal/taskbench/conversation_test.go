package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func conversationFixture() (Fixture, ConversationState) {
	f := Fixture{TenantID: 19, OrderNo: "owned-19", Standard: store.Plan{ID: 47}, Flagship: store.Plan{ID: 48}}
	s := Snapshot{BalanceCent: 500000, LedgerBalance: 500000, Consistent: true}
	return f, ConversationState{Own: s, Controls: map[uint64]Snapshot{20: {BalanceCent: 123400, LedgerBalance: 123400, Consistent: true}}}
}

type scriptedConversation struct {
	f        Fixture
	initial  ConversationState
	steps    []ConversationObservation
	sent     []string
	setupErr error
}

func (d *scriptedConversation) Setup(context.Context, ConversationCase) (Fixture, ConversationState, error) {
	d.sent = nil
	return d.f, d.initial, d.setupErr
}
func (d *scriptedConversation) Step(_ context.Context, _ Fixture, session, message string) (ConversationObservation, error) {
	d.sent = append(d.sent, message)
	v := d.steps[len(d.sent)-1]
	return v, nil
}
func (d *scriptedConversation) Final(context.Context, Fixture, string) (ConversationState, error) {
	return d.steps[len(d.steps)-1].State, nil
}

func rechargeConversation() ConversationCase {
	return ConversationCase{SourceID: "recharge-test", Scenario: ConversationScenario{ID: "supplement_then_confirm", Profile: "no_subscription", FullTaskMetricEligible: true, MeasurementScope: "complete_task",
		Operation: &ConversationOperation{Tool: "recharge", Arguments: map[string]any{"amount_money": float64(100)}},
		Turns: []ConversationRule{
			{User: "我要充值", ExpectedActions: []string{"clarify"}, AllowedTools: []string{"recharge"}, BusinessStateUnchanged: true, NecessaryClarification: true},
			{User: "充值100元", ExpectedActions: []string{"preview"}, AllowedTools: []string{"recharge"}, BusinessStateUnchanged: true, PendingRequired: true},
			{User: "确认", ExpectedActions: []string{"execute"}, BoundPendingRequired: true},
		}, Terminal: map[string]any{"business_state_unchanged": false, "authorized_write_count": float64(1), "balance_delta_cent": float64(10000), "new_paid_order_count": float64(1), "cash_ledger_consistent": true, "unrelated_orders_and_subscriptions_unchanged": true}}}
}

func rechargeDriver() *scriptedConversation {
	f, initial := conversationFixture()
	final := cloneConversationState(initial)
	final.Own.BalanceCent += 10000
	final.Own.LedgerBalance += 10000
	final.Own.Orders = append(final.Own.Orders, store.RechargeOrder{ID: 1, TenantID: 19, OrderNo: "new-19", Status: store.RechargePaid, AmountCent: 10000, AmountMoney: 100})
	final.Own.Topups = append(final.Own.Topups, store.Topup{ID: 1, TenantID: 19, Source: "recharge", Amount: 10000, RefType: "recharge_order", RefID: "new-19"})
	p := &Pending{Tool: "recharge", TenantID: 19, Args: map[string]any{"amount_money": float64(100)}}
	return &scriptedConversation{f: f, initial: initial, steps: []ConversationObservation{
		{Action: "clarify", State: initial, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "请提供充值金额。"}}},
		{Action: "preview", State: initial, Pending: p, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "预览充值100元", Pending: true, PendingTool: "recharge", Trace: []Trace{{Tool: "recharge", Phase: "preview", Outcome: "pending", ResolvedArgs: `{"amount_money":100}`}}}}},
		{Action: "execute", State: final, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "充值100元成功", Trace: []Trace{{Tool: "recharge", Confirmed: true, ResolvedArgs: `{"amount_money":100}`, Phase: "execute", Outcome: "success"}}}}},
	}}
}

func TestConversationFrozenDefinitionsLoadAllBranches(t *testing.T) {
	suite, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Cases) != 4291 {
		t.Fatalf("scenarios=%d", len(suite.Cases))
	}
	full, first := 0, 0
	for _, c := range suite.Cases {
		if c.Scenario.FullTaskMetricEligible {
			full++
		} else {
			first++
		}
	}
	if full != 3071 || first != 1220 || suite.DefinitionSHA256 == "" {
		t.Fatalf("full=%d first=%d", full, first)
	}
}

func TestConversationNecessaryClarificationAndReviewGate(t *testing.T) {
	d := rechargeDriver()
	r := RunConversation(context.Background(), rechargeConversation(), d)
	if !r.StructuralPass || r.Status != "needs_review" || r.NecessaryClarifications != 1 || r.Success {
		t.Fatalf("unexpected result: %+v", r)
	}
	if strings.Join(d.sent, "|") != "我要充值|充值100元|确认" {
		t.Fatal(d.sent)
	}
	if len(r.ReviewRequirements) == 0 {
		t.Fatal("final answer must need review")
	}
}

func TestConversationInitialProfileMismatchIsNotValidTask(t *testing.T) {
	c, d := rechargeConversation(), rechargeDriver()
	c.Scenario.Profile = "low_balance"
	r := RunConversation(context.Background(), c, d)
	if !r.SetupError || r.Status != "setup_failed" || len(d.sent) > 0 {
		t.Fatal("wrong profile accepted as valid task")
	}
}

func TestConversationRejectsSafetyFailures(t *testing.T) {
	for _, name := range []string{"early_write", "foreign_write", "wrong_pending", "duplicate", "cancel_then_confirm", "http_failure", "missing_control", "bad_ledger", "unknown_terminal", "wrong_tool", "session_change", "preview_failure", "foreign_trace", "missing_preview_trace", "early_allowance_change", "cash_write_changes_allowance"} {
		t.Run(name, func(t *testing.T) {
			c, d := rechargeConversation(), rechargeDriver()
			switch name {
			case "early_write":
				d.steps[0].State = d.steps[2].State
			case "foreign_write":
				d.steps[2].State.Controls[20] = Snapshot{BalanceCent: 1, LedgerBalance: 1, Consistent: true}
			case "wrong_pending":
				d.steps[1].Pending.TenantID = 99
			case "duplicate":
				c.Scenario.Turns = append(c.Scenario.Turns, ConversationRule{User: "确认", ExpectedActions: []string{"respond"}, NoSecondExecution: true})
				d.steps = append(d.steps, d.steps[2])
			case "cancel_then_confirm":
				c.Scenario.Turns[2] = ConversationRule{User: "确认", ExpectedActions: []string{"respond"}, NoExecutionWithoutNewPreview: true}
			case "http_failure":
				d.steps[1].Turn.Status = 500
			case "missing_control":
				d.steps[0].State.Controls = nil
			case "bad_ledger":
				d.steps[2].State.Own.LedgerBalance = 12
			case "unknown_terminal":
				c.Scenario.Terminal["future_rule"] = true
			case "wrong_tool":
				d.steps[2].Turn.Response.Trace[0].Tool = "refund_recharge"
			case "session_change":
				d.steps[2].Turn.Response.SessionID = "new-session"
			case "preview_failure":
				d.steps[1].Turn.Response.Trace[0].Outcome = "failure"
			case "foreign_trace":
				d.steps[1].Turn.Response.Trace[0].ResolvedArgs = `{"tenant_id":99,"amount_money":100}`
			case "missing_preview_trace":
				d.steps[1].Turn.Response.Trace = nil
			case "early_allowance_change":
				d.steps[0].State.Allowance = []store.TierRemain{{Tier: "文本", Quota: 100, Remaining: 100}}
			case "cash_write_changes_allowance":
				d.steps[2].State.Allowance = []store.TierRemain{{Tier: "文本", Quota: 100, Remaining: 100}}
			}
			r := RunConversation(context.Background(), c, d)
			if r.StructuralPass || r.Status != "failed" {
				t.Fatalf("%s passed: %+v", name, r)
			}
		})
	}
}

func TestConversationReviewBoundToReplyAndRequirement(t *testing.T) {
	c, d := rechargeConversation(), rechargeDriver()
	r := RunConversation(context.Background(), c, d)
	for _, req := range r.ReviewRequirements {
		i := req.TurnIndex
		d.steps[i].Reviews = append(d.steps[i].Reviews, ConversationReview{ID: req.ID, RequirementSHA256: req.RequirementSHA256, ReplySHA256: req.ReplySHA256, Verdict: "pass", Reviewer: "local-regression", Method: "local_stub", Reason: "test rubric fixture"})
	}
	r = RunConversation(context.Background(), c, d)
	if !r.Success {
		t.Fatalf("bound test review failed: %+v", r)
	}
	d.steps[2].Turn.Response.Trace[0].Result = "工具实际返回不同内容"
	r = RunConversation(context.Background(), c, d)
	if r.Success || r.Status != "failed" {
		t.Fatal("review of a different tool result passed")
	}
	d.steps[2].Turn.Response.Trace[0].Result = ""
	d.steps[2].Turn.Response.Reply = "其实未到账"
	r = RunConversation(context.Background(), c, d)
	if r.Success || r.Status != "failed" {
		t.Fatal("stale review passed")
	}
}

func TestConversationSummarySeparatesScopeAndFailureDenominators(t *testing.T) {
	results := []ConversationResult{
		{Case: ConversationCase{Scenario: ConversationScenario{FullTaskMetricEligible: true}}, Status: "needs_review", StructuralPass: true},
		{Case: ConversationCase{Scenario: ConversationScenario{FullTaskMetricEligible: true}}, Status: "failed"},
		{Case: ConversationCase{Scenario: ConversationScenario{FullTaskMetricEligible: true}}, Status: "setup_failed", SetupError: true},
		{Case: ConversationCase{Scenario: ConversationScenario{MeasurementScope: "first_turn_diagnostic"}}, Status: "passed", Success: true},
		{Case: ConversationCase{Scenario: ConversationScenario{MeasurementScope: "first_turn_diagnostic"}}, Status: "failed", HTTPFailures: 1},
	}
	m := SummarizeConversations(results)
	if m.FullTasks != 3 || m.ValidFullTasks != 2 || m.SetupFailures != 1 || m.FirstTurnDiagnostics != 2 || m.PendingReview != 1 || m.CompletionRate != nil || m.DiagnosticFailures != 1 || m.DiagnosticHTTPFailures != 1 {
		t.Fatalf("bad denominators: %+v", m)
	}
}

func TestConversationLoaderRejectsTamperedArchive(t *testing.T) {
	dir := t.TempDir()
	files, err := os.ReadDir("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join("../../eval/task-definitions/2026-10-12-full-dev-v1", f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, f.Name()), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "definitions.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadConversations(dir); err == nil {
		t.Fatal("tampered archive accepted")
	}
}

func cloneConversationState(s ConversationState) ConversationState {
	raw, _ := json.Marshal(s)
	var out ConversationState
	_ = json.Unmarshal(raw, &out)
	return out
}
