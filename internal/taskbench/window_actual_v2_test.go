package taskbench

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualV2ManifestPreservesStoppedEvidenceBinding(t *testing.T) {
	raw := []byte(`{"schema":"full-window-actual-ready.v2","baseline_evidence":{"path":"/private/stopped.json","sha256":"` + strings.Repeat("8", 64) + `"}}`)
	var m windowActualManifest
	if err := decodeWindowActualJSON(raw, &m); err != nil {
		t.Fatal("v2 manifest cannot carry its stopped evidence", err)
	}
	encoded, err := json.Marshal(m)
	if err != nil || !strings.Contains(string(encoded), `"baseline_evidence":{"path":"/private/stopped.json"`) {
		t.Fatal("stopped binding lost", err)
	}
	old, err := json.Marshal(windowActualManifest{Schema: "full-window-actual-ready.v1"})
	if err != nil || strings.Contains(string(old), "baseline_evidence") {
		t.Fatal("legacy manifest serialization changed", err)
	}
}

func TestActualCurrentEpochRequiresStoppedProofBeforeOpenAndWindowInputs(t *testing.T) {
	r := &FullWindowActualReady{manifestRaw: []byte(`{}`), manifest: windowActualManifest{Schema: "full-window-actual-ready.v4", Scope: fullWindowScope{Schema: "full-window-scope.v4"}}}
	if s, err := OpenFullWindowActual(context.Background(), r, "/never-read-settings"); err == nil || s != nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatal("Open did not check stopped evidence first", err)
	}
	s := &FullWindowActualSession{ready: r}
	if err := s.verifyInputs(); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatal("window input verification did not check stopped evidence", err)
	}
}

// These consistency fixtures cannot mint readiness or bypass the file loader.
func TestActualV2StoppedBindingRejectsCrossedEpochAndRoots(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	proof := windowStoppedBaseline{base: expectedStoppedWindowBaseline(), evidencePath: filepath.Join(root, "stopped.json"), evidenceSHA256: strings.Repeat("8", 64), originalRoot: filepath.Join(root, "original"), historicalDir: filepath.Join(root, "history"), liabilityMicroCNY: 3832}
	m := windowActualManifest{Schema: "full-window-actual-ready.v2", Scope: fullWindowScope{Schema: "full-window-scope.v2", BaselineEvidenceSHA256: proof.evidenceSHA256}, BaselineEvidence: &windowActualFile{proof.evidencePath, proof.evidenceSHA256}, HistoricalClaims: cloneWindowHistoricalClaims(proof.base.ClaimSHA256), ClaimsDir: proof.historicalDir, CampaignRoot: filepath.Join(root, "new-campaign"), SessionRoot: filepath.Join(root, "new-session")}
	if err := validateWindowActualStoppedBinding(m, proof); err != nil {
		t.Fatal("consistent v2 binding rejected", err)
	}
	for _, name := range []string{"v1 ready", "v1 scope", "missing file", "scope sha", "file sha", "file path", "history dir", "ninth baseline", "tenth claim", "liability", "campaign inside old", "session contains old", "relative old root"} {
		t.Run(name, func(t *testing.T) {
			v, p := m, proof
			v.HistoricalClaims = cloneWindowHistoricalClaims(m.HistoricalClaims)
			file := *m.BaselineEvidence
			v.BaselineEvidence = &file
			switch name {
			case "v1 ready":
				v.Schema = "full-window-actual-ready.v1"
			case "v1 scope":
				v.Scope.Schema = "full-window-scope.v1"
			case "missing file":
				v.BaselineEvidence = nil
			case "scope sha":
				v.Scope.BaselineEvidenceSHA256 = strings.Repeat("9", 64)
			case "file sha":
				file.SHA256 = strings.Repeat("9", 64)
			case "file path":
				file.Path += ".other"
			case "history dir":
				v.ClaimsDir += ".other"
			case "ninth baseline":
				p.base.PermanentModel = 29061120
			case "tenth claim":
				delete(v.HistoricalClaims, windowStoppedClaimID)
			case "liability":
				p.liabilityMicroCNY = 0
			case "campaign inside old":
				v.CampaignRoot = filepath.Join(proof.originalRoot, "child")
			case "session contains old":
				v.SessionRoot = root
			case "relative old root":
				p.originalRoot = "relative"
			}
			if validateWindowActualStoppedBinding(v, p) == nil {
				t.Fatal("crossed stopped binding accepted")
			}
		})
	}
}
