package taskbench

import (
	"encoding/json"
	"fmt"
)

const windowV7BaselineSHA = "ccf528106789daf05a61c8d8f4e5f72d3a29eea65d78a6f81afb85f4e1ea45ad"
const windowV7CapsSHA = "7b4078f89969bb46797217ea677a01cfec9adf51257437f603455a684368029d"

func validateWindowV7Declaration(width int, caps []fullWindowCaps, baselineSHA, approvalSHA string) error {
	if width != 28 || len(caps) != 131 || baselineSHA != windowV7BaselineSHA || approvalSHA != windowBudget130ApprovalSHA {
		return fmt.Errorf("v7 fixed width, caps and historical budget bindings required")
	}
	var model, jev int64
	for _, c := range caps {
		if c.ModelCap < 74752 || c.JEVCap < 2688 || c.ModelCap > 84510611-model || c.JEVCap > 650855-jev {
			return fmt.Errorf("v7 caps exceed remaining currencies after risk or minimum hold")
		}
		model += c.ModelCap
		jev += c.JEVCap
	}
	raw, err := json.Marshal(caps)
	if err != nil || model != 84500000 || jev != 600000 || conversationHash(raw) != windowV7CapsSHA {
		return fmt.Errorf("v7 fixed cap vector changed")
	}
	return nil
}
func prepareFullWindowScopeV7(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA, approvalSHA string) (fullWindowScope, error) {
	if err := validateWindowV7Declaration(width, caps, baselineSHA, approvalSHA); err != nil {
		return fullWindowScope{}, err
	}
	out, err := prepareFullWindowScopeWithBounds(s, p, width, caps, b, 84510611, 650855)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v7"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.BudgetApprovalSHA256 = approvalSHA
	return out, nil
}
