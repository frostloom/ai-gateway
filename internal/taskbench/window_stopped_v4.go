package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

const windowStoppedV4PreviousSHA = "12a9d584ea9508a9fc7701532ad9214b0c743a8e13d17829432fac0d6b48a20c"

type windowStoppedV4Evidence struct {
	Schema           string           `json:"schema"`
	PreviousBaseline windowActualFile `json:"previous_baseline"`
	OriginalRoot     string           `json:"original_root"`
	BudgetApproval   windowActualFile `json:"budget_approval"`
}

// Concrete read-only history, deliberately not an embedded old history type or
// a paid-ready token. No verifier/provider/issuer can be supplied by the caller.
type windowStoppedV4Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV3Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func loadWindowStoppedV4Baseline(path, expectedSHA string) (windowStoppedV4Baseline, error) {
	return loadWindowStoppedV4BaselineContext(context.Background(), path, expectedSHA)
}

// Context carries the caller's deadline through every nested historical read.
func loadWindowStoppedV4BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV4Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV4Baseline, error) {
		return loadWindowStoppedV4BaselineWithPass(p, path, expectedSHA)
	})
}

func loadWindowStoppedV4BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV4Baseline, error) {
	if p == nil {
		return windowStoppedV4Baseline{}, fmt.Errorf("historical pass required")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return windowStoppedV4Baseline{}, err
	}
	reject := func() (windowStoppedV4Baseline, error) {
		return windowStoppedV4Baseline{}, fmt.Errorf("stopped v4 concrete evidence rejected")
	}
	raw, err := historyStoppedFile(p, path, expectedSHA, 65536)
	var e windowStoppedV4Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || e.Schema != "full-window-stopped-baseline.v4" || e.PreviousBaseline.SHA256 != windowStoppedV4PreviousSHA || e.BudgetApproval.SHA256 != windowBudget105ApprovalSHA {
		return reject()
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject()
	}
	previous, err := loadWindowStoppedV3BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV3WindowBaseline()) {
		return reject()
	}
	canonical, err := filepath.EvalSymlinks(e.OriginalRoot)
	if err != nil || !filepath.IsAbs(e.OriginalRoot) || filepath.Clean(e.OriginalRoot) != e.OriginalRoot || canonical != e.OriginalRoot {
		return reject()
	}
	if err := historyStoppedRoot(p, e.OriginalRoot); err != nil {
		return windowStoppedV4Baseline{}, err
	}
	manifestRaw, err := historyStoppedFile(p, filepath.Join(e.OriginalRoot, "actual-manifest.json"), windowStoppedV4ArtifactPins()["actual-manifest.json"], 8<<20)
	var m windowActualManifest
	if err != nil || decodeWindowActualJSON(manifestRaw, &m) != nil || validateWindowStoppedV4Namespace(m, e, previous) != nil {
		return reject()
	}
	var out windowStoppedV4Baseline
	err = historyStoppedLock(p, 4, filepath.Join(e.OriginalRoot, "campaign/.lock"), func() error {
		files, err := readWindowStoppedV4FilesWithPass(p, e.OriginalRoot)
		if err != nil {
			return err
		}
		base, err := readWindowStoppedV4BundleWithPass(p, e.OriginalRoot, m, previous.base, files)
		if err != nil {
			return err
		}
		// Every pinned top artifact is re-read after all descendants under the same
		// original read-only lock, including original failed/audit-only metadata.
		if _, err = readWindowStoppedV4FilesWithPass(p, e.OriginalRoot); err != nil {
			return err
		}
		out = windowStoppedV4Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: e.OriginalRoot, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return windowStoppedV4Baseline{}, err
	}
	fresh, err := loadWindowStoppedV3BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous, fresh) {
		return windowStoppedV4Baseline{}, fmt.Errorf("twenty-five-claim predecessor changed")
	}
	if _, err = historyStoppedFile(p, path, expectedSHA, 65536); err != nil {
		return windowStoppedV4Baseline{}, err
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return windowStoppedV4Baseline{}, err
	}
	return out, nil
}
func (s windowStoppedV4Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if !reflect.DeepEqual(s.base, expectedStoppedV4WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v4 cached baseline changed")
	}
	fresh, err := loadWindowStoppedV4Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v4 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
func (s windowStoppedV4Baseline) windowStoppedPredecessorRootsV5() [4]string {
	return [4]string{s.previous.previous.first.originalRoot, s.previous.previous.originalRoot, s.previous.originalRoot, s.originalRoot}
}
