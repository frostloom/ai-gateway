package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

type windowStoppedV6History interface {
	windowStoppedHistory
	windowStoppedPredecessorRootsV6() [5]string
}

func validateWindowCampaignV6OrHistoricalLimits(scope fullWindowScope, limits windowCampaignLimits) error {
	if scope.Schema == "full-window-scope.v6" {
		if limits != windowV4ExpectedLimits() {
			return fmt.Errorf("v6 exact cache tariff and original envelopes required")
		}
		return nil
	}
	return validateWindowCampaignV5OrHistoricalLimits(scope, limits)
}
func validateStoppedV6CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	h, ok := p.stoppedHistory.(windowStoppedV6History)
	caps := make([]fullWindowCaps, len(p.scope.Windows))
	for i, w := range p.scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if p.scope.Schema != "full-window-scope.v6" || !ok || !reflect.DeepEqual(base, expectedCurrent130WindowBaseline()) {
		return fmt.Errorf("verified current 130 thirty-two-claim baseline required")
	}
	if err := validateWindowV6Declaration(p.scope.Width, caps, p.scope.BaselineEvidenceSHA256, p.scope.BudgetApprovalSHA256); err != nil {
		return err
	}
	if err := validateWindowCampaignV6OrHistoricalLimits(p.scope, p.limits); err != nil {
		return err
	}
	fresh, sha, last, err := h.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, base) || sha != windowV6BaselineSHA {
		return fmt.Errorf("v6 history or approval changed")
	}
	roots := h.windowStoppedPredecessorRootsV6()
	if last != roots[4] {
		return fmt.Errorf("v6 final predecessor root changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v6 historical root overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v6 predecessor roots crossed")
			}
		}
	}
	return nil
}
func newWindowCampaignV6(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits, stopped windowStoppedV5Baseline, approval windowActualFile) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v6" || scope.BaselineEvidenceSHA256 != windowV6BaselineSHA || stopped.evidenceSHA256 != windowV6BaselineSHA || scope.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || approval.SHA256 != windowBudget130ApprovalSHA {
		return nil, fmt.Errorf("v6 concrete historical/budget binding rejected")
	}
	p, err := prepareWindowCampaign(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
	if err != nil {
		return nil, err
	}
	h := windowCurrent130History{stopped: stopped, approval: approval}
	base, _, _, err := h.revalidateWindowStoppedHistory()
	if err != nil {
		return nil, err
	}
	p.stoppedHistory = h
	c, err := startWindowCampaign(p, base)
	if err != nil {
		return nil, err
	}
	c.historicalDir = stopped.historicalDir
	return c, nil
}
