package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// The exact stopped Root4 inventory is historical DATA, not an execution permit.
const windowV13BaselineSHA = "5a3e76d6de7ecf9923563f59e67a29df4a03e1015c47fa01121bdb58a2dc4cc5"

func expectedWindowV13RetryDeclaration() windowV8RetryDeclaration {
	return expectedWindowV10RetryDeclaration()
}
func expectedWindowV13BudgetDeclaration() windowV8BudgetDeclaration {
	d := expectedWindowV10BudgetDeclaration()
	d.BaselineSHA256 = windowV13BaselineSHA
	d.Parent = windowV8Amounts{195000000, 3000000}
	d.Historical = windowV8Amounts{79974331, 845904}
	d.MaximumReserved = windowV8Amounts{193684052, 2718601}
	d.Remaining = windowV8Amounts{1315948, 281399}
	return d
}

// Pure preparation deliberately has no observation argument or authority. The
// actual V13 freeze must independently bind durable observation and typed156.
func prepareFullWindowScopeV13(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV13BaselineSHA {
		return fullWindowScope{}, fmt.Errorf("v13 exact concrete156 required")
	}
	out, e := prepareFullWindowScopeV10(s, p, caps, b, windowV10BaselineSHA)
	if e != nil {
		return fullWindowScope{}, e
	}
	out.Schema = "full-window-scope.v13"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.Completion = nil
	c, d, r := expectedWindowCompletionDeclarationV13(), expectedWindowV13BudgetDeclaration(), expectedWindowV13RetryDeclaration()
	out.CompletionV13 = &c
	out.CollectionPolicySHA256 = collectionPreviewFailurePolicySHA()
	out.BudgetDeclaration = &d
	out.RetryDeclaration = &r
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(d)
	out.RetryPolicySHA256 = windowV8DeclarationSHA(r)
	return out, validateWindowV13ExecutionShape(out)
}

func validateWindowV13Declarations(s fullWindowScope) error {
	c, d, r := expectedWindowCompletionDeclarationV13(), expectedWindowV13BudgetDeclaration(), expectedWindowV13RetryDeclaration()
	if s.Schema != "full-window-scope.v13" || s.Completion != nil || s.CompletionV12 != nil || s.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || s.Bindings.PolicySHA256 != windowOwnedPreviewFailurePolicySHA() || s.BaselineEvidenceSHA256 != windowV13BaselineSHA || s.AllowPaid || s.ReadyForPaidRun || s.BudgetApprovalSHA256 != "" || !reflect.DeepEqual(s.CompletionV13, &c) || !reflect.DeepEqual(s.BudgetDeclaration, &d) || !reflect.DeepEqual(s.RetryDeclaration, &r) || s.BudgetDeclarationSHA256 != windowV8DeclarationSHA(d) || s.RetryPolicySHA256 != windowV8DeclarationSHA(r) {
		return fmt.Errorf("v13 fixed completion candidate declarations required; no human authority")
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
func validateWindowV13ExecutionShape(s fullWindowScope) error {
	if e := validateWindowV13Declarations(s); e != nil {
		return e
	}
	raw, e := json.Marshal(s.Slots)
	if e != nil || len(s.Slots) != 3660 || s.Width != 12 || !s.OddPassFirstArmImbalance || s.ScheduleSHA256 != windowV10ScheduleSHA || conversationHash(raw) != windowV10ScheduleSHA {
		return fmt.Errorf("v13 original full schedule required")
	}
	raw, e = json.Marshal(s.Windows)
	if e != nil || len(s.Windows) != 456 || conversationHash(raw) != windowV10SuffixWindowsSHA {
		return fmt.Errorf("v13 exact original056..511 suffix required")
	}
	return nil
}
