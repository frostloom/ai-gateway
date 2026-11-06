package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const windowV14BaselineSHA = "3d5f19325c69165c603a070518d2a4b25faa43a1845323d494866486abe7230f"

func expectedWindowV14RetryDeclaration() windowV8RetryDeclaration {
	return expectedWindowV13RetryDeclaration()
}

func expectedWindowV14BudgetDeclaration() windowV8BudgetDeclaration {
	d := expectedWindowV13BudgetDeclaration()
	d.BaselineSHA256 = windowV14BaselineSHA
	d.Parent = windowV8Amounts{200000000, 3000000}
	d.Historical = windowV8Amounts{85037976, 932373}
	d.MaximumReserved = windowV8Amounts{198747697, 2805070}
	d.Remaining = windowV8Amounts{1252303, 194930}
	return d
}

func hasWindowV14Fields(s fullWindowScope) bool {
	return s.CompletionV14 != nil || s.OperationPolicyV14 != nil
}

// Pure candidate construction cannot create a policy approval or paid role.
func prepareFullWindowScopeV14(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV14BaselineSHA {
		return fullWindowScope{}, fmt.Errorf("v14 exact concrete177 required")
	}
	out, err := prepareFullWindowScopeV13(s, p, caps, b, windowV13BaselineSHA)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v14"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.CompletionV13 = nil
	c, d, r, op := expectedWindowCompletionDeclarationV14(), expectedWindowV14BudgetDeclaration(), expectedWindowV14RetryDeclaration(), expectedWindowV14OperationPolicy()
	out.CompletionV14, out.BudgetDeclaration, out.RetryDeclaration, out.OperationPolicyV14 = &c, &d, &r, &op
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(d)
	out.RetryPolicySHA256 = windowV8DeclarationSHA(r)
	return out, validateWindowV14ExecutionShape(out)
}

func validateWindowV14Declarations(s fullWindowScope) error {
	c, d, r, op := expectedWindowCompletionDeclarationV14(), expectedWindowV14BudgetDeclaration(), expectedWindowV14RetryDeclaration(), expectedWindowV14OperationPolicy()
	if s.Schema != "full-window-scope.v14" || s.Completion != nil || s.CompletionV12 != nil || s.CompletionV13 != nil || s.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || s.Bindings.PolicySHA256 != windowOwnedPreviewFailurePolicySHA() || s.BaselineEvidenceSHA256 != windowV14BaselineSHA || s.AllowPaid || s.ReadyForPaidRun || s.BudgetApprovalSHA256 != "" || !reflect.DeepEqual(s.CompletionV14, &c) || !reflect.DeepEqual(s.OperationPolicyV14, &op) || !reflect.DeepEqual(s.BudgetDeclaration, &d) || !reflect.DeepEqual(s.RetryDeclaration, &r) || s.BudgetDeclarationSHA256 != windowV8DeclarationSHA(d) || s.RetryPolicySHA256 != windowV8DeclarationSHA(r) {
		return fmt.Errorf("v14 fixed study and operation-policy DATA required; no execution authority")
	}
	if s.Observation != nil {
		return s.Observation.validate()
	}
	return nil
}

func validateWindowV14ExecutionShape(s fullWindowScope) error {
	if err := validateWindowV14Declarations(s); err != nil {
		return err
	}
	raw, err := json.Marshal(s.Slots)
	if err != nil || len(s.Slots) != 3660 || s.Width != 12 || !s.OddPassFirstArmImbalance || s.ScheduleSHA256 != windowV10ScheduleSHA || conversationHash(raw) != windowV10ScheduleSHA {
		return fmt.Errorf("v14 original full schedule required")
	}
	raw, err = json.Marshal(s.Windows)
	if err != nil || len(s.Windows) != 456 || conversationHash(raw) != windowV10SuffixWindowsSHA {
		return fmt.Errorf("v14 original056..511 suffix required")
	}
	return nil
}
