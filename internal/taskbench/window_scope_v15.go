package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const windowV15BaselineSHA = "fec84f82af6951488d0886b42855f63ea2614d951f4c9f429836638933ea795c"

func expectedWindowV15RetryDeclaration() windowV8RetryDeclaration {
	return expectedWindowV14RetryDeclaration()
}

func expectedWindowV15BudgetDeclaration() windowV8BudgetDeclaration {
	d := expectedWindowV14BudgetDeclaration()
	d.BaselineSHA256 = windowV15BaselineSHA
	d.Parent = windowV8Amounts{200000000, 3000000}
	d.Historical = windowV8Amounts{85219758, 936184}
	d.MaximumReserved = windowV8Amounts{198929479, 2808881}
	d.Remaining = windowV8Amounts{1070521, 191119}
	return d
}

func hasWindowV15Fields(s fullWindowScope) bool {
	return s.CompletionV15 != nil
}

// Pure candidate construction cannot create a policy approval or paid role.
func prepareFullWindowScopeV15(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if baselineSHA != windowV15BaselineSHA {
		return fullWindowScope{}, fmt.Errorf("v15 exact concrete178 required")
	}
	out, err := prepareFullWindowScopeV14(s, p, caps, b, windowV14BaselineSHA)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v15"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.CompletionV14 = nil
	c, d, r, op := expectedWindowCompletionDeclarationV15(), expectedWindowV15BudgetDeclaration(), expectedWindowV15RetryDeclaration(), expectedWindowV14OperationPolicy()
	out.CompletionV15, out.BudgetDeclaration, out.RetryDeclaration, out.OperationPolicyV14 = &c, &d, &r, &op
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(d)
	out.RetryPolicySHA256 = windowV8DeclarationSHA(r)
	return out, validateWindowV15ExecutionShape(out)
}

func validateWindowV15Declarations(s fullWindowScope) error {
	c, d, r, op := expectedWindowCompletionDeclarationV15(), expectedWindowV15BudgetDeclaration(), expectedWindowV15RetryDeclaration(), expectedWindowV14OperationPolicy()
	if s.Schema != "full-window-scope.v15" || s.Completion != nil || s.CompletionV12 != nil || s.CompletionV13 != nil || s.CompletionV14 != nil || s.CollectionPolicySHA256 != collectionPreviewFailurePolicySHA() || s.Bindings.PolicySHA256 != windowOwnedPreviewFailurePolicySHA() || s.BaselineEvidenceSHA256 != windowV15BaselineSHA || s.AllowPaid || s.ReadyForPaidRun || s.BudgetApprovalSHA256 != "" || !reflect.DeepEqual(s.CompletionV15, &c) || !reflect.DeepEqual(s.OperationPolicyV14, &op) || !reflect.DeepEqual(s.BudgetDeclaration, &d) || !reflect.DeepEqual(s.RetryDeclaration, &r) || s.BudgetDeclarationSHA256 != windowV8DeclarationSHA(d) || s.RetryPolicySHA256 != windowV8DeclarationSHA(r) {
		return fmt.Errorf("v15 fixed study and operation-policy DATA required; no execution authority")
	}
	if s.Observation != nil {
		return s.Observation.validate()
	}
	return nil
}

func validateWindowV15ExecutionShape(s fullWindowScope) error {
	if err := validateWindowV15Declarations(s); err != nil {
		return err
	}
	raw, err := json.Marshal(s.Slots)
	if err != nil || len(s.Slots) != 3660 || s.Width != 12 || !s.OddPassFirstArmImbalance || s.ScheduleSHA256 != windowV10ScheduleSHA || conversationHash(raw) != windowV10ScheduleSHA {
		return fmt.Errorf("v15 original full schedule required")
	}
	raw, err = json.Marshal(s.Windows)
	if err != nil || len(s.Windows) != 456 || conversationHash(raw) != windowV10SuffixWindowsSHA {
		return fmt.Errorf("v15 original056..511 suffix required")
	}
	return nil
}
