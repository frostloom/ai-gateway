package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A V8 declaration must be understood as data while still conferring no runtime authority.
func TestWindowV8DeclarationDataOnly(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	scope.Schema = "full-window-scope.v8"
	scope.BaselineEvidenceSHA256 = "2a78a0188ffd5072f75aeb51c9d9d54c5fdd46edc18a521571aa8aaccf38e1df"
	if _, _, err = validateFullWindowScope(scope, s, p, b); err != nil {
		t.Fatalf("data-only V8 declaration rejected: %v", err)
	}
}

func TestWindowV8RejectsRuntimeBeforeMutations(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	sha, _, err := validateFullWindowScope(scope, s, p, b)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "campaign")
	limits := windowV4ExpectedLimits()
	limits.Model.CacheHitInputRate = 0
	exec, err := executionSHAWindowCampaign(sha, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareWindowCampaign(root, root, scope, s, p, b, sha, exec, limits); err == nil {
		t.Error("V8 partially accepted by existing campaign")
	}
	ready := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v8", Scope: scope, SessionRoot: filepath.Join(parent, "session"), CampaignRoot: root}}
	if _, err = OpenFullWindowActual(context.Background(), ready, filepath.Join(parent, "missing-settings")); err == nil || !strings.Contains(err.Error(), "v9 delegated authority required before settings") {
		t.Fatalf("actual must reject epoch before settings: %v", err)
	}
	if windowActualSupportedEpoch(ready.manifest) {
		t.Error("V8 actual readiness supported")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("runtime mutated root: %v %v", entries, err)
	}
}
func TestWindowV8FixedScheduleAndLiabilities(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	_, tot, err := validateFullWindowScope(scope, s, p, b)
	if err != nil || tot != (fullWindowTotals{3660, 7320, 11772, 1851, 84500000, 600000}) {
		t.Fatalf("totals %+v %v", tot, err)
	}
	old, err := prepareFullWindowScopeV7(s, p, 28, windowV7TestCaps(), b, windowV7BaselineSHA, windowBudget130ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Slots, scope.Slots) || !reflect.DeepEqual(old.Windows, scope.Windows) || old.ScheduleSHA256 != scope.ScheduleSHA256 || old.Bindings != scope.Bindings {
		t.Fatal("original experiment changed")
	}
	d, r := scope.BudgetDeclaration, scope.RetryDeclaration
	var modelLogical, jevLogical int
	for _, w := range scope.Windows {
		modelLogical += w.MaxModelCalls
		jevLogical += w.MaxJEVCalls
	}
	if modelLogical != r.MaxLogicalModel || jevLogical != r.MaxLogicalJEV || r.MaxNativeModel != modelLogical+r.MaxModelExtras || r.MaxNativeJEV != jevLogical+r.MaxJEVExtras {
		t.Fatal("native retries changed logical schedule")
	}

	if d.Parent != (windowV8Amounts{140000000, 1100000}) || d.Historical != (windowV8Amounts{51200203, 399529}) || d.Risk != (windowV8Amounts{2023424, 65}) || d.Base != (windowV8Amounts{84500000, 600000}) || d.MaximumExtras != (windowV8Amounts{1495040, 53760}) || d.MaximumReserved != (windowV8Amounts{139218667, 1053354}) || d.Remaining != (windowV8Amounts{781333, 46646}) {
		t.Fatalf("wrong financial declaration: %+v", d)
	}
	if d.Historical.Model+d.Risk.Model+d.Base.Model+d.MaximumExtras.Model != d.MaximumReserved.Model || d.Historical.JEV+d.Risk.JEV+d.Base.JEV+d.MaximumExtras.JEV != d.MaximumReserved.JEV || d.Parent.Model-d.MaximumReserved.Model != d.Remaining.Model || d.Parent.JEV-d.MaximumReserved.JEV != d.Remaining.JEV || int64(r.MaxModelExtras)*r.ModelExtra.Model != d.MaximumExtras.Model || int64(r.MaxJEVExtras)*r.JEVExtra.JEV != d.MaximumExtras.JEV {
		t.Fatal("separate currency arithmetic")
	}
	if r.MaxLogicalModel != 58860 || r.MaxLogicalJEV != 5886 || r.MaxNativeModel != 58880 || r.MaxNativeJEV != 5906 || r.MaxRetriesPerLogical != 1 || r.MaxModelExtras != 20 || r.MaxJEVExtras != 20 || r.ModelDeadlineMS != 90000 || r.JEVDeadlineMS != 10000 || r.KnowledgeModel != "off" || r.KnowledgeJEV != "off" || r.Model != "deepseek-flash" || r.JEV != "jev-1.13.0" {
		t.Fatalf("logical/native policy %+v", r)
	}
	if scope.AllowPaid || scope.ReadyForPaidRun || d.HumanAuthorized || scope.BudgetApprovalSHA256 != "" {
		t.Fatal("candidate became authority")
	}
	if scope.BudgetDeclarationSHA256 != "ca7a9c43141a0fc416b24f019567a489935d8988db155fd0f56bc87f5014d725" || scope.RetryPolicySHA256 != "6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63" || scope.ScheduleSHA256 != "ef427c0a70fc6186818d6ccc8306f18099d6523801005e61f4fa469b431ad5b3" {
		t.Fatal("fixed canonical declaration identity changed")
	}
}
func TestWindowV8RejectsCoherentDeclarationTampering(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	original, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*fullWindowScope){
		"history":           func(v *fullWindowScope) { v.BaselineEvidenceSHA256 = windowV7BaselineSHA },
		"approval":          func(v *fullWindowScope) { v.BudgetApprovalSHA256 = windowBudget130ApprovalSHA },
		"human":             func(v *fullWindowScope) { v.BudgetDeclaration.HumanAuthorized = true },
		"parent":            func(v *fullWindowScope) { v.BudgetDeclaration.Parent.Model++ },
		"historical amount": func(v *fullWindowScope) { v.BudgetDeclaration.Historical.JEV-- },
		"base digest":       func(v *fullWindowScope) { v.BudgetDeclaration.BaseCapsSHA256 = strings.Repeat("a", 64) },
		"budget digest":     func(v *fullWindowScope) { v.BudgetDeclarationSHA256 = strings.Repeat("a", 64) },
		"retry digest":      func(v *fullWindowScope) { v.RetryPolicySHA256 = strings.Repeat("a", 64) },
		"caps":              func(v *fullWindowScope) { v.Windows[0].ModelCap++; v.Windows[1].ModelCap-- },
		"width":             func(v *fullWindowScope) { v.Width = 27 },
		"retry count":       func(v *fullWindowScope) { v.RetryDeclaration.MaxRetriesPerLogical = 2 },
		"model quota":       func(v *fullWindowScope) { v.RetryDeclaration.MaxModelExtras = 21 },
		"jev quota":         func(v *fullWindowScope) { v.RetryDeclaration.MaxJEVExtras = 21 },
		"condition":         func(v *fullWindowScope) { v.RetryDeclaration.Condition = "any_eof" },
		"deadline":          func(v *fullWindowScope) { v.RetryDeclaration.JEVDeadlineMS++ },
		"logical native":    func(v *fullWindowScope) { v.RetryDeclaration.MaxLogicalModel = v.RetryDeclaration.MaxNativeModel },
		"native quota":      func(v *fullWindowScope) { v.RetryDeclaration.MaxNativeJEV++ },
		"window native":     func(v *fullWindowScope) { v.RetryDeclaration.PerWindowModelNativeExtra++ },
		"knowledge":         func(v *fullWindowScope) { v.RetryDeclaration.KnowledgeModel = "on" },
		"missing budget":    func(v *fullWindowScope) { v.BudgetDeclaration = nil },
		"missing retry":     func(v *fullWindowScope) { v.RetryDeclaration = nil },
		"paid":              func(v *fullWindowScope) { v.AllowPaid = true },
		"ready":             func(v *fullWindowScope) { v.ReadyForPaidRun = true },
		"source":            func(v *fullWindowScope) { v.Bindings.ExecutorRevision = strings.Repeat("f", 40) },
		"policy":            func(v *fullWindowScope) { v.Bindings.PolicySHA256 = strings.Repeat("f", 64) },
		"schedule":          func(v *fullWindowScope) { v.Slots[0].UserTurns++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			for _, coherent := range []bool{false, true} {
				if coherent && (name == "budget digest" || name == "retry digest") {
					continue
				}
				raw, _ := json.Marshal(original)
				var v fullWindowScope
				json.Unmarshal(raw, &v)
				mutate(&v)
				if coherent {
					v.RetryPolicySHA256 = windowV8DeclarationSHA(v.RetryDeclaration)
					if v.BudgetDeclaration != nil {
						v.BudgetDeclaration.RetryPolicySHA256 = v.RetryPolicySHA256
					}
					v.BudgetDeclarationSHA256 = windowV8DeclarationSHA(v.BudgetDeclaration)
					slots, _ := json.Marshal(v.Slots)
					v.ScheduleSHA256 = conversationHash(slots)
				}
				if _, _, err := validateFullWindowScope(v, s, p, b); err == nil {
					t.Fatalf("accepted coherent=%v", coherent)
				}
			}
		})
	}
}
func TestWindowV8LegacyFieldInjectionAndBytes(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v8, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	// Every prior schema must rederive empty new fields and preserve its bytes.
	for epoch := 1; epoch <= 7; epoch++ {
		t.Run(string(rune('0'+epoch)), func(t *testing.T) {
			var scope fullWindowScope
			var e error
			small := make([]fullWindowCaps, 131)
			for i := range small {
				small[i] = fullWindowCaps{100, 1}
			}
			switch epoch {
			case 1:
				scope, e = prepareFullWindowScope(s, p, 28, small, b)
			case 2:
				scope, e = prepareFullWindowScopeV2(s, p, 28, small, b, strings.Repeat("8", 64))
			case 3:
				scope, e = prepareFullWindowScopeV3(s, p, 28, small, b, strings.Repeat("8", 64), windowBudget105ApprovalSHA)
			case 4:
				scope, e = prepareFullWindowScopeV4(s, p, 28, windowV4TestCaps(t), b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
			case 5:
				scope, e = prepareFullWindowScopeV5(s, p, 28, windowV5TestCaps(t), b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
			case 6:
				scope, e = prepareFullWindowScopeV6(s, p, 28, windowV6TestCaps(), b, windowV6BaselineSHA, windowBudget130ApprovalSHA)
			case 7:
				scope, e = prepareFullWindowScopeV7(s, p, 28, windowV7TestCaps(), b, windowV7BaselineSHA, windowBudget130ApprovalSHA)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = validateFullWindowScope(scope, s, p, b); e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(scope)
			expectedSHA := []string{
				"d62ef63429bb119db7a9a95725c9a82ee060214bb9fb0c6baf711025a55a0576",
				"a15efbb2c1e9718e5d59ea115e81bb5ce2aad0f73957f1abccd79d0e7099ddd8",
				"5c457f56b41d3618de6c26d7aab4db939e2a24a85e0adf204426fcef40547c0a",
				"20c3b50513fcf7a5f4307afdd5cd94de98d65505264457f1362c5f32d8508fef",
				"d27f28c4c772273408c7300d41da20d7244320412a7752507a42a64160130ed8",
				"0a39e328698ca5be07078a37eb3f533f2d50bdf0e0d55115ec3686e0ede000d5",
				"76aaf83c6fd856f20f0c4f9da8211e0e256977580dded61b68b15cb5d97e595c",
			}
			if conversationHash(raw) != expectedSHA[epoch-1] {
				t.Fatal("legacy canonical JSON differs from HEAD overlay baseline")
			}
			if strings.Contains(string(raw), "declaration") || strings.Contains(string(raw), "retry_policy") {
				t.Fatal("legacy JSON changed")
			}
			var fields map[string]json.RawMessage
			json.Unmarshal(raw, &fields)
			for _, key := range []string{"budget_declaration", "retry_declaration", "budget_declaration_sha256", "retry_policy_sha256"} {
				if _, ok := fields[key]; ok {
					t.Fatal("new field serialized")
				}
			}
			scope.BudgetDeclaration = v8.BudgetDeclaration
			scope.RetryDeclaration = v8.RetryDeclaration
			scope.BudgetDeclarationSHA256 = v8.BudgetDeclarationSHA256
			scope.RetryPolicySHA256 = v8.RetryPolicySHA256
			if _, _, e = validateFullWindowScope(scope, s, p, b); e == nil {
				t.Fatal("legacy accepted V8 fields")
			}
		})
	}
}
