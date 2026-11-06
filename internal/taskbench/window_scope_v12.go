package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// The exact original R3 inventory is historical DATA, not an execution permit.
const windowV12BaselineSHA = "2ff9ee20cbd11e51b5b31a63b9bf31f53625f602194b5bb0d123bff4a462df9c"

func expectedWindowV12RetryDeclaration() windowV8RetryDeclaration {
	return expectedWindowV10RetryDeclaration()
}
func expectedWindowV12BudgetDeclaration() windowV8BudgetDeclaration {
	d := expectedWindowV10BudgetDeclaration()
	d.BaselineSHA256 = windowV12BaselineSHA
	d.Parent = windowV8Amounts{195000000, 3000000}
	d.Historical = windowV8Amounts{79620965, 838363}
	d.MaximumReserved = windowV8Amounts{193330686, 2711060}
	d.Remaining = windowV8Amounts{1669314, 288940}
	return d
}

// Pure preparation deliberately has no observation argument or authority. The
// actual V12 freeze must independently bind durable observation and typed154.
func prepareFullWindowScopeV12(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV12BaselineSHA {
		return fullWindowScope{}, fmt.Errorf("v12 exact concrete154 required")
	}
	out, e := prepareFullWindowScopeV10(s, p, caps, b, windowV10BaselineSHA)
	if e != nil {
		return fullWindowScope{}, e
	}
	out.Schema = "full-window-scope.v12"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.Completion = nil
	c, d, r := expectedWindowCompletionDeclarationV12(), expectedWindowV12BudgetDeclaration(), expectedWindowV12RetryDeclaration()
	out.CompletionV12 = &c
	out.BudgetDeclaration = &d
	out.RetryDeclaration = &r
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(d)
	out.RetryPolicySHA256 = windowV8DeclarationSHA(r)
	return out, validateWindowV12ExecutionShape(out)
}

func validateWindowV12Declarations(s fullWindowScope) error {
	if hasWindowV14Fields(s) {
		return fmt.Errorf("v14-only fields rejected by legacy declarations")
	}
	c, d, r := expectedWindowCompletionDeclarationV12(), expectedWindowV12BudgetDeclaration(), expectedWindowV12RetryDeclaration()
	if s.Schema != "full-window-scope.v12" || s.CompletionV13 != nil || s.CollectionPolicySHA256 != "" || s.Completion != nil || s.BaselineEvidenceSHA256 != windowV12BaselineSHA || s.AllowPaid || s.ReadyForPaidRun || s.BudgetApprovalSHA256 != "" || !reflect.DeepEqual(s.CompletionV12, &c) || !reflect.DeepEqual(s.BudgetDeclaration, &d) || !reflect.DeepEqual(s.RetryDeclaration, &r) || s.BudgetDeclarationSHA256 != windowV8DeclarationSHA(d) || s.RetryPolicySHA256 != windowV8DeclarationSHA(r) {
		return fmt.Errorf("v12 fixed completion candidate declarations required; no human authority")
	}
	// Observation is absent in pure preparation. If separately bound, it must
	// retain the complete existing durable-local identity contract.
	if s.Observation != nil {
		if e := s.Observation.validate(); e != nil {
			return e
		}
	}
	return nil
}

// This checks immutable tuple/order/cap data only. It cannot issue a permit or
// prove historical delivery, source equivalence or durable observation.
func validateWindowV12ExecutionShape(s fullWindowScope) error {
	if e := validateWindowV12Declarations(s); e != nil {
		return e
	}
	raw, e := json.Marshal(s.Slots)
	if e != nil || len(s.Slots) != 3660 || s.Width != 12 || !s.OddPassFirstArmImbalance || s.ScheduleSHA256 != windowV10ScheduleSHA || conversationHash(raw) != windowV10ScheduleSHA {
		return fmt.Errorf("v12 original full schedule required")
	}
	raw, e = json.Marshal(s.Windows)
	if e != nil || len(s.Windows) != 456 || conversationHash(raw) != windowV10SuffixWindowsSHA {
		return fmt.Errorf("v12 exact original056..511 suffix required")
	}
	return nil
}
