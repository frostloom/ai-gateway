package taskbench

import (
	"encoding/json"
	"fmt"
)

const windowV4BaselineSHA = "12a9d584ea9508a9fc7701532ad9214b0c743a8e13d17829432fac0d6b48a20c"
const windowV4CapsSHA = "0c020b80cf65c9876025d72ef8ad68bcfdea6c77b429b0390f2aabde895ca025"

// This is only a fixed offline declaration. Actual execution requires a new
// entry epoch, independent binary attestation and explicit frozen authorization.
func prepareFullWindowScopeV4(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA, approvalSHA string) (fullWindowScope, error) {
	if err := validateWindowV4Declaration(width, caps, baselineSHA, approvalSHA); err != nil {
		return fullWindowScope{}, err
	}
	out, err := prepareFullWindowScope(s, p, width, caps, b)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v4"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.BudgetApprovalSHA256 = approvalSHA
	return out, nil
}
func validateWindowV4Declaration(width int, caps []fullWindowCaps, baselineSHA, approvalSHA string) error {
	if width != 28 || len(caps) != 131 || baselineSHA != windowV4BaselineSHA || approvalSHA != windowBudget105ApprovalSHA {
		return fmt.Errorf("v4 fixed width, caps and historical budget bindings required")
	}
	var model, jev int64
	for _, c := range caps {
		if c.ModelCap < 74752 || c.JEVCap < 2688 || c.ModelCap > 66111830-windowV3RiskModel-model || c.JEVCap > 691997-windowV3RiskJEV-jev {
			return fmt.Errorf("v4 caps exceed remaining currencies after risk or minimum hold")
		}
		model += c.ModelCap
		jev += c.JEVCap
	}
	raw, err := json.Marshal(caps)
	if err != nil || model != 64000000 || jev != 690000 || conversationHash(raw) != windowV4CapsSHA {
		return fmt.Errorf("v4 fixed largest-remainder cap vector changed")
	}
	return nil
}
