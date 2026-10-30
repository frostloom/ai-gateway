package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

func validateWindowActualStoppedBindingV7(m windowActualManifest, p windowStoppedV6Baseline) error {
	f := m.BaselineEvidence
	if m.Schema != "full-window-actual-ready.v7" || m.Scope.Schema != "full-window-scope.v7" || f == nil || f.SHA256 != windowV7BaselineSHA || f.SHA256 != m.Scope.BaselineEvidenceSHA256 || f.SHA256 != p.evidenceSHA256 || f.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV6WindowBaseline()) || !reflect.DeepEqual(p.previous.base, expectedStoppedV5WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() {
		return fmt.Errorf("actual v7 stopped binding rejected")
	}
	caps := make([]fullWindowCaps, len(m.Scope.Windows))
	for i, w := range m.Scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if validateWindowV7Declaration(m.Scope.Width, caps, m.Scope.BaselineEvidenceSHA256, m.Scope.BudgetApprovalSHA256) != nil {
		return fmt.Errorf("actual v7 fixed declaration rejected")
	}
	roots := p.windowStoppedPredecessorRootsV7()
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("actual v7 roots overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("actual v7 roots crossed")
			}
		}
	}
	return nil
}
func loadWindowActualStoppedBaselineV7(m windowActualManifest) (windowStoppedV6Baseline, windowActualFile, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV6Baseline{}, windowActualFile{}, fmt.Errorf("actual v7 stopped evidence absent")
	}
	p, err := loadWindowStoppedV6Baseline(m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV7(m, p) != nil {
		return windowStoppedV6Baseline{}, windowActualFile{}, fmt.Errorf("actual v7 stopped evidence rejected")
	}
	a, err := windowActualV6Approval(m)
	if err != nil {
		return windowStoppedV6Baseline{}, windowActualFile{}, err
	}
	return p, a, nil
}
func (r *FullWindowActualReady) revalidateStoppedV7() error {
	if r == nil || validateWindowActualStoppedBindingV7(r.manifest, r.stoppedV6) != nil {
		return fmt.Errorf("actual v7 stopped snapshot absent or changed")
	}
	p, a, err := loadWindowActualStoppedBaselineV7(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV6) || a != r.approval130 || !reflect.DeepEqual(r.baseline, p.base) {
		return fmt.Errorf("actual v7 stopped evidence or approval changed")
	}
	return nil
}

func windowObserverTimeoutForScope(schema string) int64 {
	if schema == "full-window-scope.v7" || windowScopeUsesNativeV8(schema) {
		return 10000
	}
	return 2000
}

// windowActualGoCommand binds each epoch to its independently reviewed full regression.
func windowActualGoCommand(schema string) []string {
	timeout := "-timeout=20m"
	if schema == "full-window-actual-ready.v7" || (schema == "full-window-actual-ready.v8" || schema == "full-window-actual-ready.v9") {
		timeout = "-timeout=30m"
	}
	return []string{"go", "test", "./...", "-count=1", timeout, "-json"}
}
