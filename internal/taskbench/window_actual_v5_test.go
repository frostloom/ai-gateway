package taskbench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualV5EpochAndConsumedOpenBoundary(t *testing.T) {
	for _, a := range []string{"v1", "v2", "v3", "v4", "v5"} {
		for _, b := range []string{"v1", "v2", "v3", "v4", "v5"} {
			m := windowActualManifest{Schema: "full-window-actual-ready." + a, Scope: fullWindowScope{Schema: "full-window-scope." + b}}
			if windowActualSupportedEpoch(m) != (a == b && a != "v1") {
				t.Errorf("readonly epoch %s/%s", a, b)
			}
		}
	}
	for _, a := range []string{"v1", "v2", "v3", "v4"} {
		r := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready." + a, Scope: fullWindowScope{Schema: "full-window-scope." + a}}}
		s, e := OpenFullWindowActual(context.Background(), r, "/never-read-settings")
		if s != nil || e == nil || !strings.Contains(e.Error(), "v9 delegated authority required before settings") {
			t.Errorf("consumed %s did not reject at epoch boundary: %v", a, e)
		}
	}
}

func actualV5BindingFixture(t *testing.T) (windowActualManifest, windowStoppedV4Baseline) {
	t.Helper()
	m, previous := actualV4BindingFixture(t)
	p := windowStoppedV4Baseline{base: expectedStoppedV4WindowBaseline(), previous: previous, evidencePath: filepath.Join(stoppedEvidenceRoot(t), "missing28.json"), evidenceSHA256: windowV5BaselineSHA, originalRoot: filepath.Join(stoppedEvidenceRoot(t), "old-fourth"), historicalDir: previous.historicalDir}
	source, profiles, b := windowScopeInputs(t)
	scope, e := prepareFullWindowScopeV5(source, profiles, 28, windowV5TestCaps(t), b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
	if e != nil {
		t.Fatal(e)
	}
	m.Schema = "full-window-actual-ready.v5"
	m.Scope = scope
	m.BaselineEvidence = &windowActualFile{p.evidencePath, p.evidenceSHA256}
	m.HistoricalClaims = cloneWindowHistoricalClaims(p.base.ClaimSHA256)
	return m, p
}
func TestActualV5ConcreteBindingAndFreshBoundary(t *testing.T) {
	m, p := actualV5BindingFixture(t)
	if e := validateWindowActualStoppedBindingV5(m, p); e != nil {
		t.Fatal("consistent28 data mirror rejected", e)
	}
	r := &FullWindowActualReady{manifestRaw: []byte(`{}`), manifest: m, baseline: p.base, stoppedV4: p}
	if s, e := OpenFullWindowActual(context.Background(), r, "/never-read-settings"); s != nil || e == nil || !strings.Contains(e.Error(), "v9 delegated authority required before settings") {
		t.Fatal("coherent memory mirror reached settings", e)
	}
	if e := (&FullWindowActualSession{ready: r}).verifyInputs(); e == nil || !strings.Contains(e.Error(), "stopped") {
		t.Fatal("window bypassed fresh files", e)
	}
}
func TestActualV5RejectsChangedBindingAndEveryFourthRoot(t *testing.T) {
	for _, mode := range []string{"ready4", "scope4", "approval", "evidence", "fileSHA", "filePath", "historyDir", "claim3", "old25", "parent", "calls", "previousSHA", "previous25", "previous16", "liability", "cache", "miss", "jev", "cap"} {
		t.Run(mode, func(t *testing.T) {
			m, p := actualV5BindingFixture(t)
			switch mode {
			case "ready4":
				m.Schema = "full-window-actual-ready.v4"
			case "scope4":
				m.Scope.Schema = "full-window-scope.v4"
			case "approval":
				m.Scope.BudgetApprovalSHA256 = "bad"
			case "evidence":
				m.BaselineEvidence = nil
			case "fileSHA":
				m.BaselineEvidence.SHA256 = "bad"
			case "filePath":
				m.BaselineEvidence.Path += "other"
			case "historyDir":
				m.ClaimsDir += "other"
			case "claim3":
				delete(m.HistoricalClaims, "full1220-v4-20261029/window-000003")
			case "old25":
				p.base = expectedStoppedV3WindowBaseline()
			case "parent":
				p.base.TotalModel = 100000000
			case "calls":
				p.base.ActualModelCalls--
			case "previousSHA":
				p.previous.evidenceSHA256 = "bad"
			case "previous25":
				p.previous.base.PermanentModel--
			case "previous16":
				p.previous.previous.base.PermanentModel--
			case "liability":
				p.previous.previous.first.liabilityMicroCNY = 0
			case "cache":
				m.Limits.Model.CacheHitInputRate = 0
			case "miss":
				m.Limits.Model.InputRate--
			case "jev":
				m.Limits.JEV.CacheHitInputRate = 1
			case "cap":
				m.Scope.Windows[0].ModelCap--
			}
			if validateWindowActualStoppedBindingV5(m, p) == nil {
				t.Fatal("changed binding accepted")
			}
		})
	}
	for i := 0; i < 4; i++ {
		for _, which := range []string{"session", "campaign"} {
			for _, relation := range []string{"same", "parent", "child"} {
				t.Run(string(rune('1'+i))+which+relation, func(t *testing.T) {
					m, p := actualV5BindingFixture(t)
					root := p.windowStoppedPredecessorRootsV5()[i]
					if relation == "parent" {
						root = filepath.Dir(root)
					}
					if relation == "child" {
						root = filepath.Join(root, "child")
					}
					if which == "session" {
						m.SessionRoot = root
					} else {
						m.CampaignRoot = root
					}
					if validateWindowActualStoppedBindingV5(m, p) == nil {
						t.Fatal("crossed historical root accepted")
					}
				})
			}
		}
	}
}

func TestActualV5FourthRootCannotAliasPredecessors(t *testing.T) {
	for i := 0; i < 3; i++ {
		m, p := actualV5BindingFixture(t)
		p.originalRoot = p.windowStoppedPredecessorRootsV5()[i]
		if validateWindowActualStoppedBindingV5(m, p) == nil {
			t.Fatal("fourth root crossed", i)
		}
	}
	m, p := actualV5BindingFixture(t)
	p.originalRoot = "relative"
	if validateWindowActualStoppedBindingV5(m, p) == nil {
		t.Fatal("relative fourth root")
	}
}
