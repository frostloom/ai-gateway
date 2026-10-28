package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// A matching in-memory snapshot never substitutes for the concrete28 file proof.
func validateWindowActualStoppedBindingV5(m windowActualManifest, proof windowStoppedV4Baseline) error {
	f := m.BaselineEvidence
	if m.Schema != "full-window-actual-ready.v5" || m.Scope.Schema != "full-window-scope.v5" || f == nil || f.SHA256 != windowV5BaselineSHA || f.SHA256 != m.Scope.BaselineEvidenceSHA256 || f.SHA256 != proof.evidenceSHA256 || f.Path != proof.evidencePath || m.ClaimsDir != proof.historicalDir || !reflect.DeepEqual(proof.base, expectedStoppedV4WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, proof.base.ClaimSHA256) || proof.previous.evidenceSHA256 != windowStoppedV4PreviousSHA || proof.previous.historicalDir != proof.historicalDir || m.Limits != windowV4ExpectedLimits() {
		return fmt.Errorf("actual v5 stopped binding rejected")
	}
	caps := make([]fullWindowCaps, len(m.Scope.Windows))
	for i, w := range m.Scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if validateWindowV5Declaration(m.Scope.Width, caps, m.Scope.BaselineEvidenceSHA256, m.Scope.BudgetApprovalSHA256) != nil {
		return fmt.Errorf("actual v5 fixed budget declaration rejected")
	}
	// Reuse the historical consistency checks on a data-only ancestor view. This
	// neither loads old actual readiness nor grants the new proof an old interface.
	ancestor := m
	ancestor.Schema = "full-window-actual-ready.v4"
	ancestor.Scope.Schema = "full-window-scope.v4"
	ancestor.Scope.BaselineEvidenceSHA256 = proof.previous.evidenceSHA256
	ancestor.BaselineEvidence = &windowActualFile{proof.previous.evidencePath, proof.previous.evidenceSHA256}
	ancestor.HistoricalClaims = proof.previous.base.ClaimSHA256
	if validateWindowActualStoppedBindingV4(ancestor, proof.previous) != nil {
		return fmt.Errorf("actual v5 stopped predecessor chain rejected")
	}
	roots := proof.windowStoppedPredecessorRootsV5()
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("actual v5 stopped roots overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("actual v5 stopped roots crossed")
			}
		}
	}
	return nil
}
func loadWindowActualStoppedBaselineV5(m windowActualManifest) (windowStoppedV4Baseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV4Baseline{}, fmt.Errorf("actual v5 stopped evidence absent")
	}
	f := *m.BaselineEvidence
	proof, e := loadWindowStoppedV4Baseline(f.Path, f.SHA256)
	if e != nil || validateWindowActualStoppedBindingV5(m, proof) != nil {
		return windowStoppedV4Baseline{}, fmt.Errorf("actual v5 stopped evidence rejected")
	}
	return proof, nil
}
func (r *FullWindowActualReady) revalidateStoppedV5() error {
	if r == nil || validateWindowActualStoppedBindingV5(r.manifest, r.stoppedV4) != nil {
		return fmt.Errorf("actual v5 stopped snapshot absent or changed")
	}
	fresh, e := loadWindowActualStoppedBaselineV5(r.manifest)
	if e != nil || !reflect.DeepEqual(r.stoppedV4, fresh) || !reflect.DeepEqual(r.baseline, fresh.base) {
		return fmt.Errorf("actual v5 stopped evidence changed")
	}
	return nil
}
