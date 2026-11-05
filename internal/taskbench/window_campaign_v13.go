package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped156 baseline.
func windowV13CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 195000000, 3000000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV13Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v13" || validateWindowV13Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v13 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV13CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV13CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV13CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete156 baseline.
	h := p.stoppedV12
	ok := h != nil
	if !ok || p.scope.Schema != "full-window-scope.v13" || !reflect.DeepEqual(base, windowV13CandidateBaseline(expectedStoppedV12WindowBaseline())) || validateWindowV13Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v13 concrete156 history required")
	}
	proof, err := loadWindowStoppedV12BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, *h) {
		return fmt.Errorf("v13 historical snapshot changed")
	}
	roots := h.predecessorRoots()
	if err != nil || sha != windowV13BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV12WindowBaseline()) || last != roots[11] {
		return fmt.Errorf("v13 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v13 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v13 crossed predecessors")
			}
		}
	}
	return nil
}
