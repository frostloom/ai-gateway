package taskbench

import (
	"encoding/json"
	"os"
	"testing"
)

func TestWindowV14PureExactStudyAndLiability(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, err := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], b, "3d5f19325c69165c603a070518d2a4b25faa43a1845323d494866486abe7230f")
	if err != nil {
		t.Fatal(err)
	}
	c, d := v.CompletionV14, v.BudgetDeclaration
	if len(v.Slots) != 3660 || len(v.Windows) != 456 || c == nil || c.Primary.Pairs != 3660 || c.Primary.Arms != 7320 || c.Primary.DefinedUserTurns != 11772 || c.Boundaries != 1851 || c.Prefix.Pairs != 385 || c.Execution.Pairs != 3275 || c.HistoricalClaims != 177 || c.HistoricalModelAttempts != 7237 || c.HistoricalJEVAttempts != 1869 {
		t.Fatal("original study or historical population changed")
	}
	if d.Parent != (windowV8Amounts{200000000, 3000000}) || d.Historical != (windowV8Amounts{85037976, 932373}) || d.MaximumReserved != (windowV8Amounts{198747697, 2805070}) || d.Remaining != (windowV8Amounts{1252303, 194930}) {
		t.Fatal("retained claims/risk/extras not fully covered")
	}
	if d.Historical.Model+d.Base.Model+d.Risk.Model+d.MaximumExtras.Model != d.MaximumReserved.Model || d.Historical.JEV+d.Base.JEV+d.Risk.JEV+d.MaximumExtras.JEV != d.MaximumReserved.JEV {
		t.Fatal("maximum reservation arithmetic changed")
	}
	if v.OperationPolicyV14 == nil || v.OperationPolicyV14.validate() != nil || v.AllowPaid || v.ReadyForPaidRun || v.BudgetApprovalSHA256 != "" || c.HumanReviewedExactScope || v.CompletionV13 != nil || v.CompletionV12 != nil || v.Completion != nil {
		t.Fatal("declaration mixed with another epoch or execution authority")
	}
	first, err := executionWindowAt(v, 1)
	if err != nil || first.ID != "window-056" || first.FirstOrdinal != 386 {
		t.Fatal("suffix mapping lost", err)
	}
	totals, err := executionTotals(v)
	if err != nil || totals.Pairs != 3275 || totals.Arms != 6550 || totals.DefinedUserTurns != 10504 || totals.Boundaries != 0 {
		t.Fatal("suffix totals lost", err)
	}
	if _, _, err := validateFullWindowScope(v, s, p, b); err != nil {
		t.Fatal(err)
	}
	for label, value := range map[string]any{"policy": expectedWindowV14OperationPolicy(), "budget": expectedWindowV14BudgetDeclaration(), "completion": expectedWindowCompletionDeclarationV14()} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("V14_DECL_%s SHA=%s JSON=%s", label, conversationHash(raw), raw)
	}
}

func TestWindowV14PureRejectsReboundMutation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, err := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], b, windowV14BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*fullWindowScope){
		"old156":           func(x *fullWindowScope) { x.BaselineEvidenceSHA256 = windowV13BaselineSHA },
		"history":          func(x *fullWindowScope) { x.BudgetDeclaration.Historical.Model-- },
		"old300":           func(x *fullWindowScope) { x.OperationPolicyV14.WindowSeconds = 300 },
		"larger-model":     func(x *fullWindowScope) { x.OperationPolicyV14.ModelSeconds = 91 },
		"parent":           func(x *fullWindowScope) { x.BudgetDeclaration.Parent.Model++ },
		"cap-shift":        func(x *fullWindowScope) { x.Windows[0].ModelCap++; x.Windows[1].ModelCap-- },
		"slot-order":       func(x *fullWindowScope) { x.Slots[385], x.Slots[386] = x.Slots[386], x.Slots[385] },
		"prefix":           func(x *fullWindowScope) { x.CompletionV14.Prefix.Pairs-- },
		"fake-human-exact": func(x *fullWindowScope) { x.CompletionV14.HumanReviewedExactScope = true },
		"fake-paid":        func(x *fullWindowScope) { x.AllowPaid = true },
		"old-completion":   func(x *fullWindowScope) { c := expectedWindowCompletionDeclarationV13(); x.CompletionV13 = &c },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(v)
			var changed fullWindowScope
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(&changed)
			changed.ScheduleSHA256 = windowV8DeclarationSHA(changed.Slots)
			changed.BudgetDeclarationSHA256 = windowV8DeclarationSHA(changed.BudgetDeclaration)
			if validateWindowV14ExecutionShape(changed) == nil {
				t.Fatal("rewritten candidate accepted")
			}
		})
	}
	if _, err := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], b, windowV13BaselineSHA); err == nil {
		t.Fatal("old baseline accepted")
	}
}

func TestWindowV14FieldsCannotChangeLegacyV13(t *testing.T) {
	const path = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full3660-completion-v13-freeze-20261105-r4/actual-manifest.json"
	raw, err := os.ReadFile(path)
	if err != nil || conversationHash(raw) != "22861d4d687545387d3dfd6abe7c93830ad87062daa875f3f6082bf85b0cdad9" {
		t.Fatal("original public manifest changed", err)
	}
	var original struct {
		Scope fullWindowScope `json:"scope"`
	}
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(original.Scope)
	if conversationHash(wire) != "61cfb99e011a00d6b9a1113c39cbe73e7ca48e69998f116229be8f076504b1f1" {
		t.Fatal("legacy wire bytes changed")
	}
	policy := expectedWindowV14OperationPolicy()
	original.Scope.OperationPolicyV14 = &policy
	if validateWindowV13Declarations(original.Scope) == nil {
		t.Fatal("new timing field smuggled into legacy declarations")
	}
}
