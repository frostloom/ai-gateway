package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualV7RejectsConsumedEpochBeforeSettings(t *testing.T) {
	for _, epoch := range []string{"v1", "v2", "v3", "v4", "v5", "v6"} {
		m := windowActualManifest{Schema: "full-window-actual-ready." + epoch, Scope: fullWindowScope{Schema: "full-window-scope." + epoch}}
		s, err := OpenFullWindowActual(context.Background(), &FullWindowActualReady{manifest: m}, "/never-read-settings")
		if s != nil || err == nil || !strings.Contains(err.Error(), "v7 scope required") {
			t.Errorf("consumed %s must stop before settings: %v", epoch, err)
		}
	}
}

func TestActualV7ReadonlyEpochPairing(t *testing.T) {
	for _, a := range []string{"v2", "v3", "v4", "v5", "v6", "v7"} {
		for _, b := range []string{"v2", "v3", "v4", "v5", "v6", "v7"} {
			m := windowActualManifest{Schema: "full-window-actual-ready." + a, Scope: fullWindowScope{Schema: "full-window-scope." + b}}
			if windowActualSupportedEpoch(m) != (a == b) {
				t.Errorf("epoch %s/%s", a, b)
			}
		}
	}
}

func TestActualV7SettingsTimeoutIsolation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "settings")
	raw := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	for _, tc := range []struct {
		ms           string
		oldOK, newOK bool
	}{{"2000", true, false}, {"10000", false, true}, {"9999", false, false}} {
		contents := strings.Replace(raw, `"export_timeout_ms":2000`, `"export_timeout_ms":`+tc.ms, 1)
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		_, oldErr := readWindowActualSettings(path)
		_, newErr := readWindowActualSettingsForTimeout(path, 10000)
		if (oldErr == nil) != tc.oldOK || (newErr == nil) != tc.newOK {
			t.Fatalf("timeout %s old=%v new=%v", tc.ms, oldErr, newErr)
		}
	}
}
func TestActualV7ConcreteStoppedBinding(t *testing.T) {
	m, previous := actualV6BindingFixture(t)
	proof := windowStoppedV6Baseline{base: expectedStoppedV6WindowBaseline(), previous: previous, evidencePath: filepath.Join(stoppedEvidenceRoot(t), "missing33.json"), evidenceSHA256: windowV7BaselineSHA, originalRoot: filepath.Join(stoppedEvidenceRoot(t), "old-sixth"), historicalDir: previous.historicalDir}
	source, profiles, bindings := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV7(source, profiles, 28, windowV7TestCaps(), bindings, windowV7BaselineSHA, windowBudget130ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	m.Schema = "full-window-actual-ready.v7"
	m.Scope = scope
	m.BaselineEvidence = &windowActualFile{proof.evidencePath, proof.evidenceSHA256}
	m.HistoricalClaims = cloneWindowHistoricalClaims(proof.base.ClaimSHA256)
	if err = validateWindowActualStoppedBindingV7(m, proof); err != nil {
		t.Fatal(err)
	}
	for _, root := range proof.windowStoppedPredecessorRootsV7() {
		n := m
		n.SessionRoot = root
		if validateWindowActualStoppedBindingV7(n, proof) == nil {
			t.Fatal("overlap accepted")
		}
	}
	n := m
	n.HistoricalClaims = cloneWindowHistoricalClaims(m.HistoricalClaims)
	for k := range n.HistoricalClaims {
		delete(n.HistoricalClaims, k)
		break
	}
	if validateWindowActualStoppedBindingV7(n, proof) == nil {
		t.Fatal("missing claim accepted")
	}
	if (&FullWindowActualReady{manifest: m, stoppedV6: proof, baseline: proof.base}).revalidateStoppedV7() == nil {
		t.Fatal("in-memory proof accepted")
	}
}
