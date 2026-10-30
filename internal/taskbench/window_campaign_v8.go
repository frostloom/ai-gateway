package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// Historical totals stay historical. A separately derived view carries the
// candidate parent, without rewriting the concrete stopped44 baseline.
func windowV8CandidateBaseline(b fullWindowBaseline) fullWindowBaseline {
	b.TotalModel, b.TotalJEV = 140000000, 1100000
	b.RemainingModel, b.RemainingJEV = b.TotalModel-b.PermanentModel, b.TotalJEV-b.PermanentJEV
	return b
}
func prepareWindowCampaignV8Technical(root, frozenRoot string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA, frozenExecutionSHA string, limits windowCampaignLimits) (windowCampaignPrepared, error) {
	if scope.Schema != "full-window-scope.v8" || validateWindowV8Declarations(scope) != nil {
		return windowCampaignPrepared{}, fmt.Errorf("v8 declarations required")
	}
	return prepareWindowCampaignData(root, frozenRoot, scope, source, profiles, bindings, frozenSHA, frozenExecutionSHA, limits)
}
func validateStoppedV8CampaignHistory(p windowCampaignPrepared, base fullWindowBaseline) error {
	// No interface substitute can issue the concrete44 baseline.
	h, ok := p.stoppedHistory.(windowStoppedV7Baseline)
	if !ok || p.scope.Schema != "full-window-scope.v8" || !reflect.DeepEqual(base, windowV8CandidateBaseline(expectedStoppedV7WindowBaseline())) || validateWindowV8Declarations(p.scope) != nil || p.limits != windowV4ExpectedLimits() {
		return fmt.Errorf("v8 concrete44 history required")
	}
	fresh, sha, last, err := h.revalidateWindowStoppedHistory()
	roots := h.windowStoppedPredecessorRootsV8()
	if err != nil || sha != windowV8BaselineSHA || !reflect.DeepEqual(fresh, expectedStoppedV7WindowBaseline()) || last != roots[6] {
		return fmt.Errorf("v8 historical proof changed")
	}
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(p.root, root) {
			return fmt.Errorf("v8 predecessor overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("v8 crossed predecessors")
			}
		}
	}
	return nil
}

// Runtime owners check the final external artifact before any window output or
// historical/source scan. Technical preparation deliberately has no such owner.
func (c *windowCampaign) revalidateWindowV8AuthorityLocked() error {
	if c.prepared.scope.Schema != "full-window-scope.v8" {
		return nil
	}
	if err := requireWindowV8ExecutionAuthority(c.prepared.v8Authority); err != nil {
		return err
	}
	if c.prepared.v8Authority.binding.ScopeSHA256 != c.prepared.scopeSHA || c.prepared.v8Authority.binding.ExecutionSHA256 != c.prepared.executionSHA {
		return fmt.Errorf("v8 execution authority scope changed before window outputs")
	}
	return nil
}
func (c *windowCampaign) revalidateWindowV8Authority() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revalidateWindowV8AuthorityLocked()
}
