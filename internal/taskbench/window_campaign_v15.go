package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped177 baseline.
func windowV15CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 200000000, 3000000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV15Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v15" || validateWindowV15Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v15 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV15CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV15CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV15CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete178 baseline.
	h := p.stoppedV14
	ok := h != nil
	if !ok || p.scope.Schema != "full-window-scope.v15" || !reflect.DeepEqual(base, windowV15CandidateBaseline(expectedStoppedV14WindowBaseline())) || validateWindowV15Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v15 concrete178 history required")
	}
	proof, err := loadWindowStoppedV14BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, *h) {
		return fmt.Errorf("v15 historical snapshot changed")
	}
	roots := h.predecessorRoots()
	if err != nil || sha != windowV15BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV14WindowBaseline()) || last != roots[13] {
		return fmt.Errorf("v15 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v15 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v15 crossed predecessors")
			}
		}
	}
	return nil
}
