package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/frostloom/ai-gateway/internal/agent"
)

const collectionPreviewRecoveryPolicyID = "full-customer-recording-preview-recovery.v1"

func collectionPreviewRecoveryPolicySHA() string {
	return qualityJSONSHA([]string{collectionPreviewRecoveryPolicyID, collectionPolicySHA(), collectionPreviewFailurePolicySHA(), "owned-quiescent-capture;retain-original-v2-families;refund-empty-own-basis-exact-source-rejection;all-original-raw-trace-session-telemetry-state-sql-audits;no-prior-pending;one-terminal-source-correct-pending-or-no-pending;source-five-model-loop-one-repair;no-execution-before-original-confirmation;live-replay-parity;admission-not-semantic-success;no-authority"})
}

// Recovery evidence is untrusted recorded data, never acquisition authority.
// The historical failed-preview evidence remains unchanged and rejects pending.
type collectionPreviewRecoveryEvidence struct {
	Orders        *collectionPreviewFailureOrders `json:"orders,omitempty"`
	Turn          int                             `json:"turn"`
	BeforeSession json.RawMessage                 `json:"before_session"`
	AfterSession  json.RawMessage                 `json:"after_session"`
	Before        qualityEffectCapture            `json:"before"`
	After         qualityEffectCapture            `json:"after"`
	Pending       *qualityPendingCapture          `json:"pending,omitempty"`
}

type collectionPreviewRecoveryRecording struct {
	collectionRecording
	PreviewRecoveries []collectionPreviewRecoveryEvidence `json:"preview_recoveries"`
}

type collectionPreviewRecoveryProvider interface {
	collectionPreviewRecoveryEvidence(context.Context, int) (collectionPreviewRecoveryEvidence, error)
}

type collectionPreviewRecoveryRecorder struct {
	provider collectionPreviewRecoveryProvider
	evidence []collectionPreviewRecoveryEvidence
}

func recordCollectionPreviewRecoveryConversation(ctx context.Context, c ConversationCase, d ConversationDriver) collectionPreviewRecoveryRecording {
	provider, _ := d.(collectionPreviewRecoveryProvider)
	capture := &collectionPreviewRecoveryRecorder{provider: provider, evidence: []collectionPreviewRecoveryEvidence{}}
	p := &collectionRecorder{issues: []collectionIssue{}, previewRecovery: capture}
	r := runConversation(ctx, c, d, p)
	applyConversationFactChecks(&r, p)
	raw, err := json.Marshal(r)
	digest := ""
	if err != nil {
		p.issue("unserializable_result", len(r.Errors))
		r.Errors = append(r.Errors, "unserializable recording evidence")
		r.Success, r.Status = false, "failed"
	} else {
		digest = conversationHash(raw)
	}
	disposition := "continue_recorded"
	if p.halted {
		disposition = "halt"
	}
	return collectionPreviewRecoveryRecording{collectionRecording: collectionRecording{PolicyID: collectionPreviewRecoveryPolicyID, PolicySHA256: collectionPreviewRecoveryPolicySHA(), ResultSHA256: digest, Disposition: disposition, Issues: p.issues, Result: r}, PreviewRecoveries: capture.evidence}
}

func (p *collectionPreviewRecoveryRecorder) admit(ctx context.Context, c ConversationCase, f Fixture, turn int, previous ConversationState, bound *Pending, obs ConversationObservation) bool {
	if p == nil || p.provider == nil || ctx == nil || ctx.Err() != nil || bound != nil {
		return false
	}
	in, err := p.provider.collectionPreviewRecoveryEvidence(ctx, turn)
	if err != nil || in.Turn != turn || verifyCollectionPreviewRecoveryEvidence(c, f, previous, obs, in) != nil || ctx.Err() != nil {
		return false
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return false
	}
	var detached collectionPreviewRecoveryEvidence
	if json.Unmarshal(raw, &detached) != nil || !qualityJSONEqual(detached, in) {
		return false
	}
	p.evidence = append(p.evidence, detached)
	return true
}

type collectionPreviewRecoveryReplay struct {
	*replayConversationDriver
	evidence []collectionPreviewRecoveryEvidence
}

func (d *collectionPreviewRecoveryReplay) collectionPreviewRecoveryEvidence(_ context.Context, turn int) (collectionPreviewRecoveryEvidence, error) {
	for _, in := range d.evidence {
		if in.Turn == turn {
			return in, nil
		}
	}
	return collectionPreviewRecoveryEvidence{}, fmt.Errorf("missing recovery capture")
}

func verifyCollectionPreviewRecoveryRecording(record collectionPreviewRecoveryRecording) error {
	raw, err := json.Marshal(record.Result)
	if err != nil || record.PolicyID != collectionPreviewRecoveryPolicyID || record.PolicySHA256 != collectionPreviewRecoveryPolicySHA() || conversationHash(raw) != record.ResultSHA256 {
		return fmt.Errorf("recovery policy/result binding differs")
	}
	original := record.Result
	d := &collectionPreviewRecoveryReplay{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: original.Case.Key(), Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}, evidence: record.PreviewRecoveries}
	replay := recordCollectionPreviewRecoveryConversation(context.Background(), original.Case, d)
	if d.next != len(original.Observations) || record.Disposition != replay.Disposition || !reflect.DeepEqual(record.Issues, replay.Issues) || !qualityJSONEqual(record.PreviewRecoveries, replay.PreviewRecoveries) {
		return fmt.Errorf("recovery disposition/capture differs")
	}
	want, err := collectionNormalizedResult(original)
	if err != nil {
		return err
	}
	got, err := collectionNormalizedResult(replay.Result)
	if err != nil || !bytes.Equal(want, got) {
		return fmt.Errorf("recovery assessment differs from replay")
	}
	return nil
}

func verifyCollectionPreviewRecoveryEvidence(c ConversationCase, f Fixture, previous ConversationState, obs ConversationObservation, in collectionPreviewRecoveryEvidence) error {
	old := collectionPreviewFailureEvidence{
		Orders: in.Orders, Turn: in.Turn, BeforeSession: in.BeforeSession, AfterSession: in.AfterSession,
		Before: in.Before, After: in.After,
	}
	if in.Pending == nil && verifyCollectionPreviewFailureEvidence(c, f, previous, obs, old) == nil {
		session, err := windowJSONObject(in.AfterSession)
		if err == nil && collectionPreviewRecoverySourceCalls(obs, in.BeforeSession, session) {
			return nil
		}
		return fmt.Errorf("preview recovery source iteration count differs")
	}
	fail := func() error { return fmt.Errorf("preview recovery evidence unavailable or differs") }
	if in.Turn < 1 || in.Turn > len(c.Scenario.Turns) || !reflect.DeepEqual(previous, in.Before.State) || !reflect.DeepEqual(obs.State, in.After.State) || validateConversationState(previous, f) != nil || validateConversationState(obs.State, f) != nil || !qualitySameEffectState(previous, obs.State, f) || !reflect.DeepEqual(previous.Controls, obs.State.Controls) {
		return fail()
	}
	message, err := resolveConversationMessage(c.Scenario.Turns[in.Turn-1].User, f)
	if err != nil || obs.Turn.Message != message || obs.Turn.Stage != fmt.Sprintf("turn-%d", in.Turn) || !reflect.DeepEqual(obs.Turn.Snapshot, obs.State.Own) {
		return fail()
	}
	if in.Orders != nil && !collectionPreviewFailureOrdersBound(old, f) {
		return fail()
	}
	if qualityNoTableEffects(in.Before.Tables, in.After.Tables) != nil || obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" {
		return fail()
	}
	tm := obs.Turn.Response.Telemetry
	traces := obs.Turn.Response.Trace
	minimumTraces := 1
	if in.Pending != nil {
		minimumTraces = 2
	}
	if tm == nil || tm.Runtime == nil || tm.LangfuseTraceID == "" || tm.LLMCalls < len(traces)+tm.ReplyRepairs || tm.LLMCalls > 5 || tm.JevCalls < 0 || tm.LLMUsageMissing != 0 || tm.JevUsageMissing != 0 || tm.JevErrors != 0 || tm.LLMUsageResponses != tm.LLMCalls || tm.JevUsageResponses != tm.JevCalls || tm.ReplyRepairs < 0 || tm.ReplyRepairs > 1 || len(traces) < minimumTraces || len(traces) > 5 {
		return fail()
	}
	if _, err := windowJSONObject([]byte(obs.Turn.RawBody)); err != nil {
		return fail()
	}
	var response Response
	decoder := json.NewDecoder(bytes.NewReader([]byte(obs.Turn.RawBody)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || !reflect.DeepEqual(response, obs.Turn.Response) {
		return fail()
	}
	if _, err := windowJSONObject(obs.Turn.TelemetryRaw); err != nil {
		return fail()
	}
	var rawTelemetry Telemetry
	decoder = json.NewDecoder(bytes.NewReader(obs.Turn.TelemetryRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&rawTelemetry) != nil || !reflect.DeepEqual(rawTelemetry, *tm) {
		return fail()
	}
	if _, err := windowJSONObject(append(append([]byte(`{"trace":`), obs.Turn.TraceRaw...), '}')); err != nil {
		return fail()
	}
	var rawTrace []Trace
	decoder = json.NewDecoder(bytes.NewReader(obs.Turn.TraceRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&rawTrace) != nil || !reflect.DeepEqual(rawTrace, traces) {
		return fail()
	}
	p := in.Pending
	session, err := windowJSONObject(in.AfterSession)
	if err != nil || windowOnlyKeys(session, "history", "pending_action") != nil {
		return fail()
	}
	rule := c.Scenario.Turns[in.Turn-1]
	if rule.BoundPendingRequired {
		return fail()
	}
	if p != nil {
		if p.SessionID == "" || p.SessionID != response.SessionID || p.IdemKey == "" || p.Preview == "" || p.Pending.TenantID != f.TenantID || !response.Pending || response.PendingTool != p.Pending.Tool || response.Preview != p.Preview || obs.Pending == nil || !qualityJSONEqual(obs.Pending, p.Pending) || obs.Turn.Pending == nil || !qualityJSONEqual(obs.Turn.Pending, p.Pending) || !bytes.Equal(p.Raw, in.AfterSession) {
			return fail()
		}
		rawPending, ok := session["pending_action"].(map[string]any)
		if !ok || windowOnlyKeys(rawPending, "tool", "args", "preview", "idem_key", "tenant_id") != nil || !qualityJSONEqual(rawPending, map[string]any{"tool": p.Pending.Tool, "args": p.Pending.Args, "preview": p.Preview, "idem_key": p.IdemKey, "tenant_id": p.Pending.TenantID}) {
			return fail()
		}
		if !conversationPendingCorrect(c, f, &p.Pending) || (!rule.PendingRequired && !rule.PendingOptional) || !hasString(rule.AllowedTools, p.Pending.Tool) || rule.PendingCleared || rule.NoSecondExecution || rule.NoExecutionWithoutNewPreview {
			return fail()
		}
	} else if response.SessionID == "" || obs.Pending != nil || obs.Turn.Pending != nil || response.Pending || response.PendingTool != "" || response.Preview != "" || session["pending_action"] != nil {
		return fail()
	}
	failures := 0
	auditTraces := append([]Trace(nil), traces...)
	for i, tr := range traces {
		if tr.Confirmed {
			return fail()
		}
		args, err := windowJSONObject([]byte(tr.Args))
		if err != nil {
			return fail()
		}
		resolved, err := windowJSONObject([]byte(tr.ResolvedArgs))
		if err != nil {
			return fail()
		}
		for _, values := range []map[string]any{args, resolved} {
			if tenant, exists := values["tenant_id"]; exists {
				n, err := windowNonnegativeInteger(tenant)
				if err != nil || uint64(n) != f.TenantID {
					return fail()
				}
			}
		}
		if p != nil && i == len(traces)-1 {
			if !isConversationWrite(tr.Tool) || tr.Tool != p.Pending.Tool || tr.Phase != "preview" || tr.Outcome != "pending" || tr.Preview != p.Preview || tr.Result != "等待确认" || !qualityJSONEqual(resolved, p.Pending.Args) {
				return fail()
			}
			plans := previous.AnswerAuthority
			if (tr.Tool == "subscribe_plan" || tr.Tool == "change_plan") && (plans == nil || !plans.CatalogComplete || plans.Plans == nil) {
				return fail()
			}
			if plans == nil {
				if qualityPreviewTarget(tr.Tool, args, resolved, f, previous, nil) != nil {
					return fail()
				}
			} else if qualityPreviewTarget(tr.Tool, args, resolved, f, previous, plans.Plans) != nil {
				return fail()
			}
			preview, err := collectionPreviewRecoveryRendering(tr.Tool, resolved, f, previous)
			if err != nil || tr.Preview != preview || !strings.HasSuffix(response.Reply, agent.EvaluationSanitizeReply(preview+"\n\n请回复「确认」执行，或「取消」放弃。")) {
				return fail()
			}
			continue
		}
		if tr.Phase == "read" && tr.Outcome == "success" && !isConversationWrite(tr.Tool) && collectionWrongToolCode(tr) == "wrong_read_tool" && collectionPreviewReadArguments(tr.Tool, args, resolved) {
			continue
		}
		if !hasString(rule.AllowedTools, tr.Tool) || !isConversationWrite(tr.Tool) || tr.Phase != "preview" || tr.Outcome != "failure" || tr.Preview != "" || !qualityJSONEqual(args, resolved) || !(collectionPreviewBusinessFailure(tr.Tool, args, previous, tr.Result) || collectionPreviewMissingOrderFailure(tr, args, old, f) || collectionPreviewRecoveryMissingRefundBasis(tr, args)) {
			return fail()
		}
		failures++
		auditTraces[i].Phase = "failed_preview_audit"
	}
	if failures == 0 || !qualityAuditProjection(in.Before.State, in.Before.Audits, f.TenantID) || !qualityAuditProjection(in.After.State, in.After.Audits, f.TenantID) || qualityAuditDelta(in.Before.Audits, in.After.Audits, auditTraces, f.TenantID, response.SessionID) != nil {
		return fail()
	}
	bookkeepingPending := p
	if bookkeepingPending == nil {
		// This is only the existing session-bookkeeping adapter, not a claim
		// that the no-pending observation created an operation.
		bookkeepingPending = &qualityPendingCapture{SessionID: response.SessionID}
	}
	bookkeeping := qualityPreviewInput{Record: collectionRecording{Result: ConversationResult{Fixture: f}}, Turn: in.Turn, BeforeSession: in.BeforeSession, Pending: bookkeepingPending, Before: in.Before, After: in.After}
	if qualitySessionEffects(bookkeeping) != nil {
		return fail()
	}
	// The source stores failed/read call pairs but not the final pending call.
	// Validate that exact history through a private history-only projection;
	// all original raw response/session/pending/trace data were bound above and
	// remain untouched. This does not mint a historical v2 admission or proof.
	session["pending_action"] = nil
	historyRaw, err := json.Marshal(session)
	if err != nil {
		return fail()
	}
	historyObservation := obs
	if p != nil {
		historyObservation.Turn.Response.Trace = traces[:len(traces)-1]
	}
	historyEvidence := collectionPreviewFailureEvidence{Turn: in.Turn, BeforeSession: in.BeforeSession, AfterSession: historyRaw}
	if collectionPreviewFailureHistory(historyObservation, historyEvidence) != nil {
		return fail()
	}
	if !collectionPreviewRecoverySourceCalls(obs, in.BeforeSession, session) {
		return fail()
	}
	return nil
}

func collectionPreviewRecoveryMissingRefundBasis(tr Trace, args map[string]any) bool {
	return tr.Tool == "refund_recharge" && windowOnlyKeys(args, "tenant_id") == nil && tr.Result == "操作不可执行：请提供要退的订单号或退款金额（元）"
}

// Call pairs include the source's read-cache reuses, even though those reuses
// do not add another retained trace or audit. A stored terminal assistant reply
// consumes one more iteration: either the final pending call or a text answer.
func collectionPreviewRecoverySourceCalls(obs ConversationObservation, beforeRaw json.RawMessage, session map[string]any) bool {
	before, err := windowJSONObject(beforeRaw)
	if err != nil || obs.Turn.Response.Telemetry == nil {
		return false
	}
	beforeLength := 0
	if len(before) != 0 {
		history, ok := before["history"].([]any)
		if !ok {
			return false
		}
		beforeLength = len(history)
	}
	history, ok := session["history"].([]any)
	if !ok || len(history) <= beforeLength {
		return false
	}
	raw, err := json.Marshal(history[len(history)-1])
	if err != nil {
		return false
	}
	var terminal agent.Message
	if json.Unmarshal(raw, &terminal) != nil {
		return false
	}
	terminal.Content = agent.EvaluationSanitizeReply(terminal.Content)
	storedReply := reflect.DeepEqual(terminal, agent.Message{Role: "assistant", Content: obs.Turn.Response.Reply})
	toolMessages := len(history) - beforeLength - 1 // remove this turn's user
	if storedReply {
		toolMessages--
	}
	if toolMessages < 0 || toolMessages%2 != 0 {
		return false
	}
	tm := obs.Turn.Response.Telemetry
	calls := toolMessages/2 + tm.ReplyRepairs
	if storedReply {
		calls++
	}
	return calls > 0 && calls <= 5 && calls == tm.LLMCalls
}
