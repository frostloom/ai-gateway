package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// Consistency alone cannot mint readiness: both startup and every window must
// reload the concrete twenty-five-claim proof from its pinned original files.
func validateWindowActualStoppedBindingV4(m windowActualManifest, proof windowStoppedV3Baseline) error {
	f := m.BaselineEvidence
	if m.Schema != "full-window-actual-ready.v4" || m.Scope.Schema != "full-window-scope.v4" || m.Scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || f == nil || f.SHA256 != windowV4BaselineSHA || f.SHA256 != m.Scope.BaselineEvidenceSHA256 || f.SHA256 != proof.evidenceSHA256 || f.Path != proof.evidencePath || m.ClaimsDir != proof.historicalDir || !reflect.DeepEqual(proof.base, expectedStoppedV3WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, proof.base.ClaimSHA256) || !reflect.DeepEqual(proof.previous.base, expectedStoppedV2WindowBaseline()) || proof.previous.evidenceSHA256 != windowStoppedV3PreviousSHA || proof.previous.historicalDir != proof.historicalDir || !reflect.DeepEqual(proof.previous.first.base, expectedStoppedWindowBaseline()) || proof.previous.first.liabilityMicroCNY != 3832 || proof.previous.first.historicalDir != proof.historicalDir || m.Limits != windowV4ExpectedLimits() {
		return fmt.Errorf("actual v4 stopped binding rejected")
	}
	roots := proof.windowStoppedPredecessorRootsV4()
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("actual v4 stopped roots overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("actual v4 stopped roots crossed")
			}
		}
	}
	return nil
}

func loadWindowActualStoppedBaselineV4(m windowActualManifest) (windowStoppedV3Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV3Baseline{}, fmt.Errorf("actual v4 stopped evidence absent")
	}
	f := *m.BaselineEvidence
	proof, err := loadWindowStoppedV3Baseline(f.Path, f.SHA256)
	if err != nil || validateWindowActualStoppedBindingV4(m, proof) != nil {
		return windowStoppedV3Baseline{}, fmt.Errorf("actual v4 stopped evidence rejected")
	}
	return proof, nil
}

func (r *FullWindowActualReady) revalidateStoppedV4() error {
	if r == nil || validateWindowActualStoppedBindingV4(r.manifest, r.stoppedV3) != nil {
		return fmt.Errorf("actual v4 stopped snapshot absent or changed")
	}
	fresh, err := loadWindowActualStoppedBaselineV4(r.manifest)
	if err != nil || !reflect.DeepEqual(r.stoppedV3, fresh) || !reflect.DeepEqual(r.baseline, fresh.base) {
		return fmt.Errorf("actual v4 stopped evidence changed")
	}
	return nil
}
