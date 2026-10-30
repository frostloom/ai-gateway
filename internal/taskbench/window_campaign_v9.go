package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped46 baseline.
func windowV9CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 180000000, 2500000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV9Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v9" || validateWindowV9Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v9 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV9CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV9CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV9CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete46 baseline.
	h, ok := p.stoppedHistory.(windowStoppedV8Baseline)
	if !ok || p.scope.Schema != "full-window-scope.v9" || !reflect.DeepEqual(base, windowV9CandidateBaseline(expectedStoppedV8WindowBaseline())) || validateWindowV9Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v9 concrete46 history required")
	}
	proof, err := loadWindowStoppedV8BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, h) {
		return fmt.Errorf("v9 historical snapshot changed")
	}
	roots := h.windowStoppedPredecessorRootsV9()
	if err != nil || sha != windowV9BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV8WindowBaseline()) || last != roots[7] {
		return fmt.Errorf("v9 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v9 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v9 crossed predecessors")
			}
		}
	}
	return nil
}
