package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

func expectedStoppedV2WindowBaseline() fullWindowBaseline {
	b := expectedStoppedWindowBaseline()
	for i := 1; i <= 6; i++ {
		b.ClaimSHA256[fmt.Sprintf("full1220-v2-20261028/window-%06d", i)] = windowStoppedV2ArtifactPins()[fmt.Sprintf("campaign/claim-%06d.json", i)]
	}
	b.TotalModel, b.TotalJEV = 105000000, 1000000
	b.PermanentModel, b.PermanentJEV = 33395934, 259511
	b.RemainingModel, b.RemainingJEV = 71604066, 740489
	b.ActualModelCalls, b.ActualJEVCalls = 809, 204
	return b
}

type windowStoppedV3History interface {
	windowStoppedHistory
	windowStoppedPredecessorRoots() [2]string
}

func (s windowStoppedV2Baseline) windowStoppedPredecessorRoots() [2]string {
	return [2]string{s.first.originalRoot, s.originalRoot}
}

func validateStoppedV3CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	h, ok := p.stoppedHistory.(windowStoppedV3History)
	if p.scope.Schema != "full-window-scope.v3" || p.scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || !ok || !reflect.DeepEqual(base, expectedStoppedV2WindowBaseline()) {
		return fmt.Errorf("verified v3 sixteen-claim baseline required")
	}
	fresh, sha, second, err := h.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, base) || !comparisonDigest(sha) || sha != p.scope.BaselineEvidenceSHA256 {
		return fmt.Errorf("v3 historical evidence changed")
	}
	roots := h.windowStoppedPredecessorRoots()
	if second != roots[1] || !windowPayloadDisjoint(roots[0], roots[1]) {
		return fmt.Errorf("v3 predecessor roots crossed")
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v3 historical root overlap")
		}
	}
	return nil
}

// Actual construction cannot accept a caller-supplied interface/JSON verifier.
func newWindowCampaignV3(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits, stopped windowStoppedV2Baseline) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v3" || scope.BaselineEvidenceSHA256 != stopped.evidenceSHA256 || scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA {
		return nil, fmt.Errorf("v3 campaign version or budget binding rejected")
	}
	p, err := prepareWindowCampaign(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
	if err != nil {
		return nil, err
	}
	base, _, _, err := stopped.revalidateWindowStoppedHistory()
	if err != nil {
		return nil, err
	}
	p.stoppedHistory = stopped
	c, err := startWindowCampaign(p, base)
	if err != nil {
		return nil, err
	}
	c.historicalDir = stopped.historicalDir
	return c, nil
}
