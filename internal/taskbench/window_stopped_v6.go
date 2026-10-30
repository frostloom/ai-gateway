package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
)

const windowStoppedV6PreviousSHA = "b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1"

type windowStoppedV6Evidence struct {
	Schema           string           `json:"schema"`
	PreviousBaseline windowActualFile `json:"previous_baseline"`
	OriginalRoot     string           `json:"original_root"`
	BudgetApproval   windowActualFile `json:"budget_approval"`
}

// Concrete read-only history, deliberately not an embedded old history type or
// a paid-ready token. No verifier/provider/issuer can be supplied by the caller.
type windowStoppedV6Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV5Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func loadWindowStoppedV6Baseline(path, expectedSHA string) (windowStoppedV6Baseline, error) {
	return loadWindowStoppedV6BaselineContext(context.Background(), path, expectedSHA)
}

// Context carries the caller's deadline through every nested historical read.
func loadWindowStoppedV6BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV6Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV6Baseline, error) {
		return loadWindowStoppedV6BaselineWithPass(p, path, expectedSHA)
	})
}

func loadWindowStoppedV6BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV6Baseline, error) {
	if p == nil {
		return windowStoppedV6Baseline{}, fmt.Errorf("historical pass required")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return windowStoppedV6Baseline{}, err
	}
	reject := func() (windowStoppedV6Baseline, error) {
		return windowStoppedV6Baseline{}, fmt.Errorf("stopped v6 concrete evidence rejected")
	}
	raw, err := historyStoppedFile(p, path, expectedSHA, 65536)
	var e windowStoppedV6Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || e.Schema != "full-window-stopped-baseline.v6" || e.PreviousBaseline.SHA256 != windowStoppedV6PreviousSHA || e.BudgetApproval.SHA256 != windowBudget130ApprovalSHA {
		return reject()
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject()
	}
	previous, err := loadWindowStoppedV5BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV5WindowBaseline()) {
		return reject()
	}
	canonical, err := filepath.EvalSymlinks(e.OriginalRoot)
	if err != nil || !filepath.IsAbs(e.OriginalRoot) || filepath.Clean(e.OriginalRoot) != e.OriginalRoot || canonical != e.OriginalRoot {
		return reject()
	}
	if err := historyStoppedRoot(p, e.OriginalRoot); err != nil {
		return windowStoppedV6Baseline{}, err
	}
	manifestRaw, err := historyStoppedFile(p, filepath.Join(e.OriginalRoot, "actual-manifest.json"), windowStoppedV6ArtifactPins()["actual-manifest.json"], 8<<20)
	var m windowActualManifest
	if err != nil || decodeWindowActualJSON(manifestRaw, &m) != nil || validateWindowStoppedV6Namespace(m, e, previous) != nil {
		return reject()
	}
	var out windowStoppedV6Baseline
	err = historyStoppedLock(p, 6, filepath.Join(e.OriginalRoot, "campaign/.lock"), func() error {
		files, err := readWindowStoppedV6FilesWithPass(p, e.OriginalRoot)
		if err != nil {
			return err
		}
		base, err := readWindowStoppedV6BundleWithPass(p, e.OriginalRoot, m, previous.base, files)
		if err != nil {
			return err
		}
		// Every pinned top artifact is re-read after all descendants under the same
		// original read-only lock, including original failed/audit-only metadata.
		if _, err = readWindowStoppedV6FilesWithPass(p, e.OriginalRoot); err != nil {
			return err
		}
		out = windowStoppedV6Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: e.OriginalRoot, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return windowStoppedV6Baseline{}, err
	}
	fresh, err := loadWindowStoppedV5BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous, fresh) {
		return windowStoppedV6Baseline{}, fmt.Errorf("thirty-two-claim predecessor changed")
	}
	if _, err = historyStoppedFile(p, path, expectedSHA, 65536); err != nil {
		return windowStoppedV6Baseline{}, err
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return windowStoppedV6Baseline{}, err
	}
	return out, nil
}
func (s windowStoppedV6Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if !reflect.DeepEqual(s.base, expectedStoppedV6WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v6 cached baseline changed")
	}
	fresh, err := loadWindowStoppedV6Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v6 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
func (s windowStoppedV6Baseline) windowStoppedPredecessorRootsV7() [6]string {
	p := s.previous.windowStoppedPredecessorRootsV6()
	return [6]string{p[0], p[1], p[2], p[3], p[4], s.originalRoot}
}
