package taskbench

import (
	"encoding/json"
	"fmt"
)

const windowV5BaselineSHA = "2f8c83546a2918b85801160f521465c2f53e040073b3590e1ecde53f278ef340"
const windowV5CapsSHA = "7fe19887167feaa4ce85520f2717cf82d0143cd5e2eef6577b3b07afcfe04862"

// This is only a fixed offline declaration. Actual execution requires a new
// entry epoch, independent binary attestation and explicit frozen authorization.
func prepareFullWindowScopeV5(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA, approvalSHA string) (fullWindowScope, error) {
	if err := validateWindowV5Declaration(width, caps, baselineSHA, approvalSHA); err != nil {
		return fullWindowScope{}, err
	}
	out, err := prepareFullWindowScope(s, p, width, caps, b)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v5"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.BudgetApprovalSHA256 = approvalSHA
	return out, nil
}
func validateWindowV5Declaration(width int, caps []fullWindowCaps, baselineSHA, approvalSHA string) error {
	if width != 28 || len(caps) != 131 || baselineSHA != windowV5BaselineSHA || approvalSHA != windowBudget105ApprovalSHA {
		return fmt.Errorf("v5 fixed width, caps and historical budget bindings required")
	}
	var model, jev int64
	for _, c := range caps {
		if c.ModelCap < 74752 || c.JEVCap < 2688 || c.ModelCap > 64374109-windowV3RiskModel-model || c.JEVCap > 676175-windowV3RiskJEV-jev {
			return fmt.Errorf("v5 caps exceed remaining currencies after risk or minimum hold")
		}
		model += c.ModelCap
		jev += c.JEVCap
	}
	raw, err := json.Marshal(caps)
	if err != nil || model != 62000000 || jev != 676000 || conversationHash(raw) != windowV5CapsSHA {
		return fmt.Errorf("v5 fixed largest-remainder cap vector changed")
	}
	return nil
}
