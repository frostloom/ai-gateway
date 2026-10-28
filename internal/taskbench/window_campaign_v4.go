package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

type windowStoppedV4History interface {
	windowStoppedHistory
	windowStoppedPredecessorRootsV4() [3]string
}

func windowV4ExpectedLimits() windowCampaignLimits {
	return windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxRequestBytes: 32768, MaxInput: 36864, MaxOutput: 128, InputRate: 2000000, OutputRate: 8000000, CacheHitInputRate: 40000}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0", MaxRequestBytes: 4096, MaxInput: 64000, MaxOutput: 1000000, InputRate: 42000}}
}
func validateWindowCampaignEpochLimits(scope fullWindowScope, limits windowCampaignLimits) error {
	if scope.Schema == "full-window-scope.v4" {
		if limits != windowV4ExpectedLimits() {
			return fmt.Errorf("v4 exact cache tariff and original envelopes required")
		}
	} else if limits.Model.CacheHitInputRate != 0 || limits.JEV.CacheHitInputRate != 0 {
		return fmt.Errorf("legacy campaign cache rate rejected")
	}
	return nil
}
func validateStoppedV4CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	h, ok := p.stoppedHistory.(windowStoppedV4History)
	caps := make([]fullWindowCaps, len(p.scope.Windows))
	for i, w := range p.scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if p.scope.Schema != "full-window-scope.v4" || !ok || !reflect.DeepEqual(base, expectedStoppedV3WindowBaseline()) {
		return fmt.Errorf("verified v4 twenty-five-claim baseline required")
	}
	if err := validateWindowV4Declaration(p.scope.Width, caps, p.scope.BaselineEvidenceSHA256, p.scope.BudgetApprovalSHA256); err != nil {
		return err
	}
	if err := validateWindowCampaignEpochLimits(p.scope, p.limits); err != nil {
		return err
	}
	fresh, sha, third, err := h.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, base) || sha != windowV4BaselineSHA {
		return fmt.Errorf("v4 historical evidence changed")
	}
	roots := h.windowStoppedPredecessorRootsV4()
	if third != roots[2] {
		return fmt.Errorf("v4 final predecessor root changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v4 historical root overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v4 predecessor roots crossed")
			}
		}
	}
	return nil
}

// Only the concrete fresh twenty-five-claim proof can reach this constructor.
// It supplies no provider, actual-ready capability or caller verifier hook.
func newWindowCampaignV4(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits, stopped windowStoppedV3Baseline) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v4" || scope.BaselineEvidenceSHA256 != windowV4BaselineSHA || scope.BaselineEvidenceSHA256 != stopped.evidenceSHA256 || scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA {
		return nil, fmt.Errorf("v4 concrete campaign historical/budget binding rejected")
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
