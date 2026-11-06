package taskbench

import (
	"encoding/json"
	"testing"
)

func TestWindowV15PureExactStudyAndLiability(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, err := prepareFullWindowScopeV15(s, p, windowV9TestCaps(t)[55:], b, "fec84f82af6951488d0886b42855f63ea2614d951f4c9f429836638933ea795c")
	if err != nil {
		t.Fatal(err)
	}
	c, d := v.CompletionV15, v.BudgetDeclaration
	if len(v.Slots) != 3660 || len(v.Windows) != 456 || c == nil || c.Primary.Pairs != 3660 || c.Primary.Arms != 7320 || c.Primary.DefinedUserTurns != 11772 || c.Boundaries != 1851 || c.Prefix.Pairs != 385 || c.Execution.Pairs != 3275 || c.HistoricalClaims != 178 || c.HistoricalModelAttempts != 7245 || c.HistoricalJEVAttempts != 1871 {
		t.Fatal("original study or historical population changed")
	}
	if d.Parent != (windowV8Amounts{200000000, 3000000}) || d.Historical != (windowV8Amounts{85219758, 936184}) || d.MaximumReserved != (windowV8Amounts{198929479, 2808881}) || d.Remaining != (windowV8Amounts{1070521, 191119}) {
		t.Fatal("retained claims/risk/extras not fully covered")
	}
	if d.Historical.Model+d.Base.Model+d.Risk.Model+d.MaximumExtras.Model != d.MaximumReserved.Model || d.Historical.JEV+d.Base.JEV+d.Risk.JEV+d.MaximumExtras.JEV != d.MaximumReserved.JEV {
		t.Fatal("maximum reservation arithmetic changed")
	}
	if v.OperationPolicyV14 == nil || v.OperationPolicyV14.validate() != nil || v.AllowPaid || v.ReadyForPaidRun || v.BudgetApprovalSHA256 != "" || c.HumanReviewedExactScope || v.CompletionV14 != nil || v.CompletionV13 != nil || v.CompletionV12 != nil || v.Completion != nil {
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
	policyRaw, _ := json.Marshal(v.OperationPolicyV14)
	if string(policyRaw) != `{"schema":"full-window-operation-policy.v14","window_seconds":600,"model_seconds":90,"jev_seconds":10}` {
		t.Fatal("approved policy bytes changed", string(policyRaw))
	}
	for label, value := range map[string]any{"policy": expectedWindowV14OperationPolicy(), "budget": expectedWindowV15BudgetDeclaration(), "completion": expectedWindowCompletionDeclarationV15()} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("V15_DECL_%s SHA=%s JSON=%s", label, conversationHash(raw), raw)
	}
}

func TestWindowV15PureRejectsReboundMutation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, err := prepareFullWindowScopeV15(s, p, windowV9TestCaps(t)[55:], b, windowV15BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*fullWindowScope){
		"old177":           func(x *fullWindowScope) { x.BaselineEvidenceSHA256 = windowV14BaselineSHA },
		"history":          func(x *fullWindowScope) { x.BudgetDeclaration.Historical.Model-- },
		"old300":           func(x *fullWindowScope) { x.OperationPolicyV14.WindowSeconds = 300 },
		"larger-model":     func(x *fullWindowScope) { x.OperationPolicyV14.ModelSeconds = 91 },
		"parent":           func(x *fullWindowScope) { x.BudgetDeclaration.Parent.Model++ },
		"cap-shift":        func(x *fullWindowScope) { x.Windows[0].ModelCap++; x.Windows[1].ModelCap-- },
		"slot-order":       func(x *fullWindowScope) { x.Slots[385], x.Slots[386] = x.Slots[386], x.Slots[385] },
		"prefix":           func(x *fullWindowScope) { x.CompletionV15.Prefix.Pairs-- },
		"fake-human-exact": func(x *fullWindowScope) { x.CompletionV15.HumanReviewedExactScope = true },
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
			if validateWindowV15ExecutionShape(changed) == nil {
				t.Fatal("rewritten candidate accepted")
			}
		})
	}
	if _, err := prepareFullWindowScopeV15(s, p, windowV9TestCaps(t)[55:], b, windowV13BaselineSHA); err == nil {
		t.Fatal("old baseline accepted")
	}
}
