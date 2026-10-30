package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

type windowStoppedV7History interface {
	windowStoppedHistory
	windowStoppedPredecessorRootsV7() [6]string
}

// Recheck the current envelope-selected approval around the immutable historical proof.
type windowV7CurrentHistory struct {
	stopped  windowStoppedV6Baseline
	approval windowActualFile
}

func (h windowV7CurrentHistory) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if h.approval.SHA256 != windowBudget130ApprovalSHA {
		return fullWindowBaseline{}, "", "", fmt.Errorf("v7 current approval binding changed")
	}
	if _, err := readWindowActualFile(h.approval.Path, h.approval.SHA256, 65536); err != nil {
		return fullWindowBaseline{}, "", "", fmt.Errorf("v7 current approval changed")
	}
	base, sha, root, err := h.stopped.revalidateWindowStoppedHistory()
	if err != nil || sha != windowV7BaselineSHA || !reflect.DeepEqual(base, expectedStoppedV6WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("v7 historical proof changed")
	}
	if _, err = readWindowActualFile(h.approval.Path, h.approval.SHA256, 65536); err != nil {
		return fullWindowBaseline{}, "", "", fmt.Errorf("v7 current approval changed")
	}
	return base, sha, root, nil
}
func (h windowV7CurrentHistory) windowStoppedPredecessorRootsV7() [6]string {
	return h.stopped.windowStoppedPredecessorRootsV7()
}

func validateWindowCampaignV7OrHistoricalLimits(scope fullWindowScope, limits windowCampaignLimits) error {
	if scope.Schema == "full-window-scope.v7" || windowScopeUsesNativeV8(scope.Schema) {
		if limits != windowV4ExpectedLimits() {
			return fmt.Errorf("v7 exact cache tariff and original envelopes required")
		}
		return nil
	}
	return validateWindowCampaignV6OrHistoricalLimits(scope, limits)
}
func validateStoppedV7CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	h, ok := p.stoppedHistory.(windowStoppedV7History)
	caps := make([]fullWindowCaps, len(p.scope.Windows))
	for i, w := range p.scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if p.scope.Schema != "full-window-scope.v7" || !ok || !reflect.DeepEqual(base, expectedStoppedV6WindowBaseline()) {
		return fmt.Errorf("verified current 130 thirty-three-claim baseline required")
	}
	if err := validateWindowV7Declaration(p.scope.Width, caps, p.scope.BaselineEvidenceSHA256, p.scope.BudgetApprovalSHA256); err != nil {
		return err
	}
	if err := validateWindowCampaignV7OrHistoricalLimits(p.scope, p.limits); err != nil {
		return err
	}
	fresh, sha, last, err := h.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, base) || sha != windowV7BaselineSHA {
		return fmt.Errorf("v7 history or approval changed")
	}
	roots := h.windowStoppedPredecessorRootsV7()
	if last != roots[5] {
		return fmt.Errorf("v7 final predecessor root changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v7 historical root overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v7 predecessor roots crossed")
			}
		}
	}
	return nil
}
func newWindowCampaignV7(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits, stopped windowStoppedV6Baseline, approval windowActualFile) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v7" || scope.BaselineEvidenceSHA256 != windowV7BaselineSHA || stopped.evidenceSHA256 != windowV7BaselineSHA || scope.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || approval.SHA256 != windowBudget130ApprovalSHA {
		return nil, fmt.Errorf("v7 concrete historical/budget binding rejected")
	}
	p, err := prepareWindowCampaign(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
	if err != nil {
		return nil, err
	}
	if _, err := readWindowActualFile(approval.Path, approval.SHA256, 65536); err != nil {
		return nil, err
	}
	h := windowV7CurrentHistory{stopped: stopped, approval: approval}
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
