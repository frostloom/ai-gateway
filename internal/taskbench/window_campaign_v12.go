package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped154 baseline.
func windowV12CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 195000000, 3000000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV12Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v12" || validateWindowV12Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v12 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV12CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV12CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV12CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete154 baseline.
	h := p.stoppedV11
	ok := h != nil
	if !ok || p.scope.Schema != "full-window-scope.v12" || !reflect.DeepEqual(base, windowV12CandidateBaseline(expectedStoppedV11WindowBaseline())) || validateWindowV12Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v12 concrete154 history required")
	}
	proof, err := loadWindowStoppedV11BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, *h) {
		return fmt.Errorf("v12 historical snapshot changed")
	}
	roots := h.predecessorRoots()
	if err != nil || sha != windowV12BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV11WindowBaseline()) || last != roots[10] {
		return fmt.Errorf("v12 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v12 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v12 crossed predecessors")
			}
		}
	}
	return nil
}
