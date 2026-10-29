package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// Approval is obtained only from the already anchored envelope, never a caller hook.
func windowActualV6Approval(m windowActualManifest) (windowActualFile, error) {
	raw, err := readWindowActualFile(m.Envelope.Path, m.Envelope.SHA256, 8<<20)
	var e windowActualProviderEvidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil {
		return windowActualFile{}, fmt.Errorf("actual v6 envelope rejected")
	}
	seenPath, seenSHA := map[string]bool{}, map[string]bool{}
	var approval windowActualFile
	for _, f := range e.Basis {
		if seenPath[f.Path] || seenSHA[f.SHA256] {
			return windowActualFile{}, fmt.Errorf("actual v6 duplicate basis")
		}
		seenPath[f.Path], seenSHA[f.SHA256] = true, true
		if _, err := readWindowActualFile(f.Path, f.SHA256, 64<<20); err != nil {
			return windowActualFile{}, err
		}
		if f.SHA256 == windowBudget130ApprovalSHA {
			approval = f
		}
	}
	if approval.SHA256 != windowBudget130ApprovalSHA {
		return windowActualFile{}, fmt.Errorf("actual v6 approval absent")
	}
	if _, err := readWindowActualFile(approval.Path, approval.SHA256, 65536); err != nil {
		return windowActualFile{}, err
	}
	return approval, nil
}

func validateWindowActualStoppedBindingV6(m windowActualManifest, p windowStoppedV5Baseline) error {
	f := m.BaselineEvidence
	if m.Schema != "full-window-actual-ready.v6" || m.Scope.Schema != "full-window-scope.v6" || f == nil || f.SHA256 != windowV6BaselineSHA || f.SHA256 != m.Scope.BaselineEvidenceSHA256 || f.SHA256 != p.evidenceSHA256 || f.Path != p.evidencePath || m.ClaimsDir != p.historicalDir || !reflect.DeepEqual(p.base, expectedStoppedV5WindowBaseline()) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || p.previous.evidenceSHA256 != windowStoppedV5PreviousSHA || p.previous.historicalDir != p.historicalDir || m.Limits != windowV4ExpectedLimits() {
		return fmt.Errorf("actual v6 stopped binding rejected")
	}
	caps := make([]fullWindowCaps, len(m.Scope.Windows))
	for i, w := range m.Scope.Windows {
		caps[i] = w.fullWindowCaps
	}
	if validateWindowV6Declaration(m.Scope.Width, caps, m.Scope.BaselineEvidenceSHA256, m.Scope.BudgetApprovalSHA256) != nil {
		return fmt.Errorf("actual v6 fixed declaration rejected")
	}
	ancestor := m
	ancestor.Schema = "full-window-actual-ready.v4"
	ancestor.Scope.Schema = "full-window-scope.v4"
	ancestor.Scope.BaselineEvidenceSHA256 = p.previous.previous.evidenceSHA256
	ancestor.Scope.BudgetApprovalSHA256 = windowBudget105ApprovalSHA
	ancestor.BaselineEvidence = &windowActualFile{p.previous.previous.evidencePath, p.previous.previous.evidenceSHA256}
	ancestor.HistoricalClaims = p.previous.previous.base.ClaimSHA256
	if !reflect.DeepEqual(p.previous.base, expectedStoppedV4WindowBaseline()) || validateWindowActualStoppedBindingV4(ancestor, p.previous.previous) != nil {
		return fmt.Errorf("actual v6 predecessor chain rejected")
	}
	roots := p.windowStoppedPredecessorRootsV6()
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || !windowPayloadDisjoint(m.SessionRoot, root) || !windowPayloadDisjoint(m.CampaignRoot, root) {
			return fmt.Errorf("actual v6 roots overlap")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("actual v6 roots crossed")
			}
		}
	}
	return nil
}
func loadWindowActualStoppedBaselineV6(m windowActualManifest) (windowStoppedV5Baseline, windowActualFile, error) {
	if m.BaselineEvidence == nil {
		return windowStoppedV5Baseline{}, windowActualFile{}, fmt.Errorf("actual v6 stopped evidence absent")
	}
	p, err := loadWindowStoppedV5Baseline(m.BaselineEvidence.Path, m.BaselineEvidence.SHA256)
	if err != nil || validateWindowActualStoppedBindingV6(m, p) != nil {
		return windowStoppedV5Baseline{}, windowActualFile{}, fmt.Errorf("actual v6 stopped evidence rejected")
	}
	a, err := windowActualV6Approval(m)
	if err != nil {
		return windowStoppedV5Baseline{}, windowActualFile{}, err
	}
	return p, a, nil
}
func (r *FullWindowActualReady) revalidateStoppedV6() error {
	if r == nil || validateWindowActualStoppedBindingV6(r.manifest, r.stoppedV5) != nil {
		return fmt.Errorf("actual v6 stopped snapshot absent or changed")
	}
	p, a, err := loadWindowActualStoppedBaselineV6(r.manifest)
	if err != nil || !reflect.DeepEqual(p, r.stoppedV5) || a != r.approval130 || !reflect.DeepEqual(r.baseline, expectedCurrent130WindowBaseline()) {
		return fmt.Errorf("actual v6 stopped evidence or approval changed")
	}
	return nil
}
