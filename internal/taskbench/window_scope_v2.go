package taskbench

import "fmt"

// Offline declaration only. This does not load evidence or authorize execution.
func prepareFullWindowScopeV2(s ConversationSuite, p PairedProfiles, width int, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string) (fullWindowScope, error) {
	if !comparisonDigest(baselineSHA) {
		return fullWindowScope{}, fmt.Errorf("stopped baseline evidence digest required")
	}
	out, err := prepareFullWindowScope(s, p, width, caps, b)
	if err != nil {
		return fullWindowScope{}, err
	}
	var model, jev int64
	for _, cap := range caps {
		if cap.ModelCap > 70524294-model || cap.JEVCap > 772817-jev {
			return fullWindowScope{}, fmt.Errorf("v2 scope exceeds new remaining separate currencies")
		}
		model += cap.ModelCap
		jev += cap.JEVCap
	}
	out.Schema, out.BaselineEvidenceSHA256 = "full-window-scope.v2", baselineSHA
	return out, nil
}
