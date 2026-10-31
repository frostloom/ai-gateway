package taskbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const windowV10CompletionFixture = `{
 "schema":"full-window-completion-declaration.v1","human_reviewed_exact_scope":false,
 "original_manifest_sha256":"aa9a664d767c080648213e3d98c63c340d2207afea9121d104bbb2775fdc7666",
 "original_scope_sha256":"e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9",
 "original_source_commit":"4ddd37ad5ba44db630bee61ddfebba5402e09d45",
 "original_sources_sha256":"47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00",
 "original_caps_sha256":"e75a0f693700f12d95bb7bf29a2c32d6d7f11e0890433fca3ad49fbf407a244a",
 "original_schedule_sha256":"95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf",
 "suffix_windows_sha256":"e383d21f0a3981e998ab1f44d2bd1eb886072d04320a727dc2c0fea5bb1e24d2",
 "suffix_caps_sha256":"7e67ca0b51e7c1ef087cfa490ebbfbd7eda0d30f91e205e0d50272845116701e",
 "prefix_summary_sha256":"42be06b9399b999bb83e939c0722e0e2d0c952608af529ed923c29046f00206d",
 "prefix_delivery_sha256":"3ed36a445a5e8be86773d707992974c690dba41f63e2967c2066c3948612ef06",
 "prefix_window_ids":[],
 "prefix":{"first_ordinal":1,"last_ordinal":385,"first_declared_window":1,"window_count":55,"pairs":385,"arms":770,"defined_user_turns":1268,"max_model_calls":6340,"max_jev_calls":634},
 "execution":{"first_ordinal":386,"last_ordinal":3660,"first_declared_window":56,"window_count":456,"pairs":3275,"arms":6550,"defined_user_turns":10504,"max_model_calls":52520,"max_jev_calls":5252},
 "primary":{"pairs":3660,"arms":7320,"defined_user_turns":11772},"boundaries":1851,"prefix_score_count":2300,"prefix_positive_score_holes":10,
 "stopped_overhead":{"window_id":"window-056","first_ordinal":386,"last_ordinal":390,"pairs":5,"arms":10,"defined_user_turns":22,"native_calls":23,"model_calls":18,"jev_calls":5,"scores_held":true},
 "all_attempt":{"pairs":3665,"arms":7330,"defined_user_turns":11794},
 "historical_global":{"model_micro_cny":200000000,"jev_micro_usd":4000000},"original_parent":{"model_micro_cny":180000000,"jev_micro_usd":2500000},
 "prior_model_extras":0,"prior_jev_extras":0,"cohort_max_model_extras":20,"cohort_max_jev_extras":20
}`

// This fixture enters through the existing validator. Before V10 dispatch it
// fails with unsupported schema, rather than merely referring to a missing API.
func windowV10FixtureScope(t *testing.T) (fullWindowScope, ConversationSuite, PairedProfiles, fullWindowBindings) {
	t.Helper()
	s, p, b := windowScopeInputs(t)
	v, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	v.Schema = "full-window-scope.v10"
	v.BaselineEvidenceSHA256 = "2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47"
	v.Windows = append([]fullWindowDeclaration(nil), v.Windows[55:]...)
	retry := expectedWindowV8RetryDeclaration()
	retry.MaxLogicalModel = 52520
	retry.MaxLogicalJEV = 5252
	retry.MaxNativeModel = 52540
	retry.MaxNativeJEV = 5272
	v.RetryDeclaration = &retry
	v.RetryPolicySHA256 = windowV8DeclarationSHA(retry)
	budget := windowV8BudgetDeclaration{Schema: "full-window-budget-declaration.v1", BaselineSHA256: v.BaselineEvidenceSHA256, BaseCapsSHA256: "7e67ca0b51e7c1ef087cfa490ebbfbd7eda0d30f91e205e0d50272845116701e", RetryPolicySHA256: v.RetryPolicySHA256, Parent: windowV8Amounts{185000000, 3000000}, Historical: windowV8Amounts{66472061, 631114}, Risk: windowV8Amounts{2023424, 65}, Base: windowV8Amounts{110191257, 1818872}, MaximumExtras: windowV8Amounts{1495040, 53760}, MaximumReserved: windowV8Amounts{180181782, 2503811}, Remaining: windowV8Amounts{4818218, 496189}}
	v.BudgetDeclaration = &budget
	v.BudgetDeclarationSHA256 = windowV8DeclarationSHA(budget)
	raw, _ := json.Marshal(v)
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var completion map[string]json.RawMessage
	if err = json.Unmarshal([]byte(windowV10CompletionFixture), &completion); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 55)
	for i, w := range windowV9FixtureWindows(t, s, p, b)[:55] {
		ids[i] = w.ID
	}
	completion["prefix_window_ids"], _ = json.Marshal(ids)
	fields["completion"], _ = json.Marshal(completion)
	raw, _ = json.Marshal(fields)
	if err = json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, s, p, b
}
func windowV9FixtureWindows(t *testing.T, s ConversationSuite, p PairedProfiles, b fullWindowBindings) []fullWindowDeclaration {
	t.Helper()
	v, e := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	return v.Windows
}
func TestWindowV10ExistingEntryUnderstandsExactSuffix(t *testing.T) {
	v, s, p, b := windowV10FixtureScope(t)
	_, tot, err := validateFullWindowScope(v, s, p, b)
	if err != nil {
		t.Fatalf("fixed completion suffix declaration rejected: %v", err)
	}
	if tot != (fullWindowTotals{3275, 6550, 10504, 0, 110191257, 1818872}) {
		t.Fatalf("execution totals %+v", tot)
	}
}

func TestWindowV10BuilderPreservesOriginalMatrixAndSuffix(t *testing.T) {
	fixture, s, p, b := windowV10FixtureScope(t)
	v, err := prepareFullWindowScopeV10(s, p, windowV9TestCaps(t)[55:], b, "2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(v)
	want, _ := json.Marshal(fixture)
	if string(got) != string(want) {
		t.Fatal("constructor differs from independently pinned fixture")
	}
	if len(v.Slots) != 3660 || len(v.Windows) != 456 || v.ScheduleSHA256 != "95469ad60b07de7f11eeaf16c638fbc8e9b677e708eb3b4f7247246535859ccf" {
		t.Fatal("full matrix or suffix removed")
	}
	for _, w := range v.Windows {
		if w.FirstOrdinal < 386 {
			t.Fatal("prefix entered executable suffix")
		}
	}
	for _, index := range []int{-1, 0, 457, 511} {
		if _, e := executionWindowAt(v, index); e == nil {
			t.Fatalf("out-of-range local index %d accepted", index)
		}
	}
	first, e := executionWindowAt(v, 1)
	if e != nil || first.ID != "window-056" || first.FirstOrdinal != 386 || first.LastOrdinal != 390 {
		t.Fatalf("local1 mapping %+v %v", first, e)
	}
	last, e := executionWindowAt(v, 456)
	if e != nil || last.ID != "window-511" || last.FirstOrdinal != 3650 || last.LastOrdinal != 3660 {
		t.Fatalf("local456 mapping %+v %v", last, e)
	}
	totals, e := executionTotals(v)
	if e != nil || totals != (fullWindowTotals{3275, 6550, 10504, 0, 110191257, 1818872}) {
		t.Fatalf("suffix totals %+v %v", totals, e)
	}
	if v.Completion.Primary != (windowCompletionCountsV10{3660, 7320, 11772}) || v.Completion.AllAttempt != (windowCompletionCountsV10{3665, 7330, 11794}) || v.Completion.Boundaries != 1851 {
		t.Fatal("composition lost primary/all-attempt denominators")
	}
	if v.AllowPaid || v.ReadyForPaidRun || v.BudgetDeclaration.HumanAuthorized || v.Completion.HumanReviewedExactScope || v.BudgetApprovalSHA256 != "" {
		t.Fatal("declaration granted authority")
	}
	old, e := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), windowV10OriginalBindings(), windowV9BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	oldRaw, _ := json.Marshal(old)
	if conversationHash(oldRaw) != "e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9" {
		t.Fatal("original R5 full scope not reconstructed")
	}
	if len(v.Completion.PrefixWindowIDs) != 55 {
		t.Fatal("fixed prefix missing")
	}
	for i, slot := range v.Slots {
		if slot != old.Slots[i] {
			t.Fatalf("original slot changed at %d", i+1)
		}
	}
	for i, w := range v.Windows {
		if w != old.Windows[i+55] {
			t.Fatalf("suffix declaration changed at local%d", i+1)
		}
	}
}

func TestWindowV10RejectsCoherentChanges(t *testing.T) {
	original, s, p, b := windowV10FixtureScope(t)
	mutations := map[string]func(*fullWindowScope){
		"missing prefix slot": func(v *fullWindowScope) { v.Slots = v.Slots[1:] },
		"duplicate tuple":     func(v *fullWindowScope) { v.Slots[1] = v.Slots[0] },
		"case":                func(v *fullWindowScope) { v.Slots[385].CaseKey = v.Slots[0].CaseKey },
		"pass":                func(v *fullWindowScope) { v.Slots[385].Pass++ },
		"armorder": func(v *fullWindowScope) {
			v.Slots[385].ArmOrder[0], v.Slots[385].ArmOrder[1] = v.Slots[385].ArmOrder[1], v.Slots[385].ArmOrder[0]
		},
		"turns":          func(v *fullWindowScope) { v.Slots[385].UserTurns++ },
		"prefix egress":  func(v *fullWindowScope) { v.Windows[0].ID = "window-001"; v.Windows[0].FirstOrdinal = 1 },
		"overlap":        func(v *fullWindowScope) { v.Windows[1].FirstOrdinal-- },
		"missing window": func(v *fullWindowScope) { v.Windows = v.Windows[1:] },
		"coherent caps":  func(v *fullWindowScope) { v.Windows[0].ModelCap++; v.Windows[1].ModelCap-- },
		"coherent calls": func(v *fullWindowScope) {
			v.Windows[0].MaxModelCalls += 10
			v.Windows[0].MaxJEVCalls++
			v.Windows[1].MaxModelCalls -= 10
			v.Windows[1].MaxJEVCalls--
		},
		"prefix source":      func(v *fullWindowScope) { v.Completion.PrefixSummarySHA256 = v.ScheduleSHA256 },
		"quality selection":  func(v *fullWindowScope) { v.Completion.PrefixWindowIDs = v.Completion.PrefixWindowIDs[1:] },
		"union":              func(v *fullWindowScope) { v.Completion.Execution.FirstOrdinal = 385 },
		"full denominator":   func(v *fullWindowScope) { v.Completion.Primary.Arms = 6550 },
		"all attempt":        func(v *fullWindowScope) { v.Completion.AllAttempt.Arms = 7320 },
		"stop normal":        func(v *fullWindowScope) { v.Completion.StoppedOverhead.ScoresHeld = false },
		"stop native":        func(v *fullWindowScope) { v.Completion.StoppedOverhead.NativeCalls = 22 },
		"new cap digest":     func(v *fullWindowScope) { v.Completion.SuffixCapsSHA256 = v.ScheduleSHA256 },
		"baseline":           func(v *fullWindowScope) { v.BaselineEvidenceSHA256 = windowV9BaselineSHA },
		"global":             func(v *fullWindowScope) { v.Completion.HistoricalGlobal.Model = 185000000 },
		"parent":             func(v *fullWindowScope) { v.BudgetDeclaration.Parent.JEV = 2500000 },
		"history":            func(v *fullWindowScope) { v.BudgetDeclaration.Historical.Model-- },
		"extras reset":       func(v *fullWindowScope) { v.Completion.PriorModelExtras++ },
		"extras quota":       func(v *fullWindowScope) { v.RetryDeclaration.MaxJEVExtras++ },
		"deadline":           func(v *fullWindowScope) { v.RetryDeclaration.JEVDeadlineMS++ },
		"human review":       func(v *fullWindowScope) { v.Completion.HumanReviewedExactScope = true },
		"paid":               func(v *fullWindowScope) { v.AllowPaid = true },
		"ready":              func(v *fullWindowScope) { v.ReadyForPaidRun = true },
		"human authorized":   func(v *fullWindowScope) { v.BudgetDeclaration.HumanAuthorized = true },
		"approval":           func(v *fullWindowScope) { v.BudgetApprovalSHA256 = v.ScheduleSHA256 },
		"missing completion": func(v *fullWindowScope) { v.Completion = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var v fullWindowScope
			if e := json.Unmarshal(raw, &v); e != nil {
				t.Fatal(e)
			}
			mutate(&v)
			slots, _ := json.Marshal(v.Slots)
			v.ScheduleSHA256 = conversationHash(slots)
			v.RetryPolicySHA256 = windowV8DeclarationSHA(v.RetryDeclaration)
			v.BudgetDeclaration.RetryPolicySHA256 = v.RetryPolicySHA256
			v.BudgetDeclarationSHA256 = windowV8DeclarationSHA(v.BudgetDeclaration)
			if _, _, e := validateFullWindowScope(v, s, p, b); e == nil {
				t.Fatal("coherently rewritten scope accepted")
			}
		})
	}
	for _, caps := range [][]fullWindowCaps{windowV9TestCaps(t), windowV9TestCaps(t)[56:], nil} {
		if _, e := prepareFullWindowScopeV10(s, p, caps, b, "2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47"); e == nil {
			t.Fatal("nonexact suffix accepted")
		}
	}
	caps := windowV9TestCaps(t)[55:]
	caps[0].ModelCap++
	caps[1].ModelCap--
	if _, e := prepareFullWindowScopeV10(s, p, caps, b, "2e3d61e0fb383376279b5ea7d7473c48892910844855119a8c2026b063151a47"); e == nil {
		t.Fatal("coherent cap vector accepted")
	}
}

func TestWindowV10BudgetAndCohortAccounting(t *testing.T) {
	v, _, _, _ := windowV10FixtureScope(t)
	d, r, c := v.BudgetDeclaration, v.RetryDeclaration, v.Completion
	if d.Historical.Model+d.Base.Model+d.Risk.Model+d.MaximumExtras.Model != 180181782 || d.Historical.JEV+d.Base.JEV+d.Risk.JEV+d.MaximumExtras.JEV != 2503811 {
		t.Fatal("suffix liability excludes history/risk/extras or double counts old stop")
	}
	if d.Parent.Model-d.MaximumReserved.Model != 4818218 || d.Parent.JEV-d.MaximumReserved.JEV != 496189 || c.HistoricalGlobal.Model-d.MaximumReserved.Model != 19818218 || c.HistoricalGlobal.JEV-d.MaximumReserved.JEV != 1496189 {
		t.Fatal("parent/global currencies conflated")
	}
	if c.Prefix.MaxModelCalls+c.Execution.MaxModelCalls != 58860 || c.Prefix.MaxJEVCalls+c.Execution.MaxJEVCalls != 5886 {
		t.Fatal("original logical maxima changed")
	}
	if r.MaxLogicalModel != 52520 || r.MaxLogicalJEV != 5252 || r.MaxNativeModel != 52540 || r.MaxNativeJEV != 5272 || r.MaxModelExtras+c.PriorModelExtras != c.CohortMaxModelExtras || r.MaxJEVExtras+c.PriorJEVExtras != c.CohortMaxJEVExtras {
		t.Fatal("suffix reset or enlarged cohort extras")
	}
	if int64(r.MaxModelExtras)*r.ModelExtra.Model != d.MaximumExtras.Model || int64(r.MaxJEVExtras)*r.JEVExtra.JEV != d.MaximumExtras.JEV {
		t.Fatal("extra budget mismatch")
	}
	if c.Prefix.Pairs+c.Execution.Pairs != c.Primary.Pairs || c.Prefix.Arms+c.Execution.Arms != c.Primary.Arms || c.Prefix.DefinedUserTurns+c.Execution.DefinedUserTurns != c.Primary.DefinedUserTurns || c.Primary.Arms+c.StoppedOverhead.Arms != c.AllAttempt.Arms || c.Primary.DefinedUserTurns+c.StoppedOverhead.DefinedUserTurns != c.AllAttempt.DefinedUserTurns {
		t.Fatal("primary/all-attempt union mismatch")
	}
	if r.ModelDeadlineMS != 90000 || r.JEVDeadlineMS != 10000 || r.KnowledgeModel != "off" || r.KnowledgeJEV != "off" || r.MaxRetriesPerLogical != 1 || r.Condition != "native1_nil_response_exactly_one_new_connection_tls_handshake_eof_no_gotconn_no_request_write_no_first_byte_no_success_or_contradictory_or_missing_or_late_trace_original_request_and_parent_live" {
		t.Fatal("original transport bounds changed")
	}
}

func TestWindowV10ExecutionHelpersRejectRewrittenOrPrefixScope(t *testing.T) {
	original, _, _, _ := windowV10FixtureScope(t)
	mutations := map[string]func(*fullWindowScope){
		"prefix": func(v *fullWindowScope) { v.Windows[0].ID = "window-055"; v.Windows[0].FirstOrdinal = 379 },
		"coherent slots": func(v *fullWindowScope) {
			v.Slots[0], v.Slots[1] = v.Slots[1], v.Slots[0]
			v.ScheduleSHA256 = windowV8DeclarationSHA(v.Slots)
		},
		"missing completion": func(v *fullWindowScope) { v.Completion = nil },
		"schema downgrade":   func(v *fullWindowScope) { v.Schema = "full-window-scope.v9" },
		"unknown schema":     func(v *fullWindowScope) { v.Schema = "full-window-scope.v11" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var v fullWindowScope
			if e := json.Unmarshal(raw, &v); e != nil {
				t.Fatal(e)
			}
			mutate(&v)
			if _, e := executionWindowAt(v, 1); e == nil {
				t.Fatal("execution helper accepted altered mapping")
			}
			if _, e := executionTotals(v); e == nil {
				t.Fatal("totals helper accepted altered mapping")
			}
		})
	}
}

func TestWindowV10CompletionCannotBeInjectedIntoLegacySchemas(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	for epoch := 1; epoch <= 9; epoch++ {
		t.Run(fmt.Sprint(epoch), func(t *testing.T) {
			small := make([]fullWindowCaps, 131)
			for i := range small {
				small[i] = fullWindowCaps{100, 1}
			}
			var v fullWindowScope
			var err error
			switch epoch {
			case 1:
				v, err = prepareFullWindowScope(s, p, 28, small, b)
			case 2:
				v, err = prepareFullWindowScopeV2(s, p, 28, small, b, strings.Repeat("8", 64))
			case 3:
				v, err = prepareFullWindowScopeV3(s, p, 28, small, b, strings.Repeat("8", 64), windowBudget105ApprovalSHA)
			case 4:
				v, err = prepareFullWindowScopeV4(s, p, 28, windowV4TestCaps(t), b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
			case 5:
				v, err = prepareFullWindowScopeV5(s, p, 28, windowV5TestCaps(t), b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
			case 6:
				v, err = prepareFullWindowScopeV6(s, p, 28, windowV6TestCaps(), b, windowV6BaselineSHA, windowBudget130ApprovalSHA)
			case 7:
				v, err = prepareFullWindowScopeV7(s, p, 28, windowV7TestCaps(), b, windowV7BaselineSHA, windowBudget130ApprovalSHA)
			case 8:
				v, err = prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
			case 9:
				v, err = prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(v)
			expected := []string{"d62ef63429bb119db7a9a95725c9a82ee060214bb9fb0c6baf711025a55a0576", "a15efbb2c1e9718e5d59ea115e81bb5ce2aad0f73957f1abccd79d0e7099ddd8", "5c457f56b41d3618de6c26d7aab4db939e2a24a85e0adf204426fcef40547c0a", "20c3b50513fcf7a5f4307afdd5cd94de98d65505264457f1362c5f32d8508fef", "d27f28c4c772273408c7300d41da20d7244320412a7752507a42a64160130ed8", "0a39e328698ca5be07078a37eb3f533f2d50bdf0e0d55115ec3686e0ede000d5", "76aaf83c6fd856f20f0c4f9da8211e0e256977580dded61b68b15cb5d97e595c", "14e5a779ba7c01aac5efed9fb5eb1a9a08f21124b387b07c0062dd6e9b54c245", "ea09e5cac060682a7a06d53fd19d11d618d331b2cf49dce057a7b31273854859"}
			if conversationHash(raw) != expected[epoch-1] {
				t.Fatal("legacy canonical scope bytes changed")
			}
			if strings.Contains(string(raw), `"completion"`) {
				t.Fatal("nil completion serialized in old epoch")
			}
			if _, _, err = validateFullWindowScope(v, s, p, b); err != nil {
				t.Fatal(err)
			}
			if _, err = executionWindowAt(v, 1); err != nil {
				t.Fatal(err)
			}
			totals, err := executionTotals(v)
			if err != nil || totals.Pairs != 3660 || totals.Arms != 7320 || totals.DefinedUserTurns != 11772 || totals.Boundaries != 1851 {
				t.Fatal("legacy helper totals changed")
			}
			completion := expectedWindowCompletionDeclarationV10()
			v.Completion = &completion
			if _, _, err = validateFullWindowScope(v, s, p, b); err == nil {
				t.Fatal("legacy schema accepted completion injection")
			}
		})
	}
}

func TestWindowV10SourceAndBindingsCannotBeRelabeled(t *testing.T) {
	v, s, p, b := windowV10FixtureScope(t)
	caps := windowV9TestCaps(t)[55:]
	for name, mutate := range map[string]func(*ConversationSuite){
		"requirements":      func(s *ConversationSuite) { s.Cases[0].Assertions[0].Requirement += " changed" },
		"prompt":            func(s *ConversationSuite) { s.Cases[0].Scenario.Turns[0].User += " changed" },
		"definition digest": func(s *ConversationSuite) { s.DefinitionSHA256 = strings.Repeat("a", 64) },
		"manifest digest":   func(s *ConversationSuite) { s.ManifestSHA256 = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(s)
			var changed ConversationSuite
			if e := json.Unmarshal(raw, &changed); e != nil {
				t.Fatal(e)
			}
			mutate(&changed)
			if _, e := prepareFullWindowScopeV10(changed, p, caps, b, windowV10BaselineSHA); e == nil {
				t.Fatal("relabeled changed source accepted")
			}
		})
	}
	raw, _ := json.Marshal(p.archive)
	var changed runtimeProfileArchive
	if e := json.Unmarshal(raw, &changed); e != nil {
		t.Fatal(e)
	}
	for key, account := range changed.Accounts {
		account.Balance++
		changed.Accounts[key] = account
		break
	}
	if _, e := prepareFullWindowScopeV10(s, PairedProfiles{archive: changed, SHA256: p.SHA256}, caps, b, windowV10BaselineSHA); e == nil {
		t.Fatal("relabeled changed profile accepted")
	}
	bad := b
	bad.ExecutorRevision = "bad"
	if _, e := prepareFullWindowScopeV10(s, p, caps, bad, windowV10BaselineSHA); e == nil {
		t.Fatal("invalid revision accepted")
	}
	bad = b
	bad.PolicySHA256 = ""
	if _, e := prepareFullWindowScopeV10(s, p, caps, bad, windowV10BaselineSHA); e == nil {
		t.Fatal("invalid evidence accepted")
	}
	if _, e := prepareFullWindowScopeV10(s, p, caps, b, windowV9BaselineSHA); e == nil {
		t.Fatal("old stopped46 baseline accepted")
	}
	bad = b
	bad.ExecutorRevision = strings.Repeat("f", 40)
	if _, _, e := validateFullWindowScope(v, s, p, bad); e == nil {
		t.Fatal("mismatched externally frozen bindings accepted")
	}
	// A syntactically valid new executor/customer label is only a declaration;
	// actual equivalence and authorization remain independent runtime gates.
	bad.CustomerRevision = strings.Repeat("f", 40)
	candidate, e := prepareFullWindowScopeV10(s, p, caps, bad, windowV10BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	if candidate.AllowPaid || candidate.ReadyForPaidRun || candidate.BudgetDeclaration.HumanAuthorized || candidate.Completion.HumanReviewedExactScope {
		t.Fatal("new revision label granted authority")
	}
}
