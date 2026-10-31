package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped104 baseline.
func windowV11CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 195000000, 3000000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV11Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v11" || validateWindowV11Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v11 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV11CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	return validateStoppedV11CampaignHistoryContext(context.Background(), p, base)
}
func validateStoppedV11CampaignHistoryContext(ctx context.Context, p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete104 baseline.
	h := p.stoppedV10
	ok := h != nil
	if !ok || p.scope.Schema != "full-window-scope.v11" || !reflect.DeepEqual(base, windowV11CandidateBaseline(expectedStoppedV10WindowBaseline())) || validateWindowV11Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v11 concrete104 history required")
	}
	proof, err := loadWindowStoppedV10BaselineContext(ctx, h.evidencePath, h.evidenceSHA256)
	fresh, sha, last := proof.base, proof.evidenceSHA256, proof.originalRoot
	if err == nil && !reflect.DeepEqual(proof, *h) {
		return fmt.Errorf("v11 historical snapshot changed")
	}
	roots := h.predecessorRoots()
	if err != nil || sha != windowV11BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV10WindowBaseline()) || last != roots[9] {
		return fmt.Errorf("v11 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v11 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v11 crossed predecessors")
			}
		}
	}
	return nil
}
