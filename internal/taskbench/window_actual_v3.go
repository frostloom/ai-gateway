package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

func validateWindowActualStoppedBindingV3(m windowActualManifest, proof windowStoppedV2Baseline) error {
	f := m.BaselineEvidence
	if m.Schema != "full-window-actual-ready.v3" || m.Scope.Schema != "full-window-scope.v3" || m.Scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || f == nil || !comparisonDigest(f.SHA256) || f.SHA256 != m.Scope.BaselineEvidenceSHA256 || f.SHA256 != proof.evidenceSHA256 || f.Path != proof.evidencePath || m.ClaimsDir != proof.historicalDir || !reflect.DeepEqual(proof.base, expectedStoppedV2WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, proof.base.ClaimSHA256) || !reflect.DeepEqual(proof.first.base, expectedStoppedWindowBaseline()) || proof.first.liabilityMicroCNY != 3832 || proof.first.historicalDir != proof.historicalDir {
		return fmt.Errorf("actual v3 stopped binding rejected")
	}
	roots := proof.windowStoppedPredecessorRoots()
	if !windowPayloadDisjoint(roots[0], roots[1]) {
		return fmt.Errorf("actual v3 stopped roots crossed")
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("actual v3 stopped roots overlap")
		}
	}
	return nil
}

func loadWindowActualStoppedBaselineV3(m windowActualManifest) (windowStoppedV2Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV2Baseline{}, fmt.Errorf("actual v3 stopped evidence absent")
	}
	f := *m.BaselineEvidence
	proof, err := loadWindowStoppedV2Baseline(f.Path, f.SHA256)
	if err != nil {
		return windowStoppedV2Baseline{}, fmt.Errorf("actual v3 stopped evidence rejected")
	}
	if err = validateWindowActualStoppedBindingV3(m, proof); err != nil {
		return windowStoppedV2Baseline{}, err
	}
	return proof, nil
}

func windowActualSupportedEpoch(m windowActualManifest) bool {
	return m.Schema == "full-window-actual-ready.v7" && m.Scope.Schema == "full-window-scope.v7" || m.Schema == "full-window-actual-ready.v6" && m.Scope.Schema == "full-window-scope.v6" || m.Schema == "full-window-actual-ready.v5" && m.Scope.Schema == "full-window-scope.v5" || m.Schema == "full-window-actual-ready.v4" && m.Scope.Schema == "full-window-scope.v4" || m.Schema == "full-window-actual-ready.v2" && m.Scope.Schema == "full-window-scope.v2" || m.Schema == "full-window-actual-ready.v3" && m.Scope.Schema == "full-window-scope.v3"
}

func (r *FullWindowActualReady) revalidateStoppedV3() error {
	if r == nil || validateWindowActualStoppedBindingV3(r.manifest, r.stoppedV2) != nil {
		return fmt.Errorf("actual v3 stopped snapshot absent or changed")
	}
	fresh, err := loadWindowActualStoppedBaselineV3(r.manifest)
	if err != nil || !reflect.DeepEqual(r.stoppedV2, fresh) || !reflect.DeepEqual(r.baseline, fresh.base) {
		return fmt.Errorf("actual v3 stopped evidence changed")
	}
	return nil
}
