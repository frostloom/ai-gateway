package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const windowV8BaselineSHA = "2a78a0188ffd5072f75aeb51c9d9d54c5fdd46edc18a521571aa8aaccf38e1df"

// These are candidate liability declarations, never settled bills or authority.
type windowV8Amounts struct {
	Model int64 `json:"model_micro_cny"`
	JEV   int64 `json:"jev_micro_usd"`
}
type windowV8BudgetDeclaration struct {
	Schema            string          `json:"schema"`
	HumanAuthorized   bool            `json:"human_authorized"`
	BaselineSHA256    string          `json:"baseline_sha256"`
	BaseCapsSHA256    string          `json:"base_caps_sha256"`
	RetryPolicySHA256 string          `json:"retry_policy_sha256"`
	Parent            windowV8Amounts `json:"parent"`
	Historical        windowV8Amounts `json:"historical"`
	Risk              windowV8Amounts `json:"risk"`
	Base              windowV8Amounts `json:"base"`
	MaximumExtras     windowV8Amounts `json:"maximum_extras"`
	MaximumReserved   windowV8Amounts `json:"maximum_reserved"`
	Remaining         windowV8Amounts `json:"remaining"`
}
type windowV8RetryDeclaration struct {
	Schema                    string          `json:"schema"`
	Condition                 string          `json:"condition"`
	MaxRetriesPerLogical      int             `json:"max_retries_per_logical"`
	MaxModelExtras            int             `json:"max_model_extras"`
	MaxJEVExtras              int             `json:"max_jev_extras"`
	ModelExtra                windowV8Amounts `json:"model_extra"`
	JEVExtra                  windowV8Amounts `json:"jev_extra"`
	MaxLogicalModel           int             `json:"max_logical_model"`
	MaxLogicalJEV             int             `json:"max_logical_jev"`
	MaxNativeModel            int             `json:"max_native_model"`
	MaxNativeJEV              int             `json:"max_native_jev"`
	PerWindowModelNativeExtra int             `json:"per_window_model_native_extra"`
	PerWindowJEVNativeExtra   int             `json:"per_window_jev_native_extra"`
	ModelDeadlineMS           int             `json:"model_deadline_ms"`
	JEVDeadlineMS             int             `json:"jev_deadline_ms"`
	Model                     string          `json:"model"`
	JEV                       string          `json:"jev"`
	KnowledgeModel            string          `json:"knowledge_model"`
	KnowledgeJEV              string          `json:"knowledge_jev"`
}

// Canonical typed values identify the proposed policy, not a future human reply.
func expectedWindowV8RetryDeclaration() windowV8RetryDeclaration {
	return windowV8RetryDeclaration{
		Schema:               "full-window-retry-declaration.v1",
		Condition:            "native1_nil_response_exactly_one_new_connection_tls_handshake_eof_no_gotconn_no_request_write_no_first_byte_no_success_or_contradictory_or_missing_or_late_trace_original_request_and_parent_live",
		MaxRetriesPerLogical: 1, MaxModelExtras: 20, MaxJEVExtras: 20,
		ModelExtra: windowV8Amounts{74752, 0}, JEVExtra: windowV8Amounts{0, 2688},
		MaxLogicalModel: 58860, MaxLogicalJEV: 5886, MaxNativeModel: 58880, MaxNativeJEV: 5906,
		PerWindowModelNativeExtra: 20, PerWindowJEVNativeExtra: 20,
		ModelDeadlineMS: 90000, JEVDeadlineMS: 10000,
		Model: "deepseek-flash", JEV: "jev-1.13.0", KnowledgeModel: "off", KnowledgeJEV: "off",
	}
}
func windowV8DeclarationSHA(v any) string {
	// All callers provide the fixed scalar-only declaration types.
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return conversationHash(raw)
}
func expectedWindowV8BudgetDeclaration() windowV8BudgetDeclaration {
	return windowV8BudgetDeclaration{
		Schema: "full-window-budget-declaration.v1", HumanAuthorized: false,
		BaselineSHA256: windowV8BaselineSHA, BaseCapsSHA256: windowV7CapsSHA,
		RetryPolicySHA256: windowV8DeclarationSHA(expectedWindowV8RetryDeclaration()),
		Parent:            windowV8Amounts{140000000, 1100000}, Historical: windowV8Amounts{51200203, 399529},
		Risk: windowV8Amounts{2023424, 65}, Base: windowV8Amounts{84500000, 600000},
		MaximumExtras: windowV8Amounts{1495040, 53760}, MaximumReserved: windowV8Amounts{139218667, 1053354},
		Remaining: windowV8Amounts{781333, 46646},
	}
}
func prepareFullWindowScopeV8(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV8BaselineSHA {
		return fullWindowScope{}, fmt.Errorf("v8 fixed historical descriptor required")
	}
	if width != 28 || len(caps) != 131 {
		return fullWindowScope{}, fmt.Errorf("v8 original width and cap count required")
	}
	raw, err := json.Marshal(caps)
	if err != nil || conversationHash(raw) != windowV7CapsSHA {
		return fullWindowScope{}, fmt.Errorf("v8 original base cap vector required")
	}
	out, err := prepareFullWindowScopeWithBounds(s, p, width, caps, b, 84500000, 600000)
	if err != nil {
		return fullWindowScope{}, err
	}
	budget, retry := expectedWindowV8BudgetDeclaration(), expectedWindowV8RetryDeclaration()
	out.Schema = "full-window-scope.v8"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.BudgetDeclaration = &budget
	out.RetryDeclaration = &retry
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(budget)
	out.RetryPolicySHA256 = windowV8DeclarationSHA(retry)
	return out, nil
}
func validateWindowV8Declarations(scope fullWindowScope) error {
	budget, retry := expectedWindowV8BudgetDeclaration(), expectedWindowV8RetryDeclaration()
	if scope.BudgetApprovalSHA256 != "" || !reflect.DeepEqual(scope.BudgetDeclaration, &budget) || !reflect.DeepEqual(scope.RetryDeclaration, &retry) || scope.BudgetDeclarationSHA256 != windowV8DeclarationSHA(budget) || scope.RetryPolicySHA256 != windowV8DeclarationSHA(retry) {
		return fmt.Errorf("v8 fixed candidate declarations required; no human authority")
	}
	return nil
}
