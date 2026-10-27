package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

const collectionPolicyID = "full-customer-recording-policy.v1"

// This is an assessment envelope, never permission, a claim, or task success.
// It stays private until the separately bound collection entry is implemented.
type collectionRecording struct {
	PolicyID     string             `json:"policy_id"`
	PolicySHA256 string             `json:"policy_sha256"`
	ResultSHA256 string             `json:"result_sha256"`
	Disposition  string             `json:"disposition"`
	Issues       []collectionIssue  `json:"issues"`
	Result       ConversationResult `json:"result"`
}
type collectionIssue struct {
	Code        string `json:"code"`
	Turn        int    `json:"turn"`
	ErrorIndex  int    `json:"error_index"`
	Disposition string `json:"disposition"`
}
type collectionRecorder struct {
	issues []collectionIssue
	halted bool
	turn   int
}

var collectionQualityCodes = []string{"unexpected_action", "wrong_read_tool", "missing_read", "missing_preview", "missing_execution", "terminal_goal", "semantic_negative", "fact_mismatch"}

func collectionPolicySHA() string {
	raw, _ := json.Marshal(struct {
		ID         string
		Quality    []string
		Invariants string
	}{collectionPolicyID, collectionQualityCodes, "origin-issues;unknown-halt;per-turn-effects;pending-before-confirm;final-oracle;full-result-digest;no-permission"})
	return conversationHash(raw)
}
func (p *collectionRecorder) issue(code string, index int) {
	disposition := "halt"
	if hasString(collectionQualityCodes, code) {
		disposition = "continue_recorded"
	} else {
		p.halted = true
	}
	p.issues = append(p.issues, collectionIssue{Code: code, Turn: p.turn, ErrorIndex: index, Disposition: disposition})
}
func recordCollectionConversation(ctx context.Context, c ConversationCase, d ConversationDriver) collectionRecording {
	p := &collectionRecorder{issues: []collectionIssue{}}
	r := runConversation(ctx, c, d, p)
	applyConversationFactChecks(&r, p)
	raw, err := json.Marshal(r)
	if err != nil {
		p.issue("unserializable_result", len(r.Errors))
		r.Errors = append(r.Errors, "unserializable recording evidence")
		r.Success = false
		r.Status = "failed"
	}
	disposition := "continue_recorded"
	if p.halted {
		disposition = "halt"
	}
	digest := ""
	if err == nil {
		digest = conversationHash(raw)
	}
	return collectionRecording{PolicyID: collectionPolicyID, PolicySHA256: collectionPolicySHA(), ResultSHA256: digest, Disposition: disposition, Issues: p.issues, Result: r}
}

// Terminal error keys are structured at their validation origin. Human error
// text is never used as a continuation allowlist.
type conversationTerminalError struct{ key string }

func (e conversationTerminalError) Error() string {
	return fmt.Sprintf("terminal assertion failed: %s", e.key)
}
func collectionTerminalCode(err error, unchanged bool) string {
	var terminal conversationTerminalError
	if !unchanged || !errors.As(err, &terminal) {
		return "terminal_invariant"
	}
	switch terminal.key {
	case "business_state_unchanged", "authorized_write_count", "balance_delta_cent", "refund_cent", "refund_ledger_rows_added", "requested_cent", "owned_seed_order_status", "new_paid_order_count", "subscriptions_added", "subscription_status", "current_plan", "auto_renew", "pending_plan", "cycle_end_unchanged", "current_allowance_unchanged", "effective", "cycle_days", "simulated_month_fee_cash_debit", "current_cycle_access_retained", "revalidate_order_ownership_and_paid_status", "unrelated_orders_and_subscriptions_unchanged":
		return "terminal_goal"
	default:
		return "terminal_invariant"
	}
}
func collectionWrongToolCode(tr Trace) string {
	if tr.Phase == "read" && tr.Outcome == "success" && hasString([]string{"get_balance", "get_consumption", "get_recent_bills", "list_models", "list_plans", "get_my_subscription"}, tr.Tool) {
		return "wrong_read_tool"
	}
	return "unauthorized_tool"
}

func collectionKnownAction(action string) bool {
	return hasString([]string{"unreviewed", "read", "preview", "execute", "clarify", "respond", "reject", "consult", "limited", "noop", "cancel"}, action)
}
func collectionKnownTrace(tr Trace) bool {
	if isConversationWrite(tr.Tool) {
		return (tr.Phase == "preview" && (tr.Outcome == "pending" || tr.Outcome == "success")) || (tr.Phase == "execute" && tr.Outcome == "success")
	}
	return collectionWrongToolCode(tr) == "wrong_read_tool"
}

func collectionTerminalErrors(c ConversationCase, f Fixture, initial, final ConversationState, observations []ConversationObservation) []error {
	issues := conversationTerminalErrors(c, f, initial, final, observations)
	// Ordering only; classification still uses structured error origin/key.
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Error() < issues[j].Error() })
	return issues
}
func collectionNormalizedResult(r ConversationResult) ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var copy ConversationResult
	if err = json.Unmarshal(raw, &copy); err != nil {
		return nil, err
	}
	copy.DurationMS = 0
	for i := range copy.Observations {
		copy.Observations[i].Turn.LatencyMS = 0
	}
	return json.Marshal(copy)
}
func verifyCollectionRecording(record collectionRecording) error {
	raw, err := json.Marshal(record.Result)
	if err != nil || record.PolicyID != collectionPolicyID || record.PolicySHA256 != collectionPolicySHA() || conversationHash(raw) != record.ResultSHA256 {
		return fmt.Errorf("recording policy/result binding differs")
	}
	original := record.Result
	driver := &replayConversationDriver{episode: ConversationEpisode{Key: original.Case.Key(), Fixture: original.Fixture, Initial: original.Initial, Final: original.Final, Steps: original.Observations}}
	replay := recordCollectionConversation(context.Background(), original.Case, driver)
	if driver.next != len(original.Observations) || record.Disposition != replay.Disposition || !reflect.DeepEqual(record.Issues, replay.Issues) {
		return fmt.Errorf("recording disposition/issues differ from raw evidence")
	}
	want, err := collectionNormalizedResult(original)
	if err != nil {
		return err
	}
	got, err := collectionNormalizedResult(replay.Result)
	if err != nil {
		return err
	}
	if string(want) != string(got) {
		return fmt.Errorf("recording assessment differs from replay")
	}
	return nil
}

func collectionLegacyAssessment(r ConversationResult) ([]byte, error) {
	raw, err := collectionNormalizedResult(r)
	if err != nil {
		return nil, err
	}
	var copy ConversationResult
	if err = json.Unmarshal(raw, &copy); err != nil {
		return nil, err
	}
	sort.Strings(copy.Errors)
	copy.ReviewRequirements = nil // Original pair journals intentionally redact derived requirements.
	return json.Marshal(copy)
}
