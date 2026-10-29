package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
)

const windowStoppedV5PreviousSHA = "2f8c83546a2918b85801160f521465c2f53e040073b3590e1ecde53f278ef340"

type windowStoppedV5Evidence struct {
	Schema           string           `json:"schema"`
	PreviousBaseline windowActualFile `json:"previous_baseline"`
	OriginalRoot     string           `json:"original_root"`
	BudgetApproval   windowActualFile `json:"budget_approval"`
}

// Concrete read-only history, deliberately not an embedded old history type or
// a paid-ready token. No verifier/provider/issuer can be supplied by the caller.
type windowStoppedV5Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV4Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func loadWindowStoppedV5Baseline(path, expectedSHA string) (windowStoppedV5Baseline, error) {
	reject := func() (windowStoppedV5Baseline, error) {
		return windowStoppedV5Baseline{}, fmt.Errorf("stopped v5 concrete evidence rejected")
	}
	raw, err := readWindowActualFile(path, expectedSHA, 65536)
	var e windowStoppedV5Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || e.Schema != "full-window-stopped-baseline.v5" || e.PreviousBaseline.SHA256 != windowStoppedV5PreviousSHA || e.BudgetApproval.SHA256 != windowBudget105ApprovalSHA {
		return reject()
	}
	if _, err = readWindowActualFile(e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject()
	}
	previous, err := loadWindowStoppedV4Baseline(e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV4WindowBaseline()) {
		return reject()
	}
	canonical, err := filepath.EvalSymlinks(e.OriginalRoot)
	if err != nil || !filepath.IsAbs(e.OriginalRoot) || filepath.Clean(e.OriginalRoot) != e.OriginalRoot || canonical != e.OriginalRoot {
		return reject()
	}
	manifestRaw, err := readWindowActualFile(filepath.Join(e.OriginalRoot, "actual-manifest.json"), windowStoppedV5ArtifactPins()["actual-manifest.json"], 8<<20)
	var m windowActualManifest
	if err != nil || decodeWindowActualJSON(manifestRaw, &m) != nil || validateWindowStoppedV5Namespace(m, e, previous) != nil {
		return reject()
	}
	var out windowStoppedV5Baseline
	err = withWindowStoppedLock(filepath.Join(e.OriginalRoot, "campaign/.lock"), func() error {
		files, err := readWindowStoppedV5Files(e.OriginalRoot)
		if err != nil {
			return err
		}
		base, err := readWindowStoppedV5Bundle(e.OriginalRoot, m, previous.base, files)
		if err != nil {
			return err
		}
		// Every pinned top artifact is re-read after all descendants under the same
		// original read-only lock, including original failed/audit-only metadata.
		if _, err = readWindowStoppedV5Files(e.OriginalRoot); err != nil {
			return err
		}
		out = windowStoppedV5Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: e.OriginalRoot, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return windowStoppedV5Baseline{}, err
	}
	fresh, err := loadWindowStoppedV4Baseline(e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous, fresh) {
		return windowStoppedV5Baseline{}, fmt.Errorf("twenty-eight-claim predecessor changed")
	}
	if _, err = readWindowActualFile(path, expectedSHA, 65536); err != nil {
		return windowStoppedV5Baseline{}, err
	}
	if _, err = readWindowActualFile(e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return windowStoppedV5Baseline{}, err
	}
	return out, nil
}
func (s windowStoppedV5Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if !reflect.DeepEqual(s.base, expectedStoppedV5WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v5 cached baseline changed")
	}
	fresh, err := loadWindowStoppedV5Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v5 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
func (s windowStoppedV5Baseline) windowStoppedPredecessorRootsV6() [5]string {
	return [5]string{s.previous.previous.previous.first.originalRoot, s.previous.previous.previous.originalRoot, s.previous.previous.originalRoot, s.previous.originalRoot, s.originalRoot}
}
