package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
)

const collectionPreviewFailurePolicyID = "full-customer-recording-preview-failure.v2"

// Captures are untrusted replay data, never execution authority. The future
// owned caller must obtain them while quiesced under its existing permit.
type collectionPreviewFailureOrders struct {
	Columns []string              `json:"columns"`
	Rows    []store.RechargeOrder `json:"rows"`
}
type collectionPreviewFailureEvidence struct {
	Orders        *collectionPreviewFailureOrders `json:"orders,omitempty"`
	Turn          int                             `json:"turn"`
	BeforeSession json.RawMessage                 `json:"before_session"`
	AfterSession  json.RawMessage                 `json:"after_session"`
	Before        qualityEffectCapture            `json:"before"`
	After         qualityEffectCapture            `json:"after"`
}
type collectionPreviewFailureRecording struct {
	collectionRecording
	PreviewFailures []collectionPreviewFailureEvidence `json:"preview_failures"`
}
type collectionPreviewFailureProvider interface {
	collectionPreviewFailureEvidence(context.Context, int) (collectionPreviewFailureEvidence, error)
}
type collectionPreviewFailureRecorder struct {
	provider collectionPreviewFailureProvider
	evidence []collectionPreviewFailureEvidence
}

func collectionPreviewFailurePolicySHA() string {
	return qualityJSONSHA([]string{collectionPreviewFailurePolicyID, collectionPolicySHA(), qualityPreviewPolicySHA(), "own-allowed-write;unconfirmed-preview-failure;strict-raw-response-trace-args;derived-business-rejection;no-pending-before-after;exact-history-handler-one-read-reuse-exact-five-loop-fallback;whole-sql-session-audit-state;retain-origin-quality-negatives;admission-not-a-correctness-verdict;full-original-turns;same-live-replay;full-private-recharge-orders-projected-to-unchanged-sql;no-authority"})
}
func recordCollectionPreviewFailureConversation(ctx context.Context, c ConversationCase, d ConversationDriver) collectionPreviewFailureRecording {
	provider, _ := d.(collectionPreviewFailureProvider)
	capture := &collectionPreviewFailureRecorder{provider: provider, evidence: []collectionPreviewFailureEvidence{}}
	p := &collectionRecorder{issues: []collectionIssue{}, previewFailure: capture}
	r := runConversation(ctx, c, d, p)
	applyConversationFactChecks(&r, p)
	raw, err := json.Marshal(r)
	digest := ""
	if err != nil {
		p.issue("unserializable_result", len(r.Errors))
		r.Errors = append(r.Errors, "unserializable recording evidence")
		r.Success = false
		r.Status = "failed"
	} else {
		digest = conversationHash(raw)
	}
	disposition := "continue_recorded"
	if p.halted {
		disposition = "halt"
	}
	return collectionPreviewFailureRecording{collectionRecording: collectionRecording{PolicyID: collectionPreviewFailurePolicyID, PolicySHA256: collectionPreviewFailurePolicySHA(), ResultSHA256: digest, Disposition: disposition, Issues: p.issues, Result: r}, PreviewFailures: capture.evidence}
}
func (p *collectionPreviewFailureRecorder) admit(ctx context.Context, c ConversationCase, f Fixture, turn int, previous ConversationState, bound *Pending, obs ConversationObservation) bool {
	if p == nil || p.provider == nil || ctx.Err() != nil || bound != nil {
		return false
	}
	in, err := p.provider.collectionPreviewFailureEvidence(ctx, turn)
	if err != nil || in.Turn != turn || verifyCollectionPreviewFailureEvidence(c, f, previous, obs, in) != nil || ctx.Err() != nil {
		return false
	}
	// Detach from a driver's mutable capture buffers before the next Step.
	raw, err := json.Marshal(in)
	if err != nil {
		return false
	}
	var copy collectionPreviewFailureEvidence
	if json.Unmarshal(raw, &copy) != nil || !qualityJSONEqual(copy, in) {
		return false
	}
	p.evidence = append(p.evidence, copy)
	return true
}
func verifyCollectionPreviewFailureEvidence(c ConversationCase, f Fixture, previous ConversationState, obs ConversationObservation, in collectionPreviewFailureEvidence) error {
	fail := func() error { return fmt.Errorf("failed preview evidence unavailable or differs") }
	if in.Orders != nil && !collectionPreviewFailureOrdersBound(in, f) {
		return fail()
	}
	if in.Turn < 1 || in.Turn > len(c.Scenario.Turns) || !reflect.DeepEqual(previous, in.Before.State) || !reflect.DeepEqual(obs.State, in.After.State) || validateConversationState(previous, f) != nil || validateConversationState(obs.State, f) != nil || !qualitySameEffectState(previous, obs.State, f) || !reflect.DeepEqual(previous.Controls, obs.State.Controls) {
		return fail()
	}
	if qualityNoTableEffects(in.Before.Tables, in.After.Tables) != nil || obs.Pending != nil || obs.Turn.Pending != nil || obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" || obs.Turn.Response.Pending || obs.Turn.Response.PendingTool != "" || obs.Turn.Response.Preview != "" || obs.Turn.Response.SessionID == "" {
		return fail()
	}
	tm := obs.Turn.Response.Telemetry
	if tm == nil || tm.Runtime == nil || tm.LangfuseTraceID == "" || tm.LLMCalls < 0 || tm.JevCalls < 0 || tm.LLMUsageMissing != 0 || tm.JevUsageMissing != 0 || tm.JevErrors != 0 || tm.LLMUsageResponses != tm.LLMCalls || tm.JevUsageResponses != tm.JevCalls {
		return fail()
	}
	var response Response
	if _, err := windowJSONObject([]byte(obs.Turn.RawBody)); err != nil {
		return fail()
	}
	responseDecoder := json.NewDecoder(bytes.NewReader([]byte(obs.Turn.RawBody)))
	responseDecoder.DisallowUnknownFields()
	if responseDecoder.Decode(&response) != nil || !reflect.DeepEqual(response, obs.Turn.Response) {
		return fail()
	}
	if _, err := windowJSONObject(append(append([]byte(`{"trace":`), obs.Turn.TraceRaw...), '}')); err != nil {
		return fail()
	}
	var traces []Trace
	traceDecoder := json.NewDecoder(bytes.NewReader(obs.Turn.TraceRaw))
	traceDecoder.DisallowUnknownFields()
	if traceDecoder.Decode(&traces) != nil || !reflect.DeepEqual(traces, response.Trace) {
		return fail()
	}
	failures := 0
	auditTraces := append([]Trace(nil), traces...)
	rule := c.Scenario.Turns[in.Turn-1]
	if rule.BoundPendingRequired {
		return fail()
	}
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
		for _, a := range []map[string]any{args, resolved} {
			if tid, ok := a["tenant_id"]; ok {
				n, e := windowNonnegativeInteger(tid)
				if e != nil || uint64(n) != f.TenantID {
					return fail()
				}
			}
		}
		if tr.Phase == "read" && tr.Outcome == "success" && !isConversationWrite(tr.Tool) && collectionWrongToolCode(tr) == "wrong_read_tool" {
			if !collectionPreviewReadArguments(tr.Tool, args, resolved) {
				return fail()
			}
			continue
		}
		if !hasString(rule.AllowedTools, tr.Tool) || !isConversationWrite(tr.Tool) || tr.Phase != "preview" || tr.Outcome != "failure" || tr.Preview != "" || !qualityJSONEqual(args, resolved) || !(collectionPreviewBusinessFailure(tr.Tool, args, previous, tr.Result) || collectionPreviewMissingOrderFailure(tr, args, in, f)) {
			return fail()
		}
		failures++
		// Existing v1 preview audits normalize to pending_confirmation. A proven
		// failed preview instead records its exact failure result, like a read audit.
		// This projection changes no retained trace or old validator behavior.
		auditTraces[i].Phase = "failed_preview_audit"
	}
	if failures < 1 || len(traces) > 5 {
		return fail()
	}
	if !qualityAuditProjection(in.Before.State, in.Before.Audits, f.TenantID) || !qualityAuditProjection(in.After.State, in.After.Audits, f.TenantID) || qualityAuditDelta(in.Before.Audits, in.After.Audits, auditTraces, f.TenantID, response.SessionID) != nil {
		return fail()
	}
	// Adapt only the session ID to the existing bookkeeping validator. This
	// private adapter value is not an assertion that a pending action exists.
	sessionInput := qualityPreviewInput{Record: collectionRecording{Result: ConversationResult{Fixture: f}}, Turn: in.Turn, BeforeSession: in.BeforeSession, Pending: &qualityPendingCapture{SessionID: response.SessionID}, Before: in.Before, After: in.After}
	if qualitySessionEffects(sessionInput) != nil || collectionPreviewFailureHistory(obs, in) != nil {
		return fail()
	}
	return nil
}

// The session can change only by appending this exact user/trace/result/reply
// exchange. Historical history is immutable; no pending is admitted on either side.
func collectionPreviewFailureHistory(obs ConversationObservation, in collectionPreviewFailureEvidence) error {
	fail := func() error { return fmt.Errorf("failed preview session history differs") }
	decode := func(raw []byte, absent bool) ([]agent.Message, error) {
		object, err := windowJSONObject(raw)
		if err != nil || windowOnlyKeys(object, "history", "pending_action") != nil || object["pending_action"] != nil {
			return nil, fail()
		}
		if absent && len(object) == 0 {
			return []agent.Message{}, nil
		}
		if _, ok := object["history"].([]any); !ok {
			return nil, fail()
		}
		packed, _ := json.Marshal(object["history"])
		var history []agent.Message
		decoder := json.NewDecoder(bytes.NewReader(packed))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&history) != nil {
			return nil, fail()
		}
		return history, nil
	}
	before, err := decode(in.BeforeSession, in.Turn == 1)
	if err != nil {
		return err
	}
	after, err := decode(in.AfterSession, false)
	if err != nil {
		return err
	}
	if len(after) < len(before)+2 || !reflect.DeepEqual(before, after[:len(before)]) {
		return fail()
	}
	added := after[len(before):]
	if !reflect.DeepEqual(added[0], agent.Message{Role: "user", Content: obs.Turn.Message}) {
		return fail()
	}
	end := len(added)
	if reflect.DeepEqual(added[end-1], agent.Message{Role: "assistant", Content: obs.Turn.Response.Reply}) {
		end--
	} else if obs.Turn.Response.Reply != "（工具循环次数过多，请换一种说法）" || end != 11 {
		// The source's exact five-iteration fallback is returned without
		// appending another assistant message to session history.
		return fail()
	}
	if (end-1)%2 != 0 || (end-1)/2 > 5 {
		return fail()
	}
	ids := map[string]bool{}
	reads := map[string]Trace{}
	reuses := map[string]bool{}
	next := 0
	for i := 1; i < end; i += 2 {
		call, result := added[i], added[i+1]
		if call.Role != "assistant" || call.ToolCallID != "" || len(call.ToolCalls) != 1 {
			return fail()
		}
		tc := call.ToolCalls[0]
		if tc.ID == "" || ids[tc.ID] || tc.Type != "function" || !reflect.DeepEqual(result, agent.Message{Role: "tool", ToolCallID: tc.ID, Content: result.Content}) {
			return fail()
		}
		ids[tc.ID] = true
		if next < len(obs.Turn.Response.Trace) {
			tr := obs.Turn.Response.Trace[next]
			if tc.Function.Name == tr.Tool && tc.Function.Arguments == tr.Args && result.Content == tr.Result {
				next++
				if tr.Phase == "read" {
					args, err := windowJSONObject([]byte(tr.ResolvedArgs))
					if err != nil {
						return fail()
					}
					reads[tr.Tool+":"+qualityJSONSHA(args)] = tr
				}
				continue
			}
		}
		args, err := windowJSONObject([]byte(tc.Function.Arguments))
		if err != nil {
			return fail()
		}
		key := tc.Function.Name + ":" + qualityJSONSHA(args)
		tr, ok := reads[key]
		if !ok || reuses[key] || result.Content != tr.Result {
			return fail()
		}
		reuses[key] = true
	}
	if next != len(obs.Turn.Response.Trace) {
		return fail()
	}
	return nil
}

type collectionPreviewFailureReplay struct {
	*replayConversationDriver
	evidence []collectionPreviewFailureEvidence
}

func (d *collectionPreviewFailureReplay) collectionPreviewFailureEvidence(_ context.Context, turn int) (collectionPreviewFailureEvidence, error) {
	for _, e := range d.evidence {
		if e.Turn == turn {
			return e, nil
		}
	}
	return collectionPreviewFailureEvidence{}, fmt.Errorf("missing failed preview capture")
}
func verifyCollectionPreviewFailureRecording(record collectionPreviewFailureRecording) error {
	raw, err := json.Marshal(record.Result)
	if err != nil || record.PolicyID != collectionPreviewFailurePolicyID || record.PolicySHA256 != collectionPreviewFailurePolicySHA() || conversationHash(raw) != record.ResultSHA256 {
		return fmt.Errorf("failed preview policy/result binding differs")
	}
	original := record.Result
	d := &collectionPreviewFailureReplay{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: original.Case.Key(), Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}, evidence: record.PreviewFailures}
	replay := recordCollectionPreviewFailureConversation(context.Background(), original.Case, d)
	if d.next != len(original.Observations) || record.Disposition != replay.Disposition || !reflect.DeepEqual(record.Issues, replay.Issues) || !qualityJSONEqual(record.PreviewFailures, replay.PreviewFailures) {
		return fmt.Errorf("failed preview disposition/capture differs")
	}
	want, err := collectionNormalizedResult(original)
	if err != nil {
		return err
	}
	got, err := collectionNormalizedResult(replay.Result)
	if err != nil {
		return err
	}
	if !bytes.Equal(want, got) {
		return fmt.Errorf("failed preview assessment differs from replay")
	}
	return nil
}
