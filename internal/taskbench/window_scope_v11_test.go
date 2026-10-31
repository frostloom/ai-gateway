package taskbench

import (
	"encoding/json"
	"strings"
	"testing"
)

func windowV11FixtureScope(t *testing.T) (fullWindowScope, ConversationSuite, PairedProfiles, fullWindowBindings) {
	t.Helper()
	s, p, b := windowScopeInputs(t)
	v, e := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	v.Schema = "full-window-scope.v11"
	v.Observation = &windowObservationDeclarationV11{Schema: "full-window-observation-declaration.v11", PrivateSettingsSHA256: strings.Repeat("d", 64), SpoolRoot: "/private/tmp/synthetic-v11-spool", SpoolRunID: strings.Repeat("e", 64), SpoolMaxBytes: 4 << 30, Boundary: "durable_local", ObserverIdentitySHA256: strings.Repeat("f", 64)}
	v.BaselineEvidenceSHA256 = "a50e6863d5de3a1479f41173052176e83262092e618c3297bfdc6ae59949a517"
	d := *v.BudgetDeclaration
	d.BaselineSHA256 = v.BaselineEvidenceSHA256
	d.Parent = windowV8Amounts{195000000, 3000000}
	d.Historical = windowV8Amounts{66825427, 638655}
	d.MaximumReserved = windowV8Amounts{194170923, 2729954}
	d.Remaining = windowV8Amounts{829077, 270046}
	v.BudgetDeclaration = &d
	v.BudgetDeclarationSHA256 = windowV8DeclarationSHA(d)
	return v, s, p, b
}
func TestWindowV11FullPrimaryDispatch(t *testing.T) {
	v, s, p, b := windowV11FixtureScope(t)
	_, tot, e := validateFullWindowScope(v, s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	want := fullWindowTotals{3660, 7320, 11772, 1851, 123827032, 2037474}
	if tot != want {
		t.Fatalf("totals %+v", tot)
	}
	if out, e := executionTotals(v); e != nil || out != want {
		t.Fatalf("execution totals %+v %v", out, e)
	}
	for _, idx := range []int{1, 56, 511} {
		w, e := executionWindowAt(v, idx)
		if e != nil || w != v.Windows[idx-1] {
			t.Fatalf("window%d %+v %v", idx, w, e)
		}
	}
	if v.Completion != nil || v.AllowPaid || v.ReadyForPaidRun || v.BudgetDeclaration.HumanAuthorized {
		t.Fatal("candidate grants authority or composes prefix")
	}
}
func TestWindowV11RejectsAlteredFullPrimary(t *testing.T) {
	v, s, p, b := windowV11FixtureScope(t)
	for name, change := range map[string]func(*fullWindowScope){
		"suffix":      func(x *fullWindowScope) { x.Windows = x.Windows[55:] },
		"composition": func(x *fullWindowScope) { c := expectedWindowCompletionDeclarationV10(); x.Completion = &c },
		"caps":        func(x *fullWindowScope) { x.Windows[0].ModelCap++; x.Windows[1].ModelCap-- },
		"order":       func(x *fullWindowScope) { x.Slots[0], x.Slots[1] = x.Slots[1], x.Slots[0] },
		"history":     func(x *fullWindowScope) { x.BudgetDeclaration.Historical.Model-- },
		"parent":      func(x *fullWindowScope) { x.BudgetDeclaration.Parent.Model++ },
		"retry":       func(x *fullWindowScope) { x.RetryDeclaration.MaxModelExtras++ },
		"paid":        func(x *fullWindowScope) { x.AllowPaid = true },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(v)
			var x fullWindowScope
			json.Unmarshal(raw, &x)
			change(&x)
			x.ScheduleSHA256 = windowV8DeclarationSHA(x.Slots)
			x.BudgetDeclarationSHA256 = windowV8DeclarationSHA(x.BudgetDeclaration)
			x.RetryPolicySHA256 = windowV8DeclarationSHA(x.RetryDeclaration)
			if _, _, e := validateFullWindowScope(x, s, p, b); e == nil {
				t.Fatal("changed primary accepted")
			}
			if _, e := executionWindowAt(x, 1); e == nil {
				t.Fatal("changed execution accepted")
			}
		})
	}
}
func TestWindowV11ScopeRequiresObservationBinding(t *testing.T) {
	v, s, p, b := windowV11FixtureScope(t)
	raw, _ := json.Marshal(v)
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	delete(fields, "observation")
	raw, _ = json.Marshal(fields)
	v = fullWindowScope{}
	json.Unmarshal(raw, &v)
	if _, _, e := validateFullWindowScope(v, s, p, b); e == nil {
		t.Fatal("V11 scope without its settings/spool identity accepted")
	}
}
