package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped102 baseline.
func windowV10CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 185000000, 3000000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV10Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v10" || validateWindowV10Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v10 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV10CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV10CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV10CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete102 baseline.
	h := p.stoppedV9
	ok := h != nil
	if !ok || p.scope.Schema != "full-window-scope.v10" || !reflect.DeepEqual(base, windowV10CandidateBaseline(expectedStoppedV9WindowBaseline())) || validateWindowV10Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v10 concrete102 history required")
	}
	proof, err := loadWindowStoppedV9BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, *h) {
		return fmt.Errorf("v10 historical snapshot changed")
	}
	roots := h.windowStoppedPredecessorRootsV10()
	if err != nil || sha != windowV10BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV9WindowBaseline()) || last != roots[8] {
		return fmt.Errorf("v10 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v10 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v10 crossed predecessors")
			}
		}
	}
	return nil
}
