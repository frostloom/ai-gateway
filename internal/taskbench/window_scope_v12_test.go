package taskbench

import (
	"encoding/json"
	"strings"
	"testing"
)

// Changing the suffix partition, historical liability or authority flags must
// make these real preparation/shape consumers reject the candidate.
func TestWindowV12PureExactComposition(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, e := prepareFullWindowScopeV12(s, p, windowV9TestCaps(t)[55:], b, "2ff9ee20cbd11e51b5b31a63b9bf31f53625f602194b5bb0d123bff4a462df9c")
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Slots) != 3660 || len(v.Windows) != 456 || v.Windows[0].ID != "window-056" || v.Windows[455].ID != "window-511" {
		t.Fatal("incorrect full schedule/suffix mapping")
	}
	c := v.CompletionV12
	if c == nil || c.Primary.Pairs != 3660 || c.Primary.Arms != 7320 || c.Primary.DefinedUserTurns != 11772 || c.Boundaries != 1851 || c.Prefix.Pairs != 385 || c.Execution.Pairs != 3275 {
		t.Fatal("primary composition lost")
	}
	d := v.BudgetDeclaration
	if d.Historical.Model != 79620965 || d.Historical.JEV != 838363 || d.MaximumReserved.Model != 193330686 || d.MaximumReserved.JEV != 2711060 || d.Remaining.Model != 1669314 || d.Remaining.JEV != 288940 {
		t.Fatal("history/risk/extras liability mismatch")
	}
	if d.Historical.Model+d.Base.Model+d.Risk.Model+d.MaximumExtras.Model != d.MaximumReserved.Model || d.Historical.JEV+d.Base.JEV+d.Risk.JEV+d.MaximumExtras.JEV != d.MaximumReserved.JEV {
		t.Fatal("liability excludes reserved amounts")
	}
	if v.RetryDeclaration.MaxLogicalModel != 52520 || v.RetryDeclaration.MaxLogicalJEV != 5252 || v.RetryDeclaration.MaxNativeModel != 52540 || v.RetryDeclaration.MaxNativeJEV != 5272 {
		t.Fatal("suffix maxima lost")
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), `"all_attempt"`) || v.AllowPaid || v.ReadyForPaidRun || v.Completion != nil || v.Observation != nil {
		t.Fatal("old totals/authority mixed into candidate")
	}
}

func TestWindowV12PureRejectsCoherentMutation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, e := prepareFullWindowScopeV12(s, p, windowV9TestCaps(t)[55:], b, "2ff9ee20cbd11e51b5b31a63b9bf31f53625f602194b5bb0d123bff4a462df9c")
	if e != nil {
		t.Fatal(e)
	}
	mutations := map[string]func(*fullWindowScope){
		"prefix":  func(x *fullWindowScope) { x.Windows[0].FirstOrdinal = 1 },
		"gap":     func(x *fullWindowScope) { x.Windows[1].FirstOrdinal++ },
		"overlap": func(x *fullWindowScope) { x.Windows[1].FirstOrdinal-- },
		"pass":    func(x *fullWindowScope) { x.Slots[385].Pass++ },
		"arm": func(x *fullWindowScope) {
			x.Slots[385].ArmOrder[0], x.Slots[385].ArmOrder[1] = x.Slots[385].ArmOrder[1], x.Slots[385].ArmOrder[0]
		},
		"reorder":       func(x *fullWindowScope) { x.Slots[385], x.Slots[386] = x.Slots[386], x.Slots[385] },
		"cap":           func(x *fullWindowScope) { x.Windows[0].ModelCap++; x.Windows[1].ModelCap-- },
		"old104":        func(x *fullWindowScope) { x.BaselineEvidenceSHA256 = windowV11BaselineSHA },
		"oldcompletion": func(x *fullWindowScope) { c := expectedWindowCompletionDeclarationV10(); x.Completion = &c },
		"paid":          func(x *fullWindowScope) { x.AllowPaid = true },
		"approval":      func(x *fullWindowScope) { x.BudgetApprovalSHA256 = x.ScheduleSHA256 },
		"history":       func(x *fullWindowScope) { x.BudgetDeclaration.Historical.Model-- },
		"ceiling":       func(x *fullWindowScope) { x.BudgetDeclaration.Parent.Model++ },
		"prefixproof":   func(x *fullWindowScope) { x.CompletionV12.PrefixDeliverySHA256 = x.ScheduleSHA256 },
		"exclusions":    func(x *fullWindowScope) { x.CompletionV12.ExcludedSources = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(v)
			var x fullWindowScope
			if e := json.Unmarshal(raw, &x); e != nil {
				t.Fatal(e)
			}
			mutate(&x)
			x.ScheduleSHA256 = windowV8DeclarationSHA(x.Slots)
			x.BudgetDeclarationSHA256 = windowV8DeclarationSHA(x.BudgetDeclaration)
			if validateWindowV12ExecutionShape(x) == nil {
				t.Fatal("rewritten candidate accepted")
			}
		})
	}
	for _, baseline := range []string{windowV11BaselineSHA, windowV10BaselineSHA, "", strings.Repeat("f", 64)} {
		if _, e := prepareFullWindowScopeV12(s, p, windowV9TestCaps(t)[55:], b, baseline); e == nil {
			t.Fatal("foreign baseline accepted")
		}
	}
	for _, caps := range [][]fullWindowCaps{nil, windowV9TestCaps(t), windowV9TestCaps(t)[56:]} {
		if _, e := prepareFullWindowScopeV12(s, p, caps, b, windowV12BaselineSHA); e == nil {
			t.Fatal("arbitrary suffix accepted")
		}
	}
}

func TestWindowV12PureEquivalenceRejectsSelfDeclaredChanges(t *testing.T) {
	before, after := strings.Repeat("1", 64), strings.Repeat("2", 64)
	old := map[string]string{"internal/agent/llm.go": before, "go.mod": before}
	current := map[string]string{"internal/agent/llm.go": after, "go.mod": before}
	e := windowExecutorEquivalenceV12{Schema: "full-window-executor-equivalence.v12", OriginalSources: old, ChangedFiles: map[string]windowExecutorChangeV10{"internal/agent/llm.go": {Before: before, After: after}}}
	e.OriginalSourcesSHA256 = windowV8DeclarationSHA(old)
	e.SourcesSHA256 = windowV8DeclarationSHA(current)
	pins := windowExecutorEquivalencePinsV12{OriginalSourcesSHA256: e.OriginalSourcesSHA256, SourcesSHA256: e.SourcesSHA256, ChangesSHA256: windowV8DeclarationSHA(e.ChangedFiles)}
	if validateWindowV12ExecutorEquivalencePinnedData(current, e, pins) != nil {
		t.Fatal("exact synthetic pin vector rejected")
	}
	current["internal/agent/llm.go"] = before
	e.SourcesSHA256 = windowV8DeclarationSHA(current)
	if validateWindowV12ExecutorEquivalencePinnedData(current, e, pins) == nil {
		t.Fatal("self-declared current source binding accepted")
	}
	current["internal/agent/llm.go"] = after
	e.SourcesSHA256 = windowV8DeclarationSHA(current)
	e.ChangedFiles["internal/new.go"] = windowExecutorChangeV10{After: after}
	if validateWindowV12ExecutorEquivalencePinnedData(current, e, pins) == nil {
		t.Fatal("surplus allowlist entry accepted")
	}
	// The actual gate has no reviewed root pins yet and must refuse even a
	// syntactically coherent manifest/artifact. A signed caller's map is insufficient.
	if validateWindowV12ExecutorEquivalenceData(windowActualManifest{Schema: "full-window-actual-ready.v12", Sources: current}, e) == nil {
		t.Fatal("unfrozen source equivalence granted")
	}
}
