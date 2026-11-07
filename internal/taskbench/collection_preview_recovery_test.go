package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// This synthetic fixture mirrors the stopped shape, not its private originals.
// Its successful pending call is deliberately absent from session history:
// agent/chat.go stores only the rendered assistant reply for a pending action.
func collectionPreviewRecoveryRefundExample(t *testing.T) (ConversationCase, Fixture, ConversationObservation, collectionPreviewRecoveryEvidence) {
	t.Helper()
	base := qualityPreviewExample(t)
	f := base.Record.Result.Fixture
	c := ConversationCase{SourceID: "refund-recovery", Scenario: ConversationScenario{
		ID: "confirm", Profile: "no_subscription", FullTaskMetricEligible: true, MeasurementScope: "complete_task",
		Operation: &ConversationOperation{Tool: "refund_recharge", Arguments: map[string]any{"amount_money": float64(100)}},
		Turns: []ConversationRule{
			{User: "退100元", ExpectedActions: []string{"preview"}, AllowedTools: []string{"refund_recharge"}, BusinessStateUnchanged: true, PendingRequired: true},
			{User: "确认", ExpectedActions: []string{"execute"}, BoundPendingRequired: true},
		},
		Terminal: map[string]any{"authorized_write_count": float64(1), "balance_delta_cent": float64(-10000), "cash_ledger_consistent": true},
	}}
	preview := "将退款 ¥100：当前可用余额 ¥5000.00，实际退款 ¥100.00（最多退到当前余额）。确认后执行。"
	reply := preview + "\n\n请回复「确认」执行，或「取消」放弃。"
	failed := Trace{Tool: "refund_recharge", Args: `{}`, ResolvedArgs: `{}`, Phase: "preview", Outcome: "failure", Result: "操作不可执行：请提供要退的订单号或退款金额（元）"}
	valid := Trace{Tool: "refund_recharge", Args: `{"amount_money":100}`, ResolvedArgs: `{"amount_money":100}`, Phase: "preview", Outcome: "pending", Preview: preview, Result: "等待确认"}
	pending := &Pending{Tool: "refund_recharge", TenantID: 19, Args: map[string]any{"amount_money": float64(100)}}
	state := cloneConversationState(base.Before.State)
	state.Own.Audits = []store.AgentAuditLog{
		{ID: 4, SessionID: "session-19", TenantID: 19, Tool: failed.Tool, Args: failed.ResolvedArgs, Result: failed.Result},
		{ID: 5, SessionID: "session-19", TenantID: 19, Tool: valid.Tool, Args: valid.ResolvedArgs, Preview: preview, Result: "pending_confirmation"},
	}
	telemetry := &Telemetry{TurnTelemetry: agent.TurnTelemetry{LangfuseTraceID: "recovery-trace", Runtime: &agent.RuntimeMetadata{}, LLMCalls: 3, LLMUsageResponses: 3, ReplyRepairs: 1}}
	obs := ConversationObservation{Action: "preview", State: state, Pending: pending, Turn: Turn{
		Message: c.Scenario.Turns[0].User, Stage: "turn-1", Status: 200, Pending: pending, Snapshot: state.Own,
		Response: Response{SessionID: "session-19", Reply: reply, Pending: true, PendingTool: "refund_recharge", Preview: preview, Trace: []Trace{failed, valid}, Telemetry: telemetry},
	}}
	obs.Turn.RawBody = string(mustCollectionJSON(t, obs.Turn.Response))
	obs.Turn.TraceRaw = mustCollectionJSON(t, obs.Turn.Response.Trace)
	obs.Turn.TelemetryRaw = mustCollectionJSON(t, telemetry)
	call := agent.ToolCall{ID: "failed-call", Type: "function"}
	call.Function.Name, call.Function.Arguments = failed.Tool, failed.Args
	history := []agent.Message{
		{Role: "user", Content: c.Scenario.Turns[0].User},
		{Role: "assistant", ToolCalls: []agent.ToolCall{call}},
		{Role: "tool", ToolCallID: call.ID, Content: failed.Result},
		{Role: "assistant", Content: reply},
	}
	session := mustCollectionJSON(t, map[string]any{"history": history, "pending_action": map[string]any{
		"tool": pending.Tool, "args": pending.Args, "tenant_id": pending.TenantID, "preview": preview, "idem_key": "refund-recovery-id",
	}})
	after := base.After
	after.State, after.Audits = state, state.Own.Audits
	in := collectionPreviewRecoveryEvidence{
		Turn: 1, BeforeSession: json.RawMessage(`{}`), AfterSession: session,
		Before: base.Before, After: after,
		Pending: &qualityPendingCapture{Pending: *pending, Preview: preview, IdemKey: "refund-recovery-id", SessionID: "session-19", Raw: session},
	}
	return c, f, obs, in
}

func TestCollectionPreviewRecoveryMissingRefundBasisThenCorrectPending(t *testing.T) {
	c, f, obs, in := collectionPreviewRecoveryRefundExample(t)
	old := collectionPreviewFailureEvidence{Turn: in.Turn, BeforeSession: in.BeforeSession, AfterSession: in.AfterSession, Before: in.Before, After: in.After}
	if err := verifyCollectionPreviewFailureEvidence(c, f, in.Before.State, obs, old); err == nil {
		t.Fatal("historical failed-preview policy admitted a new mixed pending shape")
	}
	if err := verifyCollectionPreviewRecoveryEvidence(c, f, in.Before.State, obs, in); err != nil {
		t.Fatalf("proven non-executing failure followed by the correct pending preview was rejected: %v", err)
	}
	if obs.State.Own.BalanceCent != 500000 || obs.State.Own.LedgerBalance != 500000 || len(obs.State.Own.Topups) != 0 {
		t.Fatal("admission fixture executed the refund before confirmation")
	}
	if len(obs.Turn.Response.Trace) != 2 || obs.Turn.Response.Trace[0].Outcome != "failure" || obs.Turn.Response.Trace[1].Outcome != "pending" {
		t.Fatal("admission erased the failed call or final pending preview")
	}
}

// Rebind the independent captured witnesses after a coherent semantic mutation.
// Raw-witness corruption tests deliberately do not call this helper.
func rebindCollectionPreviewRecovery(t *testing.T, obs *ConversationObservation, in *collectionPreviewRecoveryEvidence) {
	t.Helper()
	history := []agent.Message{{Role: "user", Content: obs.Turn.Message}}
	audits := []store.AgentAuditLog{}
	for i, tr := range obs.Turn.Response.Trace {
		result := tr.Result
		if tr.Phase == "preview" && tr.Outcome == "pending" {
			result = "pending_confirmation"
		} else {
			call := agent.ToolCall{ID: string(rune('a' + i)), Type: "function"}
			call.Function.Name, call.Function.Arguments = tr.Tool, tr.Args
			history = append(history, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{call}}, agent.Message{Role: "tool", ToolCallID: call.ID, Content: tr.Result})
		}
		audits = append(audits, store.AgentAuditLog{ID: uint64(i + 4), SessionID: "session-19", TenantID: 19, Tool: tr.Tool, Args: tr.ResolvedArgs, Preview: tr.Preview, Result: result, Confirmed: tr.Confirmed})
	}
	history = append(history, agent.Message{Role: "assistant", Content: obs.Turn.Response.Reply})
	obs.State.Own.Audits = audits
	obs.Turn.Snapshot = obs.State.Own
	in.After.State, in.After.Audits = obs.State, audits
	p := in.Pending
	in.AfterSession = mustCollectionJSON(t, map[string]any{"history": history, "pending_action": map[string]any{"tool": p.Pending.Tool, "args": p.Pending.Args, "tenant_id": p.Pending.TenantID, "preview": p.Preview, "idem_key": p.IdemKey}})
	p.Raw = append(json.RawMessage(nil), in.AfterSession...)
	obs.Turn.RawBody = string(mustCollectionJSON(t, obs.Turn.Response))
	obs.Turn.TraceRaw = mustCollectionJSON(t, obs.Turn.Response.Trace)
	obs.Turn.TelemetryRaw = mustCollectionJSON(t, obs.Turn.Response.Telemetry)
}

func TestCollectionPreviewRecoveryRejectsUnprovenPending(t *testing.T) {
	for _, fault := range []string{"no-failure", "unallowed", "bound-confirmation", "foreign-args", "unknown-tool", "unknown-outcome", "confirmed", "execute", "after-pending", "duplicate-pending", "source-error", "nonempty-missing-basis", "bad-args", "wrong-amount", "wrong-preview", "wrong-idem", "wrong-tenant", "response-not-pending", "unaccounted-model-call", "too-many-model-calls", "too-many-repairs", "jev-error", "raw-response", "raw-trace", "raw-telemetry", "raw-session", "wrong-message", "wrong-stage", "snapshot", "before-pending", "old-history", "sql-effect", "own-effect", "foreign-effect", "missing-audit", "missing-table", "http"} {
		t.Run(fault, func(t *testing.T) {
			c, f, obs, in := collectionPreviewRecoveryRefundExample(t)
			coherent := true
			switch fault {
			case "no-failure":
				obs.Turn.Response.Trace = obs.Turn.Response.Trace[1:]
			case "unallowed":
				c.Scenario.Turns[0].AllowedTools = nil
			case "bound-confirmation":
				c.Scenario.Turns[0].BoundPendingRequired = true
			case "foreign-args":
				obs.Turn.Response.Trace[0].Args, obs.Turn.Response.Trace[0].ResolvedArgs = `{"tenant_id":20}`, `{"tenant_id":20}`
			case "unknown-tool":
				obs.Turn.Response.Trace[0].Tool = "unknown_write"
			case "unknown-outcome":
				obs.Turn.Response.Trace[0].Outcome = "unknown"
			case "confirmed":
				obs.Turn.Response.Trace[0].Confirmed = true
			case "execute":
				obs.Turn.Response.Trace[0].Phase = "execute"
			case "after-pending":
				obs.Turn.Response.Trace = append(obs.Turn.Response.Trace, Trace{Tool: "get_balance", Args: `{}`, ResolvedArgs: `{}`, Phase: "read", Outcome: "success", Result: "余额5000元"})
			case "duplicate-pending":
				obs.Turn.Response.Trace = append(obs.Turn.Response.Trace, obs.Turn.Response.Trace[1])
			case "source-error":
				obs.Turn.Response.Trace[0].Result = "操作不可执行：数据库超时"
			case "nonempty-missing-basis":
				obs.Turn.Response.Trace[0].Args, obs.Turn.Response.Trace[0].ResolvedArgs = `{"extra":1}`, `{"extra":1}`
			case "bad-args":
				obs.Turn.Response.Trace[0].Args = `{"amount_money":`
			case "wrong-amount":
				in.Pending.Pending.Args = map[string]any{"amount_money": float64(200)}
				*obs.Pending = in.Pending.Pending
				obs.Turn.Response.Trace[1].Args, obs.Turn.Response.Trace[1].ResolvedArgs = `{"amount_money":200}`, `{"amount_money":200}`
			case "wrong-preview":
				in.Pending.Preview = "different preview"
			case "wrong-idem":
				in.Pending.IdemKey = ""
			case "wrong-tenant":
				in.Pending.Pending.TenantID = 20
			case "response-not-pending":
				obs.Turn.Response.Pending = false
			case "too-many-model-calls":
				obs.Turn.Response.Telemetry.LLMCalls, obs.Turn.Response.Telemetry.LLMUsageResponses = 6, 6
			case "unaccounted-model-call":
				obs.Turn.Response.Telemetry.LLMCalls, obs.Turn.Response.Telemetry.LLMUsageResponses = 4, 4
			case "too-many-repairs":
				obs.Turn.Response.Telemetry.ReplyRepairs = 2
			case "jev-error":
				obs.Turn.Response.Telemetry.JevErrors = 1
			case "raw-response":
				obs.Turn.RawBody = `{}`
				coherent = false
			case "raw-trace":
				obs.Turn.TraceRaw = json.RawMessage(`[]`)
				coherent = false
			case "raw-telemetry":
				obs.Turn.TelemetryRaw = json.RawMessage(`{}`)
				coherent = false
			case "raw-session":
				in.Pending.Raw = json.RawMessage(`{}`)
				coherent = false
			case "wrong-message":
				obs.Turn.Message = "another request"
			case "wrong-stage":
				obs.Turn.Stage = "turn-2"
			case "snapshot":
				obs.Turn.Snapshot.BalanceCent--
				coherent = false
			case "before-pending":
				in.BeforeSession = append(json.RawMessage(nil), in.AfterSession...)
			case "old-history":
				in.BeforeSession = mustCollectionJSON(t, map[string]any{"history": []agent.Message{{Role: "user", Content: "earlier user"}}, "pending_action": nil})
			case "sql-effect":
				x := in.After.Tables["bills"]
				x.Rows = []string{strings.Repeat("a", 64)}
				in.After.Tables["bills"] = x
			case "own-effect":
				obs.State.Own.BalanceCent--
			case "foreign-effect":
				x := obs.State.Controls[20]
				x.BalanceCent--
				obs.State.Controls[20] = x
			case "missing-audit":
				in.After.Audits = in.After.Audits[:1]
				coherent = false
			case "missing-table":
				delete(in.After.Tables, "bills")
			case "http":
				obs.Turn.Status = 500
			}
			if coherent {
				rebindCollectionPreviewRecovery(t, &obs, &in)
			}
			if err := verifyCollectionPreviewRecoveryEvidence(c, f, in.Before.State, obs, in); err == nil {
				t.Fatal("unproven recovery was admitted", fault)
			}
		})
	}
}

func TestCollectionPreviewRecoveryRejectsHiddenReadReuseIterations(t *testing.T) {
	c, f, obs, in := collectionPreviewRecoveryRefundExample(t)
	c.Scenario.Turns[0].AllowedTools = []string{"get_balance", "get_my_subscription", "refund_recharge"}
	reads := []Trace{
		{Tool: "get_balance", Args: `{}`, ResolvedArgs: `{}`, Phase: "read", Outcome: "success", Result: "现金余额5000元"},
		{Tool: "get_my_subscription", Args: `{}`, ResolvedArgs: `{}`, Phase: "read", Outcome: "success", Result: "当前无订阅"},
	}
	obs.Turn.Response.Trace = append(reads, obs.Turn.Response.Trace...)
	obs.Turn.Response.Telemetry.LLMCalls, obs.Turn.Response.Telemetry.LLMUsageResponses = 4, 4
	obs.Turn.Response.Telemetry.ReplyRepairs = 0
	rebindCollectionPreviewRecovery(t, &obs, &in)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(in.AfterSession, &object); err != nil {
		t.Fatal(err)
	}
	var history []agent.Message
	if err := json.Unmarshal(object["history"], &history); err != nil {
		t.Fatal(err)
	}
	// Two cache reuses have no extra trace/audit, but each consumes a source
	// model iteration. Five stored pairs plus final pending require six calls.
	reused := []agent.Message{}
	for i, tr := range reads {
		call := agent.ToolCall{ID: fmt.Sprintf("reuse-%d", i), Type: "function"}
		call.Function.Name, call.Function.Arguments = tr.Tool, tr.Args
		reused = append(reused, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{call}}, agent.Message{Role: "tool", ToolCallID: call.ID, Content: tr.Result})
	}
	history = append(append(append([]agent.Message(nil), history[:5]...), reused...), history[5:]...)
	object["history"] = mustCollectionJSON(t, history)
	in.AfterSession = mustCollectionJSON(t, object)
	in.Pending.Raw = append(json.RawMessage(nil), in.AfterSession...)
	if err := verifyCollectionPreviewRecoveryEvidence(c, f, in.Before.State, obs, in); err == nil {
		t.Fatal("six source iterations admitted under the five-iteration bound")
	}
}

func TestCollectionPreviewRecoveryMissingBasisWithoutPending(t *testing.T) {
	c, f, obs, in := collectionPreviewRecoveryRefundExample(t)
	in.Pending, obs.Pending, obs.Turn.Pending = nil, nil, nil
	obs.Turn.Response.Pending = false
	obs.Turn.Response.PendingTool, obs.Turn.Response.Preview = "", ""
	obs.Turn.Response.Trace = obs.Turn.Response.Trace[:1]
	obs.Turn.Response.Reply = "请提供退款金额（元）或本人已支付充值订单号，任意一项即可。"
	obs.Turn.Response.Telemetry.LLMCalls, obs.Turn.Response.Telemetry.LLMUsageResponses = 2, 2
	obs.Turn.Response.Telemetry.ReplyRepairs = 0
	obs.State.Own.Audits = obs.State.Own.Audits[:1]
	obs.Turn.Snapshot = obs.State.Own
	in.After.State, in.After.Audits = obs.State, obs.State.Own.Audits
	call := agent.ToolCall{ID: "failed-call", Type: "function"}
	call.Function.Name, call.Function.Arguments = "refund_recharge", `{}`
	in.AfterSession = mustCollectionJSON(t, map[string]any{"history": []agent.Message{
		{Role: "user", Content: obs.Turn.Message},
		{Role: "assistant", ToolCalls: []agent.ToolCall{call}},
		{Role: "tool", ToolCallID: call.ID, Content: obs.Turn.Response.Trace[0].Result},
		{Role: "assistant", Content: obs.Turn.Response.Reply},
	}, "pending_action": nil})
	obs.Turn.RawBody = string(mustCollectionJSON(t, obs.Turn.Response))
	obs.Turn.TraceRaw = mustCollectionJSON(t, obs.Turn.Response.Trace)
	obs.Turn.TelemetryRaw = mustCollectionJSON(t, obs.Turn.Response.Telemetry)
	if err := verifyCollectionPreviewRecoveryEvidence(c, f, in.Before.State, obs, in); err != nil {
		t.Fatalf("known missing basis with a proven non-executing clarification rejected: %v", err)
	}
}

func TestCollectionPreviewRecoveryRetainsHistoricalFailureAdmission(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	e := d.evidence
	in := collectionPreviewRecoveryEvidence{Orders: e.Orders, Turn: e.Turn, BeforeSession: e.BeforeSession, AfterSession: e.AfterSession, Before: e.Before, After: e.After}
	obs := d.steps[0]
	obs.Turn.Message = c.Scenario.Turns[0].User // assigned by the real conversation runner
	if err := verifyCollectionPreviewRecoveryEvidence(c, d.f, e.Before.State, obs, in); err != nil {
		t.Fatalf("historical fully proven failure family lost: %v", err)
	}
}

type collectionPreviewRecoveryTestDriver struct {
	*collectionTestDriver
	evidence collectionPreviewRecoveryEvidence
}

func (d *collectionPreviewRecoveryTestDriver) collectionPreviewRecoveryEvidence(ctx context.Context, turn int) (collectionPreviewRecoveryEvidence, error) {
	if ctx == nil || ctx.Err() != nil || turn != d.evidence.Turn {
		return collectionPreviewRecoveryEvidence{}, fmt.Errorf("capture unavailable")
	}
	return d.evidence, nil
}

func collectionPreviewRecoveryConversationExample(t *testing.T) (ConversationCase, *collectionPreviewRecoveryTestDriver) {
	t.Helper()
	c, f, first, in := collectionPreviewRecoveryRefundExample(t)
	final := cloneConversationState(first.State)
	final.Own.BalanceCent, final.Own.LedgerBalance = 490000, 490000
	final.Own.Topups = []store.Topup{{ID: 1, TenantID: 19, Source: store.TopupRefund, Amount: -10000, RefType: store.TopupRefund, RefID: "refund-recovery-id"}}
	execute := Trace{Tool: "refund_recharge", Args: `{"amount_money":100}`, ResolvedArgs: `{"amount_money":100}`, Confirmed: true, Phase: "execute", Outcome: "success", Result: "退款成功：退回 ¥100.00，当前可用余额 ¥4900.00。"}
	final.Own.Audits = append(final.Own.Audits, store.AgentAuditLog{ID: 6, SessionID: "session-19", TenantID: 19, Tool: execute.Tool, Args: execute.ResolvedArgs, Confirmed: true, Result: execute.Result})
	second := ConversationObservation{Action: "execute", State: final, Turn: Turn{Status: 200, Snapshot: final.Own, Response: Response{SessionID: "session-19", Reply: execute.Result, Trace: []Trace{execute}}}}
	second.Turn.RawBody = string(mustCollectionJSON(t, second.Turn.Response))
	second.Turn.TraceRaw = mustCollectionJSON(t, second.Turn.Response.Trace)
	driver := &collectionTestDriver{&scriptedConversation{f: f, initial: in.Before.State, steps: []ConversationObservation{first, second}}}
	return c, &collectionPreviewRecoveryTestDriver{collectionTestDriver: driver, evidence: in}
}

func TestCollectionPreviewRecoveryRecordsOriginalConfirmationAndReplays(t *testing.T) {
	c, d := collectionPreviewRecoveryConversationExample(t)
	got := recordCollectionPreviewRecoveryConversation(context.Background(), c, d)
	if got.Disposition != "continue_recorded" || len(d.sent) != 2 || len(got.Result.Observations) != 2 || len(got.PreviewRecoveries) != 1 {
		t.Fatalf("proven recovery did not retain the original confirmation: sent=%v disposition=%s issues=%+v", d.sent, got.Disposition, got.Issues)
	}
	if got.Result.Success || len(got.Result.ReviewRequirements) < 3 || got.Result.Final.Own.BalanceCent != 490000 || len(got.Result.Final.Own.Topups) != 1 {
		t.Fatal("recording lost the final state or promoted admission to semantic success")
	}
	if got.Result.Observations[0].Turn.Response.Trace[0].Outcome != "failure" || got.Result.Observations[0].Turn.Response.Telemetry.LLMCalls != 3 {
		t.Fatal("recording erased the failed call or recovery cost")
	}
	if err := verifyCollectionPreviewRecoveryRecording(got); err != nil {
		t.Fatal(err)
	}
	d.evidence.AfterSession[0] = '!'
	if got.PreviewRecoveries[0].AfterSession[0] != '{' || verifyCollectionPreviewRecoveryRecording(got) != nil {
		t.Fatal("retained recovery capture aliases the driver's mutable buffer")
	}
	old := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	if old.Disposition != "halt" || len(d.sent) != 1 || !hasCollectionIssue(old.Issues, "unknown_trace") {
		t.Fatal("old recording policy began admitting recovery", old.Issues)
	}
}

func TestCollectionPreviewRecoveryStopsWrongPendingBeforeConfirmation(t *testing.T) {
	c, d := collectionPreviewRecoveryConversationExample(t)
	d.steps[0].Pending.Args["amount_money"] = float64(200)
	got := recordCollectionPreviewRecoveryConversation(context.Background(), c, d)
	if got.Disposition != "halt" || len(d.sent) != 1 || !hasCollectionIssue(got.Issues, "unknown_trace") || got.Result.Final.Own.BalanceCent != 500000 || len(got.Result.Final.Own.Topups) != 0 {
		t.Fatalf("wrong pending crossed the original confirmation boundary: sent=%v disposition=%s", d.sent, got.Disposition)
	}
}

func TestCollectionPreviewRecoveryCancelledContextCreatesNoConversation(t *testing.T) {
	c, d := collectionPreviewRecoveryConversationExample(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := recordCollectionPreviewRecoveryConversation(ctx, c, d)
	if got.Disposition != "halt" || len(d.sent) != 0 || len(got.PreviewRecoveries) != 0 {
		t.Fatal("cancelled context admitted conversation or capture")
	}
}

func TestCollectionPreviewRecoveryRecordingRejectsAlteredBindings(t *testing.T) {
	c, d := collectionPreviewRecoveryConversationExample(t)
	good := recordCollectionPreviewRecoveryConversation(context.Background(), c, d)
	if err := verifyCollectionPreviewRecoveryRecording(good); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"policy", "result-digest", "disposition", "issues", "capture-missing", "capture-duplicate", "capture-turn", "capture-raw", "result-errors"} {
		t.Run(fault, func(t *testing.T) {
			var bad collectionPreviewRecoveryRecording
			if err := json.Unmarshal(mustCollectionJSON(t, good), &bad); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "policy":
				bad.PolicyID, bad.PolicySHA256 = collectionPreviewFailurePolicyID, collectionPreviewFailurePolicySHA()
			case "result-digest":
				bad.ResultSHA256 = strings.Repeat("a", 64)
			case "disposition":
				bad.Disposition = "halt"
			case "issues":
				bad.Issues = append(bad.Issues, collectionIssue{Code: "unknown_trace", Turn: 1, Disposition: "halt"})
			case "capture-missing":
				bad.PreviewRecoveries = nil
			case "capture-duplicate":
				bad.PreviewRecoveries = append(bad.PreviewRecoveries, bad.PreviewRecoveries[0])
			case "capture-turn":
				bad.PreviewRecoveries[0].Turn = 2
			case "capture-raw":
				bad.PreviewRecoveries[0].AfterSession = json.RawMessage(`{}`)
			case "result-errors":
				bad.Result.Errors = append(bad.Result.Errors, "invented error")
				bad.ResultSHA256 = conversationHash(mustCollectionJSON(t, bad.Result))
			}
			if err := verifyCollectionPreviewRecoveryRecording(bad); err == nil {
				t.Fatal("altered recording gained a valid recovery replay", fault)
			}
		})
	}
}

func collectionPreviewRecoveryToolExample(t *testing.T, tool string) (ConversationCase, Fixture, ConversationObservation, collectionPreviewRecoveryEvidence) {
	t.Helper()
	c, f, obs, in := collectionPreviewRecoveryRefundExample(t)
	if tool == "refund_recharge" {
		return c, f, obs, in
	}
	f.Standard = store.Plan{ID: 47, Name: "月度·标准", PriceMoney: 20, RefreshHours: 5, PlanType: store.PlanTypeRecurring, TierQuota: `{"文本":100000,"推理":50000}`}
	f.Flagship = store.Plan{ID: 48, Name: "月度·旗舰", PriceMoney: 50, RefreshHours: 5, PlanType: store.PlanTypeRecurring, TierQuota: `{"文本":300000,"推理":150000}`}
	state := cloneConversationState(in.Before.State)
	state.AnswerAuthority.Plans = []store.Plan{f.Standard, f.Flagship}
	preview := ""
	args, rawArgs := map[string]any{"amount_money": float64(100)}, `{"amount_money":100}`
	c.Scenario.Operation.Tool = tool
	c.Scenario.Turns[0].AllowedTools = []string{tool}
	c.Scenario.Turns[0].User = "申请对应操作，请先预览"
	if tool == "recharge" {
		preview = "将模拟充值 ¥100（模拟支付，不产生真实扣款）。确认后到账。"
		obs.Turn.Response.Trace[0] = Trace{Tool: tool, Args: `{"amount_money":0}`, ResolvedArgs: `{"amount_money":0}`, Phase: "preview", Outcome: "failure", Result: "操作不可执行：充值金额必须大于 0"}
	} else if tool == "cancel_subscription" {
		end := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
		state.Own.Subscriptions = []store.Subscription{{ID: 1, TenantID: 19, PlanID: 47, PlanType: store.PlanTypeRecurring, Status: store.SubActive, CycleEnd: &end}}
		preview = "将退订当前套餐（#47）：当期不退、只取消下期续费，本期用到 2026-12-01 为止。确认后退订。"
		args, rawArgs = map[string]any{}, `{}`
		c.Scenario.Operation.Arguments = map[string]any{}
		c.Scenario.Turns[0].AllowedTools = []string{"refund_recharge", tool}
	} else {
		args, rawArgs = map[string]any{"plan_id": float64(48), "plan_name": "月度·旗舰"}, `{"plan_id":48,"plan_name":"月度·旗舰"}`
		c.Scenario.Operation.Arguments = map[string]any{"plan_reference": "flagship"}
		obs.Turn.Response.Trace[0] = Trace{Tool: tool, Args: `{"plan_name":"pro plan"}`, ResolvedArgs: `{"plan_name":"pro plan"}`, Phase: "preview", Outcome: "failure", Result: "操作不可执行：未找到名称为「proplan」的在售定期套餐，请查询套餐目录"}
		switch tool {
		case "subscribe_plan":
			preview = "将订阅「月度·旗舰」：月费 ¥50，每 5 小时刷新额度：推理15万/文本30万。确认后开通。"
		case "change_plan":
			preview = "将把当前订阅改为定期套餐「月度·旗舰」：下期续费生效，当期已订购额度与到期日不变。确认后生效。"
		default:
			t.Fatal("unknown synthetic preview tool", tool)
		}
	}
	in.Before.State = state
	obs.State = cloneConversationState(state)
	in.Pending.Pending = Pending{Tool: tool, TenantID: 19, Args: args}
	*obs.Pending = in.Pending.Pending
	in.Pending.Preview = preview
	obs.Turn.Message = c.Scenario.Turns[0].User
	obs.Turn.Response.PendingTool, obs.Turn.Response.Preview = tool, preview
	obs.Turn.Response.Reply = preview + "\n\n请回复「确认」执行，或「取消」放弃。"
	obs.Turn.Response.Trace[1] = Trace{Tool: tool, Args: rawArgs, ResolvedArgs: rawArgs, Phase: "preview", Outcome: "pending", Preview: preview, Result: "等待确认"}
	rebindCollectionPreviewRecovery(t, &obs, &in)
	return c, f, obs, in
}

func TestCollectionPreviewRecoveryRequiresSourceRenderedPreview(t *testing.T) {
	for _, tc := range []struct{ tool, old, wrong string }{
		{"refund_recharge", "¥100", "¥1"},
		{"recharge", "¥100", "¥1"},
		{"subscribe_plan", "¥50", "¥51"},
		{"change_plan", "月度·旗舰", "月度·标准"},
		{"cancel_subscription", "#47", "#48"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			c, f, obs, in := collectionPreviewRecoveryToolExample(t, tc.tool)
			if err := verifyCollectionPreviewRecoveryEvidence(c, f, in.Before.State, obs, in); err != nil {
				t.Fatal("correct literal source preview rejected", err)
			}
			wrong := strings.ReplaceAll(in.Pending.Preview, tc.old, tc.wrong)
			if wrong == in.Pending.Preview {
				t.Fatal("test did not alter the source-rendered value")
			}
			in.Pending.Preview = wrong
			obs.Turn.Response.Preview, obs.Turn.Response.Trace[1].Preview = wrong, wrong
			obs.Turn.Response.Reply = wrong + "\n\n请回复「确认」执行，或「取消」放弃。"
			rebindCollectionPreviewRecovery(t, &obs, &in)
			if err := verifyCollectionPreviewRecoveryEvidence(c, f, in.Before.State, obs, in); err == nil {
				t.Fatal("coherently wrong preview admitted before confirmation")
			}
		})
	}
}
