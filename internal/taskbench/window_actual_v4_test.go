package taskbench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Data-only consistency mirror: it has no pinned files and cannot mint ready.
func actualV4BindingFixture(t *testing.T) (windowActualManifest, windowStoppedV3Baseline) {
	t.Helper()
	m, previous := actualV3BindingFixture(t)
	previous.evidenceSHA256 = windowStoppedV3PreviousSHA
	proof := windowStoppedV3Baseline{base: expectedStoppedV3WindowBaseline(), previous: previous, evidencePath: filepath.Join(stoppedEvidenceRoot(t), "absent-proof.json"), evidenceSHA256: windowV4BaselineSHA, originalRoot: filepath.Join(stoppedEvidenceRoot(t), "old-third"), historicalDir: previous.historicalDir}
	m.Schema, m.Scope.Schema = "full-window-actual-ready.v4", "full-window-scope.v4"
	m.Scope.BaselineEvidenceSHA256 = windowV4BaselineSHA
	m.BaselineEvidence = &windowActualFile{proof.evidencePath, windowV4BaselineSHA}
	m.HistoricalClaims = cloneWindowHistoricalClaims(proof.base.ClaimSHA256)
	m.Limits = windowV4ExpectedLimits()
	return m, proof
}

func TestActualV4ConsistencyPreservesEveryHistoricalClaimAndFixedCachePrice(t *testing.T) {
	m, proof := actualV4BindingFixture(t)
	if err := validateWindowActualStoppedBindingV4(m, proof); err != nil {
		t.Fatal("consistent data mirror rejected", err)
	}
	if len(m.HistoricalClaims) != 25 || proof.base.PermanentModel != 38888170 || proof.base.PermanentJEV != 308003 {
		t.Fatal("historical denominator changed")
	}
	r := &FullWindowActualReady{manifestRaw: []byte(`{}`), manifest: m, baseline: proof.base, stoppedV3: proof}
	if s, err := OpenFullWindowActual(context.Background(), r, "/never-read-settings"); s != nil || err == nil || !strings.Contains(err.Error(), "v6 scope required") {
		t.Fatal("memory mirror reached settings or claim", err)
	}
	if err := (&FullWindowActualSession{ready: r}).verifyInputs(); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatal("memory mirror reached window input verification", err)
	}
}

func TestActualV4ConsistencyRejectsChangedHistoryRootsAndTariffs(t *testing.T) {
	for _, mode := range []string{"old ready", "old scope", "approval", "missing evidence", "baseline sha", "file sha", "file path", "claims dir", "missing25", "old16", "parent", "previous baseline", "previous descriptor", "previous history", "first baseline", "first liability", "first history", "cache absent", "cache wrong", "jev cache", "miss", "output", "model alias", "byte envelope", "token envelope", "output envelope", "first relative", "second relative", "third relative", "first session", "second session", "third session", "first campaign", "second campaign", "third campaign", "cross12", "cross13", "cross23"} {
		t.Run(mode, func(t *testing.T) {
			m, p := actualV4BindingFixture(t)
			switch mode {
			case "old ready":
				m.Schema = "full-window-actual-ready.v3"
			case "old scope":
				m.Scope.Schema = "full-window-scope.v3"
			case "approval":
				m.Scope.BudgetApprovalSHA256 = strings.Repeat("9", 64)
			case "missing evidence":
				m.BaselineEvidence = nil
			case "baseline sha":
				m.Scope.BaselineEvidenceSHA256 = strings.Repeat("9", 64)
			case "file sha":
				m.BaselineEvidence.SHA256 = strings.Repeat("9", 64)
			case "file path":
				m.BaselineEvidence.Path += ".other"
			case "claims dir":
				m.ClaimsDir += ".other"
			case "missing25":
				delete(m.HistoricalClaims, "full1220-v3-20261028/window-000009")
			case "old16":
				p.base = expectedStoppedV2WindowBaseline()
			case "parent":
				p.base.TotalModel = 100000000
			case "previous baseline":
				p.previous.base.PermanentModel--
			case "previous descriptor":
				p.previous.evidenceSHA256 = strings.Repeat("9", 64)
			case "previous history":
				p.previous.historicalDir += ".other"
			case "first baseline":
				p.previous.first.base.PermanentModel--
			case "first liability":
				p.previous.first.liabilityMicroCNY = 0
			case "first history":
				p.previous.first.historicalDir += ".other"
			case "cache absent":
				m.Limits.Model.CacheHitInputRate = 0
			case "cache wrong":
				m.Limits.Model.CacheHitInputRate++
			case "jev cache":
				m.Limits.JEV.CacheHitInputRate = 1
			case "miss":
				m.Limits.Model.InputRate--
			case "output":
				m.Limits.Model.OutputRate--
			case "model alias":
				m.Limits.Model.Model = "other"
			case "byte envelope":
				m.Limits.Model.MaxRequestBytes++
			case "token envelope":
				m.Limits.Model.MaxInput++
			case "output envelope":
				m.Limits.Model.MaxOutput++
			case "first relative":
				p.previous.first.originalRoot = "relative"
			case "second relative":
				p.previous.originalRoot = "relative"
			case "third relative":
				p.originalRoot = "relative"
			case "first session":
				m.SessionRoot = p.previous.first.originalRoot
			case "second session":
				m.SessionRoot = p.previous.originalRoot
			case "third session":
				m.SessionRoot = p.originalRoot
			case "first campaign":
				m.CampaignRoot = p.previous.first.originalRoot
			case "second campaign":
				m.CampaignRoot = p.previous.originalRoot
			case "third campaign":
				m.CampaignRoot = p.originalRoot
			case "cross12":
				p.previous.first.originalRoot = p.previous.originalRoot
			case "cross13":
				p.previous.first.originalRoot = p.originalRoot
			case "cross23":
				p.previous.originalRoot = p.originalRoot
			}
			if validateWindowActualStoppedBindingV4(m, p) == nil {
				t.Fatal("changed binding accepted")
			}
		})
	}
}

func TestActualV4EpochSupportedWithoutCrossingHistoricalReadiness(t *testing.T) {
	for _, ready := range []string{"v1", "v2", "v3", "v4"} {
		for _, scope := range []string{"v1", "v2", "v3", "v4"} {
			m := windowActualManifest{Schema: "full-window-actual-ready." + ready, Scope: fullWindowScope{Schema: "full-window-scope." + scope}}
			want := ready == scope && ready != "v1"
			if got := windowActualSupportedEpoch(m); got != want {
				t.Fatalf("read-only epoch pairing %s/%s got %t want %t", ready, scope, got, want)
			}
		}
	}
}

func TestActualV4OpenRejectsEveryConsumedEpochBeforeSettings(t *testing.T) {
	for _, epoch := range []string{"v1", "v2", "v3"} {
		r := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready." + epoch, Scope: fullWindowScope{Schema: "full-window-scope." + epoch}}}
		s, err := OpenFullWindowActual(context.Background(), r, "/never-read-settings")
		if s != nil || err == nil || !strings.Contains(err.Error(), "v6 scope required") {
			t.Fatalf("consumed epoch %s reached proof/settings boundary: %v", epoch, err)
		}
	}
}
