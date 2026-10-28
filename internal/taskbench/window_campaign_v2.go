package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// Sealed within this package. Actual construction supplies only the concrete
// file-backed stopped proof; JSON cannot supply an implementation or callback.
type windowStoppedHistory interface {
	revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error)
}

func (s windowStoppedBaseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	fresh, err := loadWindowStoppedBaseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s.base, fresh.base) || s.originalRoot != fresh.originalRoot || s.historicalDir != fresh.historicalDir || s.liabilityMicroCNY != fresh.liabilityMicroCNY {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}

func expectedStoppedWindowBaseline() fullWindowBaseline {
	claims := fullWindowCanonicalClaims()
	claims[windowStoppedClaimID] = windowStoppedArtifactPins()["campaign/claim-000001.json"]
	return fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29475706, PermanentJEV: 227183, RemainingModel: 70524294, RemainingJEV: 772817, ActualModelCalls: 144, ActualJEVCalls: 37, ClaimSHA256: claims}
}

func newWindowCampaignV2(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits, stopped windowStoppedBaseline) (*windowCampaign, error) {
	if scope.Schema != "full-window-scope.v2" || scope.BaselineEvidenceSHA256 != stopped.evidenceSHA256 {
		return nil, fmt.Errorf("v2 campaign scope version or baseline rejected")
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

func validateStoppedCampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	if p.scope.Schema != "full-window-scope.v2" || p.stoppedHistory == nil || !reflect.DeepEqual(base, expectedStoppedWindowBaseline()) {
		return fmt.Errorf("verified v2 historical baseline required")
	}
	fresh, sha, oldRoot, err := p.stoppedHistory.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, base) || !comparisonDigest(sha) || sha != p.scope.BaselineEvidenceSHA256 || !filepath.IsAbs(oldRoot) || filepath.Clean(oldRoot) != oldRoot || !windowPayloadDisjoint(p.root, oldRoot) {
		return fmt.Errorf("v2 historical evidence changed or roots overlap")
	}
	return nil
}

func cloneWindowHistoricalClaims(claims map[string]string) map[string]string {
	out := make(map[string]string, len(claims))
	for id, sha := range claims {
		out[id] = sha
	}
	return out
}
