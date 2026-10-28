package taskbench

import "fmt"

const (
	windowBudget105ApprovalSHA       = "d5062e6f39f9ae7c9a8461121f9ba907c798100d7a5428af72da459595d2d554"
	windowV3RemainingModel     int64 = 71604066
	windowV3RemainingJEV       int64 = 740489
	// Additional configured liability margin, never an executable claim, a
	// settlement of the old unknown holds, or a guarantee of provider bills.
	windowV3RiskModel int64 = 2023424
	windowV3RiskJEV   int64 = 65
)

// Offline declaration only. Neither a digest nor this result supplies the
// concrete sixteen-claim proof required by the separately gated actual entry.
func prepareFullWindowScopeV3(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA, approvalSHA string) (fullWindowScope, error) {
	if !comparisonDigest(baselineSHA) || approvalSHA != windowBudget105ApprovalSHA {
		return fullWindowScope{}, fmt.Errorf("v3 stopped baseline and exact budget approval required")
	}
	out, err := prepareFullWindowScope(s, p, width, caps, b)
	if err != nil {
		return fullWindowScope{}, err
	}
	var model, jev int64
	for _, cap := range caps {
		if cap.ModelCap > windowV3RemainingModel-windowV3RiskModel-model || cap.JEVCap > windowV3RemainingJEV-windowV3RiskJEV-jev {
			return fullWindowScope{}, fmt.Errorf("v3 scope exceeds remaining separate currencies after liability margin")
		}
		model += cap.ModelCap
		jev += cap.JEVCap
	}
	out.Schema, out.BaselineEvidenceSHA256, out.BudgetApprovalSHA256 = "full-window-scope.v3", baselineSHA, approvalSHA
	return out, nil
}
