package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
)

type windowStoppedV2Evidence struct {
	Schema         string           `json:"schema"`
	FirstBaseline  windowActualFile `json:"first_baseline"`
	OriginalRoot   string           `json:"original_root"`
	BudgetApproval windowActualFile `json:"budget_approval"`
}

type windowStoppedV2Baseline struct {
	base                                                      fullWindowBaseline
	first                                                     windowStoppedBaseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func loadWindowStoppedV2Baseline(path, expectedSHA string) (windowStoppedV2Baseline, error) {
	return loadWindowStoppedV2BaselineContext(context.Background(), path, expectedSHA)
}

// Context carries the caller's deadline through every nested historical read.
func loadWindowStoppedV2BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV2Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV2Baseline, error) {
		return loadWindowStoppedV2BaselineWithPass(p, path, expectedSHA)
	})
}

func loadWindowStoppedV2BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV2Baseline, error) {
	if p == nil {
		return windowStoppedV2Baseline{}, fmt.Errorf("historical pass required")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return windowStoppedV2Baseline{}, err
	}
	var out windowStoppedV2Baseline
	reject := func() (windowStoppedV2Baseline, error) {
		return out, fmt.Errorf("stopped v2 concrete evidence rejected")
	}
	raw, err := historyStoppedFile(p, path, expectedSHA, 65536)
	var e windowStoppedV2Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || e.Schema != "full-window-stopped-baseline.v2" || e.FirstBaseline.SHA256 != "4c55d694405c61948857dfd72279118243e5ea4dbd2de72b6254687ab08d1dbf" || e.BudgetApproval.SHA256 != windowBudget105ApprovalSHA {
		return reject()
	}
	if _, err = historyStoppedFile(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject()
	}
	first, err := loadWindowStoppedBaselineWithPass(p, e.FirstBaseline.Path, e.FirstBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(first.base, expectedStoppedWindowBaseline()) {
		return reject()
	}
	canonical, err := filepath.EvalSymlinks(e.OriginalRoot)
	if err != nil || !filepath.IsAbs(e.OriginalRoot) || filepath.Clean(e.OriginalRoot) != e.OriginalRoot || canonical != e.OriginalRoot || !windowPayloadDisjoint(first.originalRoot, e.OriginalRoot) {
		return reject()
	}
	pins := windowStoppedV2ArtifactPins()
	if err := historyStoppedRoot(p, e.OriginalRoot); err != nil {
		return windowStoppedV2Baseline{}, err
	}
	manifestRaw, err := historyStoppedFile(p, filepath.Join(e.OriginalRoot, "actual-manifest.json"), pins["actual-manifest.json"], 8<<20)
	var m windowActualManifest
	if err != nil || decodeWindowActualJSON(manifestRaw, &m) != nil || validateWindowStoppedV2Namespace(m, e, first) != nil {
		return reject()
	}
	err = historyStoppedLock(p, 2, filepath.Join(e.OriginalRoot, "campaign/.lock"), func() error {
		files := map[string][]byte{}
		names := make([]string, 0, len(pins))
		for name := range pins {
			names = append(names, name)
		}
		sort.Strings(names)
		for start := 0; start < len(names); start += 4 {
			end := min(start+4, len(names))
			specs := make([]windowStoppedReadSpec, end-start)
			for i, name := range names[start:end] {
				specs[i] = windowStoppedReadSpec{path: filepath.Join(e.OriginalRoot, filepath.FromSlash(name)), sha256: pins[name], maxBytes: 8 << 20, kind: windowStoppedReadFile}
			}
			b, err := historyStoppedBatch(p, specs)
			if err != nil {
				return fmt.Errorf("stopped v2 top artifact missing or changed")
			}
			for i, name := range names[start:end] {
				files[name] = b[i]
			}
		}
		base, err := readWindowStoppedV2BundleWithPass(p, e.OriginalRoot, m, first.base, files)
		if err != nil {
			return err
		}
		out = windowStoppedV2Baseline{base: base, first: first, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: e.OriginalRoot, historicalDir: first.historicalDir}
		return nil
	})
	if err != nil {
		return windowStoppedV2Baseline{}, err
	}
	// Re-read the first stopped proof after the second owner's immutable bundle;
	// each owner uses its own existing read-only lock, never a new lock or writer.
	fresh, err := loadWindowStoppedBaselineWithPass(p, e.FirstBaseline.Path, e.FirstBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(first, fresh) {
		return windowStoppedV2Baseline{}, fmt.Errorf("first stopped predecessor changed")
	}
	if _, err = historyStoppedFile(p, path, expectedSHA, 65536); err != nil {
		return windowStoppedV2Baseline{}, err
	}
	return out, nil
}

func validateWindowStoppedV2Namespace(m windowActualManifest, e windowStoppedV2Evidence, first windowStoppedBaseline) error {
	if m.Schema != "full-window-actual-ready.v2" || m.Scope.Schema != "full-window-scope.v2" || m.ArchiveCommit != "3eb22933050d61ff57068964e42a3312bb5d4eec" || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || m.CampaignRoot != filepath.Join(e.OriginalRoot, "campaign") || m.SessionRoot != filepath.Join(e.OriginalRoot, "records") || m.ClaimsDir != first.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.FirstBaseline || !reflect.DeepEqual(m.HistoricalClaims, first.base.ClaimSHA256) {
		return fmt.Errorf("stopped v2 namespace differs from original")
	}
	return nil
}

func (s windowStoppedV2Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	fresh, err := loadWindowStoppedV2Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v2 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
