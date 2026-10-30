package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

const windowStoppedV3PreviousSHA = "857bc856db8ee7837f9043a97fd98080d7e881c78fae22a6f6a789d0bbbd4ab3"

type windowStoppedV3Evidence struct {
	Schema           string           `json:"schema"`
	PreviousBaseline windowActualFile `json:"previous_baseline"`
	OriginalRoot     string           `json:"original_root"`
	BudgetApproval   windowActualFile `json:"budget_approval"`
}

// Concrete read-only history, deliberately not an embedded old history type or
// a paid-ready token. No verifier/provider/issuer can be supplied by the caller.
type windowStoppedV3Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV2Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func loadWindowStoppedV3Baseline(path, expectedSHA string) (windowStoppedV3Baseline, error) {
	return loadWindowStoppedV3BaselineContext(context.Background(), path, expectedSHA)
}

// Context carries the caller's deadline through every nested historical read.
func loadWindowStoppedV3BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV3Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV3Baseline, error) {
		return loadWindowStoppedV3BaselineWithPass(p, path, expectedSHA)
	})
}

func loadWindowStoppedV3BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV3Baseline, error) {
	if p == nil {
		return windowStoppedV3Baseline{}, fmt.Errorf("historical pass required")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return windowStoppedV3Baseline{}, err
	}
	reject := func() (windowStoppedV3Baseline, error) {
		return windowStoppedV3Baseline{}, fmt.Errorf("stopped v3 concrete evidence rejected")
	}
	raw, err := historyStoppedFile(p, path, expectedSHA, 65536)
	var e windowStoppedV3Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || e.Schema != "full-window-stopped-baseline.v3" || e.PreviousBaseline.SHA256 != windowStoppedV3PreviousSHA || e.BudgetApproval.SHA256 != windowBudget105ApprovalSHA {
		return reject()
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject()
	}
	previous, err := loadWindowStoppedV2BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV2WindowBaseline()) {
		return reject()
	}
	canonical, err := filepath.EvalSymlinks(e.OriginalRoot)
	if err != nil || !filepath.IsAbs(e.OriginalRoot) || filepath.Clean(e.OriginalRoot) != e.OriginalRoot || canonical != e.OriginalRoot {
		return reject()
	}
	if err := historyStoppedRoot(p, e.OriginalRoot); err != nil {
		return windowStoppedV3Baseline{}, err
	}
	manifestRaw, err := historyStoppedFile(p, filepath.Join(e.OriginalRoot, "actual-manifest.json"), windowStoppedV3ArtifactPins()["actual-manifest.json"], 8<<20)
	var m windowActualManifest
	if err != nil || decodeWindowActualJSON(manifestRaw, &m) != nil || validateWindowStoppedV3Namespace(m, e, previous) != nil {
		return reject()
	}
	var out windowStoppedV3Baseline
	err = historyStoppedLock(p, 3, filepath.Join(e.OriginalRoot, "campaign/.lock"), func() error {
		files, err := readWindowStoppedV3FilesWithPass(p, e.OriginalRoot)
		if err != nil {
			return err
		}
		base, err := readWindowStoppedV3BundleWithPass(p, e.OriginalRoot, m, previous.base, files)
		if err != nil {
			return err
		}
		// Every pinned top artifact is re-read after all descendants under the same
		// original read-only lock, including original failed/audit-only metadata.
		if _, err = readWindowStoppedV3FilesWithPass(p, e.OriginalRoot); err != nil {
			return err
		}
		out = windowStoppedV3Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: e.OriginalRoot, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return windowStoppedV3Baseline{}, err
	}
	fresh, err := loadWindowStoppedV2BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous, fresh) {
		return windowStoppedV3Baseline{}, fmt.Errorf("sixteen-claim predecessor changed")
	}
	if _, err = historyStoppedFile(p, path, expectedSHA, 65536); err != nil {
		return windowStoppedV3Baseline{}, err
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return windowStoppedV3Baseline{}, err
	}
	return out, nil
}
func (s windowStoppedV3Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if !reflect.DeepEqual(s.base, expectedStoppedV3WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v3 cached baseline changed")
	}
	fresh, err := loadWindowStoppedV3Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v3 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
func (s windowStoppedV3Baseline) windowStoppedPredecessorRootsV4() [3]string {
	return [3]string{s.previous.first.originalRoot, s.previous.originalRoot, s.originalRoot}
}
