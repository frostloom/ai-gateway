package taskbench

import (
	"encoding/json"
	"fmt"
)

const windowV6BaselineSHA = "b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1"
const windowV6CapsSHA = "30c8151031f40cb05562b255b789b9fb021a93467288e677c4eb39d87806113d"
const windowBudget130ApprovalSHA = "ef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce"

func validateWindowV6Declaration(width int, caps []fullWindowCaps, baselineSHA, approvalSHA string) error {
	if width != 28 || len(caps) != 131 || baselineSHA != windowV6BaselineSHA || approvalSHA != windowBudget130ApprovalSHA {
		return fmt.Errorf("v6 fixed width, caps and historical budget bindings required")
	}
	var model, jev int64
	for _, c := range caps {
		if c.ModelCap < 74752 || c.JEVCap < 2688 || c.ModelCap > 85015146-model || c.JEVCap > 655442-jev {
			return fmt.Errorf("v6 caps exceed remaining currencies after risk or minimum hold")
		}
		model += c.ModelCap
		jev += c.JEVCap
	}
	raw, err := json.Marshal(caps)
	if err != nil || model != 85000000 || jev != 600000 || conversationHash(raw) != windowV6CapsSHA {
		return fmt.Errorf("v6 fixed cap vector changed")
	}
	return nil
}
func prepareFullWindowScopeV6(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA, approvalSHA string) (fullWindowScope, error) {
	if err := validateWindowV6Declaration(width, caps, baselineSHA, approvalSHA); err != nil {
		return fullWindowScope{}, err
	}
	out, err := prepareFullWindowScopeWithBounds(s, p, width, caps, b, 85015146, 655442)
	if err != nil {
		return fullWindowScope{}, err
	}
	out.Schema = "full-window-scope.v6"
	out.BaselineEvidenceSHA256 = baselineSHA
	out.BudgetApprovalSHA256 = approvalSHA
	return out, nil
}
