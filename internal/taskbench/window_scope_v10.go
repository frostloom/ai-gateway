package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

func windowV10OriginalBindings() fullWindowBindings {
	return fullWindowBindings{
		PolicySHA256:     "333c69bbb7506bd0c9237891cce8ddca720174974bf3f85753dd62339f368f15",
		ExecutorRevision: "4ddd37ad5ba44db630bee61ddfebba5402e09d45", CustomerRevision: "4ddd37ad5ba44db630bee61ddfebba5402e09d45",
		PriceEvidenceSHA256:   "d331a34e0f088ea126d72fb36ed253a2936e1256e4bd60dbcab3b989015cb119",
		RequestEnvelopeSHA256: "c309336c3bd024e4894bdaa9b7aff35c7d667e43b19e0705844086f3f6653dea",
	}
}

// Non-secret scalar caps copied from the pinned R5 manifest aa9a664d.... They
// only reconstruct the old full vector; the old scope is never made executable.
func windowV10PrefixCaps() []fullWindowCaps {
	return []fullWindowCaps{
		{228487, 4383}, {250586, 4220}, {385639, 4383}, {369503, 4056}, {399908, 3975},
		{393985, 4217}, {385815, 4383}, {395363, 4220}, {371884, 3975}, {371064, 3975},
		{392999, 4301}, {375428, 4301}, {395507, 4383}, {371613, 4138}, {413484, 4138},
		{398709, 4216}, {405729, 4301}, {392796, 4383}, {409784, 4138}, {397603, 4383},
		{391595, 4383}, {303093, 4138}, {159833, 3893}, {159840, 3893}, {144084, 3811},
		{165599, 3811}, {144093, 3811}, {160140, 3811}, {159821, 3893}, {154591, 3893},
		{159939, 3893}, {154813, 3893}, {176399, 3811}, {165915, 3811}, {170672, 3893},
		{159856, 3893}, {154651, 3893}, {159838, 3893}, {160177, 3811}, {165694, 3811},
		{160611, 3730}, {171420, 3730}, {155201, 3730}, {171439, 3730}, {182297, 3730},
		{155076, 3730}, {187513, 3730}, {182024, 3730}, {171383, 3730}, {182045, 3730},
		{182253, 3730}, {171413, 3730}, {154972, 3893}, {160172, 3811}, {171427, 3730},
	}
}

func expectedWindowV10RetryDeclaration() windowV8RetryDeclaration {
	out := expectedWindowV8RetryDeclaration()
	// Extras are the remaining allowance of the same completion cohort: R5 used
	// zero. The original full logical maxima remain prefix + execution in Completion.
	out.MaxLogicalModel = 52520
	out.MaxLogicalJEV = 5252
	out.MaxNativeModel = 52540
	out.MaxNativeJEV = 5272
	return out
}
func expectedWindowV10BudgetDeclaration() windowV8BudgetDeclaration {
	return windowV8BudgetDeclaration{
		Schema: "full-window-budget-declaration.v1", HumanAuthorized: false,
		BaselineSHA256: windowV10BaselineSHA, BaseCapsSHA256: windowV10SuffixCapsSHA,
		RetryPolicySHA256: windowV8DeclarationSHA(expectedWindowV10RetryDeclaration()),
		Parent:            windowV8Amounts{185000000, 3000000}, Historical: windowV8Amounts{66472061, 631114},
		Risk: windowV8Amounts{2023424, 65}, Base: windowV8Amounts{110191257, 1818872},
		MaximumExtras: windowV8Amounts{1495040, 53760}, MaximumReserved: windowV8Amounts{180181782, 2503811},
		Remaining: windowV8Amounts{4818218, 496189},
	}
}

// prepareFullWindowScopeV10 accepts only the exact remaining 456 caps, never an
// arbitrary range. It rederives the original complete R5 scope before slicing.
// The new bindings are declarations; physical customer equivalence and actual
// authorization must be independently established by the runtime freeze.
func prepareFullWindowScopeV10(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV10BaselineSHA || len(caps) != 456 {
		return fullWindowScope{}, fmt.Errorf("v10 exact stopped102 and456 suffix caps required")
	}
	for _, h := range []string{b.PolicySHA256, b.PriceEvidenceSHA256, b.RequestEnvelopeSHA256} {
		if !comparisonDigest(h) {
			return fullWindowScope{}, fmt.Errorf("missing frozen evidence digest")
		}
	}
	if !windowActualRevision(b.ExecutorRevision) || !windowActualRevision(b.CustomerRevision) {
		return fullWindowScope{}, fmt.Errorf("invalid frozen revision")
	}
	allCaps := append(windowV10PrefixCaps(), caps...)
	old, err := prepareFullWindowScopeV9(s, p, allCaps, windowV10OriginalBindings(), windowV9BaselineSHA)
	if err != nil {
		return fullWindowScope{}, err
	}
	raw, err := json.Marshal(old)
	if err != nil || conversationHash(raw) != windowV10OriginalScopeSHA {
		return fullWindowScope{}, fmt.Errorf("v10 original R5 scope reconstruction mismatch")
	}
	old.Schema = "full-window-scope.v10"
	old.BaselineEvidenceSHA256 = baselineSHA
	old.Bindings = b
	old.Windows = append([]fullWindowDeclaration(nil), old.Windows[55:]...)
	budget, retry, completion := expectedWindowV10BudgetDeclaration(), expectedWindowV10RetryDeclaration(), expectedWindowCompletionDeclarationV10()
	old.BudgetDeclaration = &budget
	old.RetryDeclaration = &retry
	old.Completion = &completion
	old.BudgetDeclarationSHA256 = windowV8DeclarationSHA(budget)
	old.RetryPolicySHA256 = windowV8DeclarationSHA(retry)
	if err = validateWindowV10ExecutionShape(old); err != nil {
		return fullWindowScope{}, err
	}
	return old, nil
}

func validateWindowV10Declarations(scope fullWindowScope) error {
	budget, retry, completion := expectedWindowV10BudgetDeclaration(), expectedWindowV10RetryDeclaration(), expectedWindowCompletionDeclarationV10()
	if scope.BaselineEvidenceSHA256 != windowV10BaselineSHA || scope.BudgetApprovalSHA256 != "" || scope.AllowPaid || scope.ReadyForPaidRun || !reflect.DeepEqual(scope.Completion, &completion) || !reflect.DeepEqual(scope.BudgetDeclaration, &budget) || !reflect.DeepEqual(scope.RetryDeclaration, &retry) || scope.BudgetDeclarationSHA256 != windowV8DeclarationSHA(budget) || scope.RetryPolicySHA256 != windowV8DeclarationSHA(retry) {
		return fmt.Errorf("v10 fixed completion candidate declarations required; no human authority")
	}
	return nil
}

// Shape helpers are pure data checks, never permit issuers. The full scope
// validator separately rederives source/profile/bindings from the frozen inputs.
func validateWindowV10ExecutionShape(scope fullWindowScope) error {
	if scope.Schema != "full-window-scope.v10" {
		return fmt.Errorf("v10 scope required")
	}
	if err := validateWindowV10Declarations(scope); err != nil {
		return err
	}
	slots, err := json.Marshal(scope.Slots)
	if err != nil || len(scope.Slots) != 3660 || scope.ScheduleSHA256 != windowV10ScheduleSHA || conversationHash(slots) != windowV10ScheduleSHA || scope.Width != 12 || !scope.OddPassFirstArmImbalance {
		return fmt.Errorf("v10 fixed full original schedule required")
	}
	windows, err := json.Marshal(scope.Windows)
	if err != nil || len(scope.Windows) != 456 || conversationHash(windows) != windowV10SuffixWindowsSHA {
		return fmt.Errorf("v10 fixed exact suffix declarations required")
	}
	return nil
}

// Local index 1 means the copied declaration window-056, not a prefix slot.
func executionWindowAt(scope fullWindowScope, localIndex int) (fullWindowDeclaration, error) {
	if scope.Schema == "full-window-scope.v10" {
		if err := validateWindowV10ExecutionShape(scope); err != nil {
			return fullWindowDeclaration{}, err
		}
	} else if !legacyExecutionScope(scope) {
		return fullWindowDeclaration{}, fmt.Errorf("unsupported execution scope")
	}
	if localIndex < 1 || localIndex > len(scope.Windows) {
		return fullWindowDeclaration{}, fmt.Errorf("execution local index out of range")
	}
	return scope.Windows[localIndex-1], nil
}

// V10 returns only suffix totals. The original 1851 boundaries remain the
// separate Completion.Boundaries denominator, not an invented suffix partition.
func executionTotals(scope fullWindowScope) (fullWindowTotals, error) {
	if scope.Schema == "full-window-scope.v10" {
		if err := validateWindowV10ExecutionShape(scope); err != nil {
			return fullWindowTotals{}, err
		}
		return fullWindowTotals{3275, 6550, 10504, 0, 110191257, 1818872}, nil
	}
	if !legacyExecutionScope(scope) {
		return fullWindowTotals{}, fmt.Errorf("unsupported execution scope")
	}
	out := fullWindowTotals{Pairs: len(scope.Slots), Arms: 2 * len(scope.Slots), Boundaries: 1851}
	for _, s := range scope.Slots {
		out.DefinedUserTurns += 2 * s.UserTurns
	}
	for _, w := range scope.Windows {
		out.ModelCap += w.ModelCap
		out.JEVCap += w.JEVCap
	}
	return out, nil
}
func legacyExecutionScope(scope fullWindowScope) bool {
	if scope.Completion != nil {
		return false
	}
	switch scope.Schema {
	case "full-window-scope.v1", "full-window-scope.v2", "full-window-scope.v3", "full-window-scope.v4", "full-window-scope.v5", "full-window-scope.v6", "full-window-scope.v7", "full-window-scope.v8", "full-window-scope.v9":
		return true
	}
	return false
}
