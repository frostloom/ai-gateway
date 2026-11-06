package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped177 baseline.
func windowV14CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 200000000, 3000000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV14Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v14" || validateWindowV14Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v14 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV14CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV14CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV14CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete177 baseline.
	h := p.stoppedV13
	ok := h != nil
	if !ok || p.scope.Schema != "full-window-scope.v14" || !reflect.DeepEqual(base, windowV14CandidateBaseline(expectedStoppedV13WindowBaseline())) || validateWindowV14Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v14 concrete177 history required")
	}
	proof, err := loadWindowStoppedV13BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, *h) {
		return fmt.Errorf("v14 historical snapshot changed")
	}
	roots := h.predecessorRoots()
	if err != nil || sha != windowV14BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV13WindowBaseline()) || last != roots[12] {
		return fmt.Errorf("v14 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v14 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v14 crossed predecessors")
			}
		}
	}
	return nil
}
