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

// The regression keeps the captured blocker shape: an unknown pro plan preview,
// a successful catalogue read and clarification, then explicit selection/confirm.
// SQL/session material is synthetic; it never certifies the stopped original.
func collectionFailedPreviewExample(t *testing.T) (ConversationCase, *collectionFailedPreviewDriver) {
	t.Helper()
	c := rechargeConversation()
	c.SourceID = "change_plan-073"
	c.Scenario.Profile = "active_standard"
	c.Scenario.Operation = &ConversationOperation{Tool: "change_plan", Arguments: map[string]any{"plan_reference": "flagship"}}
	c.Scenario.Terminal = map[string]any{"authorized_write_count": float64(1), "pending_plan": "flagship", "cash_ledger_consistent": true}
	c.Scenario.Turns[0].User = "帮我把套餐改成 pro plan"
	c.Scenario.Turns[0].AllowedTools = []string{"change_plan", "list_plans"}
	c.Scenario.Turns[1].User = "改成旗舰套餐"
	c.Scenario.Turns[1].AllowedTools = []string{"change_plan"}
	f, initial := conversationFixture()
	f.Standard.Name, f.Standard.PlanType, f.Standard.TierQuota = "月度·标准", store.PlanTypeRecurring, `{"文本":100000}`
	f.Flagship.Name, f.Flagship.PlanType = "月度·旗舰", store.PlanTypeRecurring
	initial.CapturedAt = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	start, end := initial.CapturedAt.Add(-24*time.Hour), initial.CapturedAt.Add(29*24*time.Hour)
	initial.Own.Subscriptions = []store.Subscription{{ID: 1, TenantID: 19, PlanID: 47, PlanType: store.PlanTypeRecurring, Status: store.SubActive, AutoRenew: true, CycleStart: &start, CycleEnd: &end}}
	initial.Allowance = []store.TierRemain{{Tier: "文本", Quota: 100000, Consumed: 3000, Remaining: 97000}}
	initial.AnswerAuthority = &ConversationAnswerAuthority{TenantID: 19, CapturedAt: initial.CapturedAt, CatalogComplete: true, Plans: []store.Plan{f.Standard, f.Flagship}, BillsLimit: 10, BillsPhase: "settle", BillsStatus: "settled", ConsumptionComplete: true, ConsumptionDays: 7, RefreshHours: 24}
	failed := Trace{Tool: "change_plan", Args: `{"plan_name":"pro plan"}`, ResolvedArgs: `{"plan_name":"pro plan"}`, Phase: "preview", Outcome: "failure", Result: "操作不可执行：未找到名称为「proplan」的在售定期套餐，请查询套餐目录"}
	read := Trace{Tool: "list_plans", Args: `{}`, ResolvedArgs: `{}`, Phase: "read", Outcome: "success", Result: "可选月度·标准、月度·旗舰"}
	first := ConversationObservation{Action: "preview", State: cloneConversationState(initial), Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "未找到 pro plan，请选择标准或旗舰。", Trace: []Trace{failed, read}, Telemetry: &Telemetry{TurnTelemetry: agent.TurnTelemetry{LangfuseTraceID: "trace", Runtime: &agent.RuntimeMetadata{}, LLMCalls: 3, LLMUsageResponses: 3}}}}}
	first.State.Own.Audits = []store.AgentAuditLog{{ID: 4, SessionID: "session-19", TenantID: 19, Tool: failed.Tool, Args: failed.ResolvedArgs, Result: failed.Result}, {ID: 5, SessionID: "session-19", TenantID: 19, Tool: read.Tool, Args: read.ResolvedArgs, Result: read.Result}}
	pending := &Pending{Tool: "change_plan", TenantID: 19, Args: map[string]any{"plan_id": float64(48), "plan_name": "月度·旗舰"}}
	second := ConversationObservation{Action: "preview", State: first.State, Pending: pending, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "预览：改成旗舰，下期生效。", Pending: true, PendingTool: "change_plan", Trace: []Trace{{Tool: "change_plan", Args: `{"plan_name":"旗舰套餐"}`, ResolvedArgs: `{"plan_id":48,"plan_name":"月度·旗舰"}`, Phase: "preview", Outcome: "pending"}}}}}
	final := cloneConversationState(first.State)
	flagship := uint64(48)
	final.Own.Subscriptions[0].PendingPlanID = &flagship
	third := ConversationObservation{Action: "execute", State: final, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "已设置下期切换旗舰。", Trace: []Trace{{Tool: "change_plan", Confirmed: true, ResolvedArgs: `{"plan_id":48,"plan_name":"月度·旗舰"}`, Phase: "execute", Outcome: "success"}}}}}
	first.Turn.RawBody = string(mustCollectionJSON(t, first.Turn.Response))
	first.Turn.TraceRaw = mustCollectionJSON(t, first.Turn.Response.Trace)
	tables := map[string]qualityTableCapture{}
	for _, key := range qualityPreviewTableNames() {
		tables[key] = qualityTableCapture{Columns: []string{"id"}, Rows: []string{}}
	}
	columns := []string{"id", "session_id", "tenant_id", "api_key_id", "created_at", "last_active_at"}
	beforeSessions := []store.AgentSession{}
	afterSessions := []store.AgentSession{{ID: 7, SessionID: "session-19", TenantID: 19, CreatedAt: initial.CapturedAt, LastActiveAt: initial.CapturedAt}}
	tables["agent_sessions"], _ = qualitySessionTableCapture(beforeSessions, columns)
	afterTables := map[string]qualityTableCapture{}
	for k, v := range tables {
		afterTables[k] = v
	}
	afterTables["agent_sessions"], _ = qualitySessionTableCapture(afterSessions, columns)
	history := []agent.Message{{Role: "user", Content: c.Scenario.Turns[0].User}}
	for i, tr := range first.Turn.Response.Trace {
		tc := agent.ToolCall{ID: fmt.Sprintf("call-%d", i), Type: "function"}
		tc.Function.Name, tc.Function.Arguments = tr.Tool, tr.Args
		history = append(history, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}, agent.Message{Role: "tool", ToolCallID: tc.ID, Content: tr.Result})
	}
	history = append(history, agent.Message{Role: "assistant", Content: first.Turn.Response.Reply})
	evidence := collectionPreviewFailureEvidence{Turn: 1, BeforeSession: json.RawMessage(`{}`), AfterSession: mustCollectionJSON(t, map[string]any{"history": history, "pending_action": nil}), Before: qualityEffectCapture{State: initial, Tables: tables, Audits: []store.AgentAuditLog{}, Sessions: beforeSessions}, After: qualityEffectCapture{State: first.State, Tables: afterTables, Audits: first.State.Own.Audits, Sessions: afterSessions}}
	return c, &collectionFailedPreviewDriver{collectionTestDriver: &collectionTestDriver{&scriptedConversation{f: f, initial: initial, steps: []ConversationObservation{first, second, third}}}, evidence: evidence}
}
func mustCollectionJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type collectionFailedPreviewDriver struct {
	*collectionTestDriver
	evidence collectionPreviewFailureEvidence
}

func (d *collectionFailedPreviewDriver) collectionPreviewFailureEvidence(_ context.Context, turn int) (collectionPreviewFailureEvidence, error) {
	if turn != d.evidence.Turn {
		return collectionPreviewFailureEvidence{}, fmt.Errorf("unavailable")
	}
	return d.evidence, nil
}

func TestCollectionPreviewFailureContinuesOriginalTurnsAndReplays(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	got := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	if got.Disposition != "continue_recorded" || len(d.sent) != 3 || len(got.Result.Observations) != 3 {
		t.Fatalf("known no-effect failure stopped valid selection/confirmation: sent=%v disposition=%s issues=%+v", d.sent, got.Disposition, got.Issues)
	}
	if got.Result.Success || got.Result.StructuralPass || got.Result.Status != "failed" {
		t.Fatal("failed preview/action negative promoted", got.Result.Status)
	}
	if len(got.PreviewFailures) != 1 {
		t.Fatal("missing retained capture")
	}
	if err := verifyCollectionPreviewFailureRecording(got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(got.Result.Errors, "|"), "unexpected action preview") {
		t.Fatal("lost original action negative")
	}
	old := recordCollectionConversation(context.Background(), c, d)
	if old.Disposition != "halt" || len(d.sent) != 1 || !hasCollectionIssue(old.Issues, "unknown_trace") {
		t.Fatal("v1 failed-preview stop changed", old.Issues)
	}
	if collectionPolicySHA() != "604c68b1c192c17594ba422b85367579cabd60c31623af6a9632278e401ae970" {
		t.Fatal("v1 identity changed")
	}
	if verifyCollectionRecording(old) != nil {
		t.Fatal("v1 replay changed")
	}
}
func hasCollectionIssue(issues []collectionIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestCollectionPreviewFailureRejectsUnprovenAdmission(t *testing.T) {
	for _, fault := range []string{"no-capture", "turn", "state-link", "own-effect", "control-effect", "sql-effect", "missing-table", "nil-rows", "after-session-hash", "foreign-session", "session-time", "audit-missing", "audit-extra", "audit-result", "audit-guard", "audit-foreign", "raw-response", "raw-trace", "before-pending", "after-pending", "history", "hidden-pending", "response-pending", "confirmed", "unknown-tool", "unallowed-tool", "phase", "execute-failure", "read-failure", "foreign-args", "malformed-args", "duplicate-args", "args-mismatch", "unknown-result", "infra-result", "preview-text", "runtime", "usage", "http"} {
		t.Run(fault, func(t *testing.T) {
			c, d := collectionFailedPreviewExample(t)
			o := &d.steps[0]
			e := &d.evidence
			rebind := true
			switch fault {
			case "no-capture":
				e.Turn = 99
			case "turn":
				e.Turn = 0
			case "state-link":
				e.Before.State.Own.BalanceCent++
			case "own-effect":
				o.State = cloneConversationState(o.State)
				o.State.Own.BalanceCent++
				o.State.Own.LedgerBalance++
				e.After.State = o.State
			case "control-effect":
				o.State = cloneConversationState(o.State)
				s := o.State.Controls[20]
				s.BalanceCent++
				s.LedgerBalance++
				o.State.Controls[20] = s
				e.After.State = o.State
			case "sql-effect":
				v := e.After.Tables["bills"]
				v.Rows = []string{strings.Repeat("a", 64)}
				e.After.Tables["bills"] = v
			case "missing-table":
				delete(e.After.Tables, "bills")
			case "nil-rows":
				v := e.After.Tables["bills"]
				v.Rows = nil
				e.After.Tables["bills"] = v
			case "after-session-hash":
				v := e.After.Tables["agent_sessions"]
				v.Rows = []string{strings.Repeat("a", 64)}
				e.After.Tables["agent_sessions"] = v
			case "foreign-session":
				e.After.Sessions[0].TenantID = 20
				e.After.Tables["agent_sessions"], _ = qualitySessionTableCapture(e.After.Sessions, e.After.Tables["agent_sessions"].Columns)
			case "session-time":
				e.After.Sessions[0].LastActiveAt = e.After.Sessions[0].LastActiveAt.Add(time.Minute)
				e.After.Tables["agent_sessions"], _ = qualitySessionTableCapture(e.After.Sessions, e.After.Tables["agent_sessions"].Columns)
			case "audit-missing":
				e.After.Audits = []store.AgentAuditLog{}
			case "audit-extra":
				e.After.Audits = append(e.After.Audits, store.AgentAuditLog{ID: 6, TenantID: 19, Tool: "change_plan"})
			case "audit-result":
				e.After.Audits[0].Result = "pending_confirmation"
			case "audit-guard":
				e.After.Audits[0].Guard = "blocked"
			case "audit-foreign":
				e.After.Audits[0].TenantID = 20
			case "raw-response":
				o.Turn.RawBody = `{}`
				rebind = false
			case "raw-trace":
				o.Turn.TraceRaw = json.RawMessage(`[]`)
				rebind = false
			case "before-pending":
				e.BeforeSession = json.RawMessage(`{"history":[],"pending_action":{}}`)
			case "after-pending":
				e.AfterSession = json.RawMessage(`{"history":[],"pending_action":{}}`)
			case "history":
				e.AfterSession = json.RawMessage(`{"history":[{"role":"assistant","content":"forged"}],"pending_action":null}`)
			case "hidden-pending":
				o.Pending = &Pending{Tool: "change_plan", TenantID: 19}
			case "response-pending":
				o.Turn.Response.Pending = true
			case "confirmed":
				o.Turn.Response.Trace[0].Confirmed = true
			case "unknown-tool":
				o.Turn.Response.Trace[0].Tool = "unknown"
			case "unallowed-tool":
				c.Scenario.Turns[0].AllowedTools = []string{"list_plans"}
			case "phase":
				o.Turn.Response.Trace[0].Phase = "unknown"
			case "execute-failure":
				o.Turn.Response.Trace[0].Phase = "execute"
			case "read-failure":
				o.Turn.Response.Trace[1].Outcome = "failure"
			case "foreign-args":
				o.Turn.Response.Trace[0].Args = `{"plan_name":"pro plan","tenant_id":20}`
			case "malformed-args":
				o.Turn.Response.Trace[0].Args = `{"plan_name":1}`
			case "duplicate-args":
				o.Turn.Response.Trace[0].Args = `{"plan_name":"standard","plan_name":"pro plan"}`
			case "args-mismatch":
				o.Turn.Response.Trace[0].ResolvedArgs = `{"plan_name":"other"}`
			case "unknown-result":
				o.Turn.Response.Trace[0].Result = "操作不可执行：unknown"
			case "infra-result":
				o.Turn.Response.Trace[0].Result = "操作不可执行：context deadline exceeded"
			case "preview-text":
				o.Turn.Response.Trace[0].Preview = "pending preview"
			case "runtime":
				o.Turn.Response.Telemetry.Runtime = nil
			case "usage":
				o.Turn.Response.Telemetry.LLMUsageMissing = 1
			case "http":
				o.Turn.Status = 503
			}
			if rebind {
				o.Turn.RawBody = string(mustCollectionJSON(t, o.Turn.Response))
				o.Turn.TraceRaw = mustCollectionJSON(t, o.Turn.Response.Trace)
			}
			got := recordCollectionPreviewFailureConversation(context.Background(), c, d)
			if got.Disposition != "halt" || len(d.sent) != 1 || len(got.PreviewFailures) != 0 {
				t.Fatalf("unproven %s admitted: sent%d issues%+v", fault, len(d.sent), got.Issues)
			}
		})
	}
}

func TestCollectionPreviewFailureBusinessBranches(t *testing.T) {
	tests := []struct {
		tool, args, result           string
		emptyBalance, noSubscription bool
		want                         bool
	}{
		{"recharge", `{"amount_money":0}`, "操作不可执行：充值金额必须大于 0", false, false, true},
		{"recharge", `{"amount_money":-1}`, "操作不可执行：充值金额必须大于 0", false, false, true},
		{"recharge", `{"amount_money":0.5}`, "操作不可执行：充值金额必须大于 0", false, false, false},
		{"recharge", `{"amount_money":"0"}`, "操作不可执行：充值金额必须大于 0", false, false, false},
		{"recharge", `{}`, "操作不可执行：缺少参数 amount_money", false, false, false},
		{"recharge", `{"amount_money":100}`, "操作不可执行：context deadline exceeded", false, false, false},
		{"refund_recharge", `{"amount_money":0}`, "操作不可执行：退款金额必须大于 0", false, false, true},
		{"refund_recharge", `{"amount_money":10}`, "操作不可执行：当前余额为 0，无可退额度", true, false, true},
		{"refund_recharge", `{"amount_money":10}`, "操作不可执行：当前余额为 0，无可退额度", false, false, false},
		{"refund_recharge", `{"order_no":"owned-19"}`, "操作不可执行：当前余额为 0，订单 owned-19 无可退额度", true, false, true},
		{"refund_recharge", `{"order_no":"unknown"}`, "操作不可执行：未找到你名下的订单 unknown", false, false, false},
		{"refund_recharge", `{"amount_money":1,"order_no":"owned-19"}`, "操作不可执行：当前余额为 0，无可退额度", true, false, false},
		{"cancel_subscription", `{}`, "操作不可执行：当前没有生效中的订阅，无需退订", false, true, true},
		{"cancel_subscription", `{}`, "操作不可执行：当前没有生效中的订阅，无需退订", false, false, false},
		{"subscribe_plan", `{"plan_name":"pro plan"}`, "操作不可执行：未找到名称为「proplan」的在售定期套餐，请查询套餐目录", false, false, true},
		{"change_plan", `{"plan_name":"月度"}`, "操作不可执行：套餐名称「月度」匹配多个在售定期套餐，请提供完整名称", false, false, true},
		{"change_plan", `{"plan_id":999}`, "操作不可执行：套餐 #999 不存在", false, false, true},
		{"change_plan", `{"plan_id":47,"plan_name":"月度·旗舰"}`, "操作不可执行：套餐 ID 与名称不一致，请重新选择", false, false, true},
		{"change_plan", `{"plan_id":48}`, "操作不可执行：未找到名称为「proplan」的在售定期套餐，请查询套餐目录", false, false, false},
		{"change_plan", `{"plan_name":"pro plan","unknown":1}`, "操作不可执行：未找到名称为「proplan」的在售定期套餐，请查询套餐目录", false, false, false},
		{"change_plan", `{"plan_id":1.5}`, "操作不可执行：套餐 #1 不存在", false, false, false},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d-%s", i, tt.tool), func(t *testing.T) {
			c, d := collectionFailedPreviewExample(t)
			o := &d.steps[0]
			e := &d.evidence
			if tt.emptyBalance {
				e.Before.State.Own.BalanceCent = 0
				e.Before.State.Own.LedgerBalance = 0
			}
			if tt.noSubscription {
				e.Before.State.Own.Subscriptions = nil
				e.Before.State.Allowance = nil
			}
			e.Before.State.Own.Orders = []store.RechargeOrder{{ID: 9, TenantID: 19, OrderNo: "owned-19", Status: store.RechargePaid, AmountCent: 10000}}
			o.State = cloneConversationState(e.Before.State)
			tr := &o.Turn.Response.Trace[0]
			tr.Tool, tr.Args, tr.ResolvedArgs, tr.Result = tt.tool, tt.args, tt.args, tt.result
			c.Scenario.Turns[0].AllowedTools = []string{tt.tool, "list_plans"}
			o.Turn.Message = c.Scenario.Turns[0].User
			o.State.Own.Audits = []store.AgentAuditLog{{ID: 4, TenantID: 19, SessionID: "session-19", Tool: tr.Tool, Args: tr.ResolvedArgs, Result: tr.Result}, {ID: 5, TenantID: 19, SessionID: "session-19", Tool: "list_plans", Args: `{}`, Result: o.Turn.Response.Trace[1].Result}}
			e.After.State = o.State
			e.After.Audits = o.State.Own.Audits
			rebindCollectionFailedPreview(t, o, e)
			err := verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e)
			if (err == nil) != tt.want {
				t.Fatalf("business branch want%v: %v", tt.want, err)
			}
		})
	}
}
func rebindCollectionFailedPreview(t *testing.T, o *ConversationObservation, e *collectionPreviewFailureEvidence) {
	t.Helper()
	o.Turn.RawBody = string(mustCollectionJSON(t, o.Turn.Response))
	o.Turn.TraceRaw = mustCollectionJSON(t, o.Turn.Response.Trace)
	history := []agent.Message{{Role: "user", Content: o.Turn.Message}}
	for i, tr := range o.Turn.Response.Trace {
		tc := agent.ToolCall{ID: fmt.Sprintf("call-%d", i), Type: "function"}
		tc.Function.Name, tc.Function.Arguments = tr.Tool, tr.Args
		history = append(history, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}, agent.Message{Role: "tool", ToolCallID: tc.ID, Content: tr.Result})
	}
	history = append(history, agent.Message{Role: "assistant", Content: o.Turn.Response.Reply})
	e.AfterSession = mustCollectionJSON(t, map[string]any{"history": history, "pending_action": nil})
}
func TestCollectionPreviewFailureReplayRejectsCoherentTampering(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	original := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	for _, fault := range []string{"identity", "policy", "result-digest", "capture", "extra-capture", "turn", "disposition", "issues", "action", "source", "capture-effect"} {
		t.Run(fault, func(t *testing.T) {
			var bad collectionPreviewFailureRecording
			if json.Unmarshal(mustCollectionJSON(t, original), &bad) != nil {
				t.Fatal("clone")
			}
			switch fault {
			case "identity":
				bad.PolicyID = collectionPolicyID
			case "policy":
				bad.PolicySHA256 = collectionPolicySHA()
			case "result-digest":
				bad.ResultSHA256 = strings.Repeat("0", 64)
			case "capture":
				bad.PreviewFailures = nil
			case "extra-capture":
				bad.PreviewFailures = append(bad.PreviewFailures, bad.PreviewFailures[0])
			case "turn":
				bad.PreviewFailures[0].Turn = 2
			case "disposition":
				bad.Disposition = "halt"
			case "issues":
				bad.Issues = nil
			case "action":
				bad.Result.Observations[0].Action = "clarify"
			case "source":
				bad.Result.Case.Scenario.Turns[0].AllowedTools = []string{"list_plans"}
			case "capture-effect":
				bad.PreviewFailures[0].After.State.Own.BalanceCent++
			}
			if fault != "result-digest" {
				bad.ResultSHA256 = qualityJSONSHA(bad.Result)
			}
			if verifyCollectionPreviewFailureRecording(bad) == nil {
				t.Fatal("coherent tampering admitted")
			}
		})
	}
	if verifyCollectionRecording(original.collectionRecording) == nil {
		t.Fatal("v2 recording accepted as v1")
	}
}

type collectionPreviewFailureCancelledDriver struct {
	*collectionFailedPreviewDriver
	cancel context.CancelFunc
}

func (d *collectionPreviewFailureCancelledDriver) Step(ctx context.Context, f Fixture, session, message string) (ConversationObservation, error) {
	o, err := d.collectionFailedPreviewDriver.Step(ctx, f, session, message)
	d.cancel()
	return o, err
}
func TestCollectionPreviewFailureCancellationStillHalts(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := recordCollectionPreviewFailureConversation(ctx, c, &collectionPreviewFailureCancelledDriver{d, cancel})
	if len(d.sent) != 1 || got.Disposition != "halt" || len(got.PreviewFailures) != 0 || !hasCollectionIssue(got.Issues, "context_cancelled") {
		t.Fatal("cancelled preview continued")
	}
}

func TestCollectionPreviewFailureCompleteOrdersProveUnknownOrForeignTarget(t *testing.T) {
	for _, target := range []string{"unknown", "foreign-20"} {
		t.Run(target, func(t *testing.T) {
			c, d := collectionFailedPreviewExample(t)
			o := &d.steps[0]
			e := &d.evidence
			c.Scenario.Turns[0].AllowedTools = []string{"refund_recharge", "list_plans"}
			tr := &o.Turn.Response.Trace[0]
			tr.Tool = "refund_recharge"
			tr.Args = fmt.Sprintf(`{"order_no":%q}`, target)
			tr.ResolvedArgs = tr.Args
			tr.Result = "操作不可执行：未找到你名下的订单 " + target
			o.Turn.Message = c.Scenario.Turns[0].User
			o.State.Own.Audits[0].Tool, o.State.Own.Audits[0].Args, o.State.Own.Audits[0].Result = tr.Tool, tr.Args, tr.Result
			columns := []string{"id", "tenant_id", "order_no", "amount_money", "amount_cent", "status", "paid_at", "created_at", "updated_at"}
			e.Orders = &collectionPreviewFailureOrders{Columns: columns, Rows: []store.RechargeOrder{{ID: 9, TenantID: 19, OrderNo: "owned-19", AmountMoney: 100, AmountCent: 10000, Status: store.RechargePaid, CreatedAt: d.initial.CapturedAt, UpdatedAt: d.initial.CapturedAt}, {ID: 10, TenantID: 20, OrderNo: "foreign-20", AmountMoney: 100, AmountCent: 10000, Status: store.RechargePaid, CreatedAt: d.initial.CapturedAt, UpdatedAt: d.initial.CapturedAt}}}
			// Literal hashes were calculated independently from the nine SQL cells.
			table := qualityTableCapture{Columns: columns, Rows: []string{"b0b3e25989ba204e0217719e33470a49ccc562cf7a3d8c7209f7e64313b4fd52", "cbf9777185a2ff7fae4ac3f4875da8c29fe2465ac56290f022f485255321d040"}}
			e.Before.Tables["recharge_orders"], e.After.Tables["recharge_orders"] = table, table
			control := e.Before.State.Controls[20]
			control.Orders = []store.RechargeOrder{e.Orders.Rows[1]}
			e.Before.State.Controls[20] = control
			o.State.Controls[20] = control
			e.Before.State.Own.Orders = []store.RechargeOrder{e.Orders.Rows[0]}
			o.State.Own.Orders = []store.RechargeOrder{e.Orders.Rows[0]}
			e.After.State = o.State
			rebindCollectionFailedPreview(t, o, e)
			if err := verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e); err != nil {
				t.Fatal("complete SQL-projected orders did not prove absent owned target", err)
			}
			raw := mustCollectionJSON(t, e)
			for _, fault := range []string{"missing", "nil-rows", "drop", "extra", "foreign-mismatch", "hash", "columns", "duplicate", "own-target"} {
				t.Run(fault, func(t *testing.T) {
					var bad collectionPreviewFailureEvidence
					if json.Unmarshal(raw, &bad) != nil {
						t.Fatal("clone")
					}
					switch fault {
					case "missing":
						bad.Orders = nil
					case "nil-rows":
						bad.Orders.Rows = nil
					case "drop":
						bad.Orders.Rows = bad.Orders.Rows[:1]
					case "extra":
						bad.Orders.Rows = append(bad.Orders.Rows, store.RechargeOrder{ID: 11, TenantID: 99, OrderNo: "other"})
					case "foreign-mismatch":
						bad.Orders.Rows[1].TenantID = 19
					case "hash":
						bad.After.Tables["recharge_orders"] = qualityTableCapture{Columns: columns, Rows: []string{strings.Repeat("a", 64)}}
					case "columns":
						bad.Orders.Columns = []string{"id"}
					case "duplicate":
						bad.Orders.Rows[1] = bad.Orders.Rows[0]
					case "own-target":
						bad.Orders.Rows[0].OrderNo = target
					}
					if verifyCollectionPreviewFailureEvidence(c, d.f, bad.Before.State, *o, bad) == nil {
						t.Fatal("incomplete/unbound private orders accepted")
					}
				})
			}
		})
	}
}

func TestCollectionPreviewFailureRepeatedProvenFailuresContinue(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	o := &d.steps[0]
	e := &d.evidence
	o.Turn.Message = c.Scenario.Turns[0].User
	o.Turn.Response.Trace = append([]Trace{o.Turn.Response.Trace[0]}, o.Turn.Response.Trace...)
	o.State.Own.Audits = append([]store.AgentAuditLog{{ID: 6, TenantID: 19, SessionID: "session-19", Tool: o.Turn.Response.Trace[0].Tool, Args: o.Turn.Response.Trace[0].ResolvedArgs, Result: o.Turn.Response.Trace[0].Result}}, o.State.Own.Audits...)
	e.After.State = o.State
	e.After.Audits = o.State.Own.Audits
	rebindCollectionFailedPreview(t, o, e)
	got := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	if got.Disposition != "continue_recorded" || len(d.sent) != 3 || len(got.PreviewFailures) != 1 || verifyCollectionPreviewFailureRecording(got) != nil {
		t.Fatal("multiple independently proven no-effect failures stopped original suffix", got.Issues)
	}
}
func TestCollectionPreviewFailureExactReadReuseIsNotAnotherEffect(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	o := &d.steps[0]
	e := &d.evidence
	o.Turn.Message = c.Scenario.Turns[0].User
	var session struct {
		History []agent.Message `json:"history"`
		Pending any             `json:"pending_action"`
	}
	if json.Unmarshal(e.AfterSession, &session) != nil {
		t.Fatal("history")
	}
	reused := append([]agent.Message(nil), session.History[3:5]...)
	reused[0].ToolCalls = append([]agent.ToolCall(nil), reused[0].ToolCalls...)
	reused[0].ToolCalls[0].ID = "read-reuse"
	reused[1].ToolCallID = "read-reuse"
	session.History = append(append(session.History[:5:5], reused...), session.History[5])
	e.AfterSession = mustCollectionJSON(t, session)
	if err := verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e); err != nil {
		t.Fatal("source read reuse rejected despite no trace/audit/effect", err)
	}
	// The source permits exactly one reuse, with the same result, per read key.
	reused[1].Content = "changed result"
	session.History[6] = reused[1]
	e.AfterSession = mustCollectionJSON(t, session)
	if verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e) == nil {
		t.Fatal("changed reused read result admitted")
	}
}
func TestCollectionPreviewFailureStrictResponseAndReadArguments(t *testing.T) {
	for _, fault := range []string{"unknown-response-field", "unknown-trace-field", "read-unknown-field", "read-args-mismatch", "read-wrong-type"} {
		t.Run(fault, func(t *testing.T) {
			c, d := collectionFailedPreviewExample(t)
			o := &d.steps[0]
			e := &d.evidence
			o.Turn.Message = c.Scenario.Turns[0].User
			switch fault {
			case "unknown-response-field":
				var object map[string]any
				json.Unmarshal([]byte(o.Turn.RawBody), &object)
				object["unexpected"] = true
				o.Turn.RawBody = string(mustCollectionJSON(t, object))
			case "unknown-trace-field":
				var objects []map[string]any
				json.Unmarshal(o.Turn.TraceRaw, &objects)
				objects[0]["unexpected"] = true
				o.Turn.TraceRaw = mustCollectionJSON(t, objects)
			default:
				tr := &o.Turn.Response.Trace[1]
				if fault == "read-unknown-field" {
					tr.Args = `{"unexpected":1}`
					tr.ResolvedArgs = tr.Args
				} else if fault == "read-args-mismatch" {
					tr.Args = `{"tenant_id":19}`
				} else {
					tr.Tool = "get_recent_bills"
					tr.Args = `{"limit":"3"}`
					tr.ResolvedArgs = tr.Args
					c.Scenario.Turns[0].AllowedTools = append(c.Scenario.Turns[0].AllowedTools, tr.Tool)
				}
				o.State.Own.Audits[1].Tool, o.State.Own.Audits[1].Args = tr.Tool, tr.ResolvedArgs
				e.After.State = o.State
				e.After.Audits = o.State.Own.Audits
				rebindCollectionFailedPreview(t, o, e)
			}
			if verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e) == nil {
				t.Fatal("unrecognized raw/schema evidence admitted")
			}
		})
	}
}

func TestCollectionPreviewFailureClarifyStillRequiresIndependentReview(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	d.steps[0].Action = "clarify"
	got := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	if got.Disposition != "continue_recorded" || len(d.sent) != 3 || !got.Result.StructuralPass || got.Result.Success || got.Result.Status != "needs_review" || len(got.Issues) != 0 || len(got.Result.ReviewRequirements) == 0 {
		t.Fatal("business failure admission fabricated a verdict or quality error", got.Issues)
	}
	if got.Result.Observations[0].Turn.Response.Trace[0].Outcome != "failure" || len(got.PreviewFailures) != 1 || verifyCollectionPreviewFailureRecording(got) != nil {
		t.Fatal("failure diagnostics or replay lost")
	}
}

func TestCollectionPreviewFailureExactLoopExhaustionHistory(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	o := &d.steps[0]
	e := &d.evidence
	o.Turn.Message = c.Scenario.Turns[0].User
	tr := o.Turn.Response.Trace[0]
	o.Turn.Response.Trace = []Trace{tr, tr, tr, tr, tr}
	o.Turn.Response.Reply = "（工具循环次数过多，请换一种说法）"
	o.State.Own.Audits = []store.AgentAuditLog{}
	for i := 0; i < 5; i++ {
		o.State.Own.Audits = append(o.State.Own.Audits, store.AgentAuditLog{ID: uint64(4 + i), TenantID: 19, SessionID: "session-19", Tool: tr.Tool, Args: tr.ResolvedArgs, Result: tr.Result})
	}
	e.After.State = o.State
	e.After.Audits = o.State.Own.Audits
	rebindCollectionFailedPreview(t, o, e)
	var session struct {
		History []agent.Message `json:"history"`
		Pending any             `json:"pending_action"`
	}
	json.Unmarshal(e.AfterSession, &session)
	session.History = session.History[:len(session.History)-1]
	e.AfterSession = mustCollectionJSON(t, session)
	if verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e) != nil {
		t.Fatal("exact source loop fallback rejected")
	}
	o.Turn.Response.Reply = "invented reply"
	o.Turn.RawBody = string(mustCollectionJSON(t, o.Turn.Response))
	if verifyCollectionPreviewFailureEvidence(c, d.f, e.Before.State, *o, *e) == nil {
		t.Fatal("unstored arbitrary reply admitted")
	}
}
