package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

type windowStoppedV5History interface {
	windowStoppedHistory
	windowStoppedPredecessorRootsV5() [4]string
}

// Explicit new epoch only; historical validation remains in its original helper.
func validateWindowCampaignV5OrHistoricalLimits(scope fullWindowScope, limits windowCampaignLimits) error {
	if scope.Schema == "full-window-scope.v5" {
		if limits != windowV4ExpectedLimits() {
			return fmt.Errorf("v5 exact cache tariff and original envelopes required")
		}
		return nil
	}
	return validateWindowCampaignEpochLimits(scope, limits)
}
func validateStoppedV5CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	h, ok := p.stoppedHistory.(windowStoppedV5History)
	caps := make([]fullWindowCaps, len(p.scope.Windows))
	for i, w := range p.scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if p.scope.Schema != "full-window-scope.v5" || !ok || !reflect.DeepEqual(base, expectedStoppedV4WindowBaseline()) {
		return fmt.Errorf("verified v5 twenty-eight-claim baseline required")
	}
	if err := validateWindowV5Declaration(p.scope.Width, caps, p.scope.BaselineEvidenceSHA256, p.scope.BudgetApprovalSHA256); err != nil {
		return err
	}
	if err := validateWindowCampaignV5OrHistoricalLimits(p.scope, p.limits); err != nil {
		return err
	}
	fresh, sha, fourth, err := h.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, base) || sha != windowV5BaselineSHA {
		return fmt.Errorf("v5 historical evidence changed")
	}
	roots := h.windowStoppedPredecessorRootsV5()
	if fourth != roots[3] {
		return fmt.Errorf("v5 final predecessor root changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v5 historical root overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v5 predecessor roots crossed")
			}
		}
	}
	return nil
}

// Only the concrete fresh twenty-eight-claim proof can reach this constructor.
// It supplies no provider, actual-ready capability or caller verifier hook.
func newWindowCampaignV5(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits, stopped windowStoppedV4Baseline) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v5" || scope.BaselineEvidenceSHA256 != windowV5BaselineSHA || scope.BaselineEvidenceSHA256 != stopped.evidenceSHA256 || scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA {
		return nil, fmt.Errorf("v5 concrete campaign historical/budget binding rejected")
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
