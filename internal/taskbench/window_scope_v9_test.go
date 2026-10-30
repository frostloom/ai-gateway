package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func windowV9TestCaps(t *testing.T) []fullWindowCaps {
	t.Helper()
	raw, err := os.ReadFile("testdata/window_v9_caps.json")
	if err != nil {
		t.Fatal(err)
	}
	var caps []fullWindowCaps
	if json.Unmarshal(raw, &caps) != nil {
		t.Fatal("caps fixture")
	}
	return caps
}

func TestWindowV9FullMatrixDataOnly(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatalf("new complete data-only scope rejected: %v", err)
	}
	if v.BudgetDeclarationSHA256 != "d6584f9462b6ffbfe94acede036e0b62de536fe04aacc4bb57394ccd71184664" || v.RetryPolicySHA256 != "6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63" || v.ScheduleSHA256 != "95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf" {
		t.Fatal("canonical V9 bindings changed")
	}
	rawV9, _ := json.Marshal(v)
	if conversationHash(rawV9) != "ea09e5cac060682a7a06d53fd19d11d618d331b2cf49dce057a7b31273854859" {
		t.Fatal("independent fixture scope differs from Go canonical data")
	}
	_, tot, err := validateFullWindowScope(v, s, p, b)
	if err != nil || tot != (fullWindowTotals{3660, 7320, 11772, 1851, 123827032, 2037474}) {
		t.Fatalf("full denominator/caps: %+v %v", tot, err)
	}
	if v.Schema != "full-window-scope.v9" || v.Width != 12 || len(v.Windows) != 511 {
		t.Fatal("fixed new matrix")
	}
	old, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	for i, slot := range v.Slots {
		got, want := slot, old.Slots[i]
		got.WindowID = ""
		want.WindowID = ""
		if got != want {
			t.Fatalf("slot non-window identity %d", i)
		}
	}
	next := 1
	for i, w := range v.Windows {
		turns := 0
		if w.FirstOrdinal != next || w.LastOrdinal-w.FirstOrdinal+1 > 12 {
			t.Fatalf("partition %d", i)
		}
		for j := w.FirstOrdinal - 1; j < w.LastOrdinal; j++ {
			if v.Slots[j].WindowID != w.ID {
				t.Fatal("slot window join")
			}
			turns += 2 * v.Slots[j].UserTurns
		}
		if turns > 24 || w.MaxModelCalls != 5*turns || w.MaxJEVCalls != turns/2 {
			t.Fatal("turn/call bound")
		}
		if i+1 < len(v.Windows) && w.LastOrdinal-w.FirstOrdinal+1 < 12 && turns+2*v.Slots[w.LastOrdinal].UserTurns <= 24 {
			t.Fatal("partition not greedy")
		}
		next = w.LastOrdinal + 1
	}
	if next != 3661 {
		t.Fatal("suffix removed")
	}
	d := v.BudgetDeclaration
	if d == nil || d.Parent != (windowV8Amounts{180000000, 2500000}) || d.Historical != (windowV8Amounts{52654504, 408701}) || d.Base != (windowV8Amounts{123827032, 2037474}) || d.MaximumReserved != d.Parent || d.Remaining != (windowV8Amounts{}) {
		t.Fatalf("candidate liability %+v", d)
	}
	if d.Historical.Model+d.Risk.Model+d.Base.Model+d.MaximumExtras.Model != d.Parent.Model || d.Historical.JEV+d.Risk.JEV+d.Base.JEV+d.MaximumExtras.JEV != d.Parent.JEV {
		t.Fatal("currency arithmetic")
	}
	wantRetry := expectedWindowV8RetryDeclaration()
	if !reflect.DeepEqual(v.RetryDeclaration, &wantRetry) || d.HumanAuthorized || v.AllowPaid || v.ReadyForPaidRun || v.BudgetApprovalSHA256 != "" {
		t.Fatal("candidate granted authority or changed retry")
	}
}

func TestWindowV9CoherentChangesRejected(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	original, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*fullWindowScope){
		"caps":       func(v *fullWindowScope) { v.Windows[0].ModelCap++; v.Windows[1].ModelCap-- },
		"window":     func(v *fullWindowScope) { v.Windows[0].LastOrdinal-- },
		"turns":      func(v *fullWindowScope) { v.Slots[0].UserTurns++ },
		"order":      func(v *fullWindowScope) { v.Slots[0].ArmOrder = [2]string{"jev_selective", "model"} },
		"baseline44": func(v *fullWindowScope) { v.BaselineEvidenceSHA256 = windowV8BaselineSHA },
		"authority":  func(v *fullWindowScope) { v.BudgetDeclaration.HumanAuthorized = true },
		"parent":     func(v *fullWindowScope) { v.BudgetDeclaration.Parent.Model = 200000000 },
		"historical": func(v *fullWindowScope) { v.BudgetDeclaration.Historical.Model-- },
		"retry":      func(v *fullWindowScope) { v.RetryDeclaration.MaxModelExtras++ },
		"paid":       func(v *fullWindowScope) { v.AllowPaid = true },
		"approval":   func(v *fullWindowScope) { v.BudgetApprovalSHA256 = windowV8BaselineSHA },
	}
	for name, mut := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var v fullWindowScope
			json.Unmarshal(raw, &v)
			mut(&v)
			slots, _ := json.Marshal(v.Slots)
			v.ScheduleSHA256 = conversationHash(slots)
			v.RetryPolicySHA256 = windowV8DeclarationSHA(v.RetryDeclaration)
			v.BudgetDeclaration.RetryPolicySHA256 = v.RetryPolicySHA256
			v.BudgetDeclarationSHA256 = windowV8DeclarationSHA(v.BudgetDeclaration)
			if _, _, err := validateFullWindowScope(v, s, p, b); err == nil {
				t.Fatal("coherently altered declaration accepted")
			}
		})
	}
}

func TestWindowV9DataCannotUseV8ActualAuthority(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	root := stoppedEvidenceRoot(t)
	m := windowActualManifest{Schema: "full-window-actual-ready.v9", Scope: v, SessionRoot: filepath.Join(root, "session"), CampaignRoot: filepath.Join(root, "campaign")}
	if _, err = OpenFullWindowActual(context.Background(), &FullWindowActualReady{manifest: m}, filepath.Join(root, "missing-settings")); err == nil {
		t.Fatal("data-only V9 opened actual session")
	}
	if _, err = verifyWindowV8ExecutionAuthorization([]byte(`{}`), m, windowV9BaselineSHA, windowV9CapsSHA); err == nil {
		t.Fatal("V8 authority accepted V9")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("rejection mutated namespace")
	}
}
