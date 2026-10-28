package taskbench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func actualV3BindingFixture(t *testing.T) (windowActualManifest, windowStoppedV2Baseline) {
	t.Helper()
	root := stoppedEvidenceRoot(t)
	first := windowStoppedBaseline{base: expectedStoppedWindowBaseline(), originalRoot: filepath.Join(root, "old-first"), historicalDir: filepath.Join(root, "history"), liabilityMicroCNY: 3832}
	proof := windowStoppedV2Baseline{base: expectedStoppedV2WindowBaseline(), first: first, evidencePath: filepath.Join(root, "proof.json"), evidenceSHA256: strings.Repeat("8", 64), originalRoot: filepath.Join(root, "old-second"), historicalDir: first.historicalDir}
	m := windowActualManifest{Schema: "full-window-actual-ready.v3", Scope: fullWindowScope{Schema: "full-window-scope.v3", BaselineEvidenceSHA256: proof.evidenceSHA256, BudgetApprovalSHA256: windowBudget105ApprovalSHA}, BaselineEvidence: &windowActualFile{proof.evidencePath, proof.evidenceSHA256}, HistoricalClaims: cloneWindowHistoricalClaims(proof.base.ClaimSHA256), ClaimsDir: proof.historicalDir, CampaignRoot: filepath.Join(root, "new-campaign"), SessionRoot: filepath.Join(root, "new-session")}
	return m, proof
}

func TestActualV3BindingRejectsCrossedProofBudgetAndEitherOldRoot(t *testing.T) {
	for _, mode := range []string{"old ready", "old scope", "wrong approval", "missing file", "wrong scope sha", "wrong file sha", "wrong file path", "wrong historical directory", "missing sixteenth claim", "old parent total", "first baseline", "first liability", "first history directory", "first root relative", "second root relative", "first campaign overlap", "first session contains", "second campaign overlap", "second session contains", "crossed predecessors"} {
		t.Run(mode, func(t *testing.T) {
			m, p := actualV3BindingFixture(t)
			switch mode {
			case "old ready":
				m.Schema = "full-window-actual-ready.v2"
			case "old scope":
				m.Scope.Schema = "full-window-scope.v2"
			case "wrong approval":
				m.Scope.BudgetApprovalSHA256 = strings.Repeat("9", 64)
			case "missing file":
				m.BaselineEvidence = nil
			case "wrong scope sha":
				m.Scope.BaselineEvidenceSHA256 = strings.Repeat("9", 64)
			case "wrong file sha":
				m.BaselineEvidence.SHA256 = strings.Repeat("9", 64)
			case "wrong file path":
				m.BaselineEvidence.Path += ".other"
			case "wrong historical directory":
				m.ClaimsDir += ".other"
			case "missing sixteenth claim":
				delete(m.HistoricalClaims, "full1220-v2-20261028/window-000006")
			case "old parent total":
				p.base.TotalModel = 100000000
			case "first baseline":
				p.first.base.PermanentModel--
			case "first liability":
				p.first.liabilityMicroCNY = 0
			case "first history directory":
				p.first.historicalDir += ".other"
			case "first root relative":
				p.first.originalRoot = "relative"
			case "second root relative":
				p.originalRoot = "relative"
			case "first campaign overlap":
				m.CampaignRoot = filepath.Join(p.first.originalRoot, "child")
			case "first session contains":
				m.SessionRoot = filepath.Dir(p.first.originalRoot)
			case "second campaign overlap":
				m.CampaignRoot = filepath.Join(p.originalRoot, "child")
			case "second session contains":
				m.SessionRoot = filepath.Dir(p.originalRoot)
			case "crossed predecessors":
				p.first.originalRoot = p.originalRoot
			}
			if validateWindowActualStoppedBindingV3(m, p) == nil {
				t.Fatal("crossed v3 binding accepted")
			}
		})
	}
}

func TestActualV3ConsistentMemoryFixtureCannotReplaceConcreteFileProof(t *testing.T) {
	m, p := actualV3BindingFixture(t)
	r := &FullWindowActualReady{manifestRaw: []byte(`{}`), manifest: m, baseline: p.base, stoppedV2: p}
	if s, err := OpenFullWindowActual(context.Background(), r, "/never-read-settings"); err == nil || s != nil || !strings.Contains(err.Error(), "v4 scope required") {
		t.Fatal("consumed v3 reached settings or claim", err)
	}
	if err := (&FullWindowActualSession{ready: r}).verifyInputs(); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatal("window accepted memory proof", err)
	}
}

func TestActualV3BindingKeepsSixteenClaimsAndBothStoppedRoots(t *testing.T) {
	m, p := actualV3BindingFixture(t)
	if err := validateWindowActualStoppedBindingV3(m, p); err != nil {
		t.Fatalf("consistent v3 binding rejected: %v", err)
	}
}

func TestActualEpochVersionsMustMatchAndRetainV2Readonly(t *testing.T) {
	for _, tc := range []struct {
		ready, scope string
		accept       bool
	}{
		{"v3", "v3", true}, {"v2", "v2", true}, {"v1", "v1", false}, {"v3", "v2", false}, {"v2", "v3", false}, {"v3", "v1", false}, {"", "", false},
	} {
		m := windowActualManifest{Schema: "full-window-actual-ready." + tc.ready, Scope: fullWindowScope{Schema: "full-window-scope." + tc.scope}}
		if windowActualSupportedEpoch(m) != tc.accept {
			t.Fatalf("epoch mismatch ready=%s scope=%s accepted=%v", tc.ready, tc.scope, !tc.accept)
		}
	}
}
