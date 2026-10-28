package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// This checks consistency only. Readiness must first obtain the concrete proof
// from loadWindowStoppedBaseline; callers cannot supply one through JSON.
func validateWindowActualStoppedBinding(m windowActualManifest, stopped windowStoppedBaseline) error {
	f := m.BaselineEvidence
	if m.Schema != "full-window-actual-ready.v2" || m.Scope.Schema != "full-window-scope.v2" || f == nil || !comparisonDigest(f.SHA256) || f.SHA256 != m.Scope.BaselineEvidenceSHA256 || f.SHA256 != stopped.evidenceSHA256 || f.Path != stopped.evidencePath || m.ClaimsDir != stopped.historicalDir || !reflect.DeepEqual(stopped.base, expectedStoppedWindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, stopped.base.ClaimSHA256) || stopped.liabilityMicroCNY != 3832 {
		return fmt.Errorf("actual stopped baseline binding rejected")
	}
	if !filepath.IsAbs(stopped.originalRoot) || filepath.Clean(stopped.originalRoot) != stopped.originalRoot || !windowPayloadDisjoint(m.SessionRoot, stopped.originalRoot) || !windowPayloadDisjoint(m.CampaignRoot, stopped.originalRoot) {
		return fmt.Errorf("actual stopped baseline roots overlap")
	}
	return nil
}

func loadWindowActualStoppedBaseline(m windowActualManifest) (windowStoppedBaseline, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedBaseline{}, fmt.Errorf("actual stopped evidence absent")
	}
	f := *m.BaselineEvidence
	proof, err := loadWindowStoppedBaseline(f.Path, f.SHA256)
	if err != nil {
		return windowStoppedBaseline{}, fmt.Errorf("actual stopped evidence rejected")
	}
	if err := validateWindowActualStoppedBinding(m, proof); err != nil {
		return windowStoppedBaseline{}, err
	}
	return proof, nil
}

func (r *FullWindowActualReady) revalidateStopped() error {
	if r != nil && r.manifest.Schema == "full-window-actual-ready.v3" {
		return r.revalidateStoppedV3()
	}
	if r == nil || validateWindowActualStoppedBinding(r.manifest, r.stopped) != nil {
		return fmt.Errorf("actual stopped snapshot absent or changed")
	}
	fresh, err := loadWindowActualStoppedBaseline(r.manifest)
	if err != nil || !reflect.DeepEqual(r.stopped, fresh) || !reflect.DeepEqual(r.baseline, fresh.base) {
		return fmt.Errorf("actual stopped evidence changed")
	}
	return nil
}
