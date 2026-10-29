package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStoppedV5Exact32Baseline(t *testing.T) {
	b := expectedStoppedV5WindowBaseline()
	if len(b.ClaimSHA256) != 32 || b.PermanentModel != 42961430 || b.PermanentJEV != 344493 || b.RemainingModel != 62038570 || b.RemainingJEV != 655507 || b.ActualModelCalls != 2292 || b.ActualJEVCalls != 585 {
		t.Fatalf("unexpected stopped R2 baseline: %+v", b)
	}
	pins := windowStoppedV5ArtifactPins()
	if len(pins) != 24 || pins["campaign/claim-000004.json"] != "8207bf55da28cfe920eac539fdf09e5cee1a61c5b54fe4b993647fcb4f47300a" {
		t.Fatal("R2 top pins changed")
	}
	raw, err := json.Marshal(b.ClaimSHA256)
	if err != nil || conversationHash(raw) != "7f6a967d739a1e338b69153413a5e9f06dcc3fb7d7bae05b64e2988686b5dc1b" {
		t.Fatal("R2 canonical 32-claim map changed", err)
	}
	b.ClaimSHA256["full1220-v5-20261029/window-000001"] = "changed"
	if fresh := expectedStoppedV5WindowBaseline(); fresh.ClaimSHA256["full1220-v5-20261029/window-000001"] != "e439a45da5d9df4d2eec2f1fd56e7df197fe3bf8c21ba7e1f4599379b1e6eaa5" {
		t.Fatal("claim maps share mutable state")
	}
}

func TestWindowStoppedV5CachedBaselineTamperingRejected(t *testing.T) {
	for _, mode := range []string{"claim", "model", "jev", "calls", "parent", "remaining"} {
		t.Run(mode, func(t *testing.T) {
			b := expectedStoppedV5WindowBaseline()
			switch mode {
			case "claim":
				delete(b.ClaimSHA256, "full1220-v5-20261029/window-000004")
			case "model":
				b.PermanentModel--
			case "jev":
				b.PermanentJEV--
			case "calls":
				b.ActualModelCalls--
			case "parent":
				b.TotalModel++
			case "remaining":
				b.RemainingJEV++
			}
			if _, _, _, err := (windowStoppedV5Baseline{base: b}).revalidateWindowStoppedHistory(); err == nil {
				t.Fatal("tampered baseline accepted")
			}
		})
	}
}

func TestWindowStoppedV5NativeRejectsChangedBody(t *testing.T) {
	dir, manifest, ledger := stoppedV4NativeFixture(t)
	if _, err := readWindowStoppedV5Native(dir, manifest, ledger); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "request-000001.body")
	if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowStoppedV5Native(dir, manifest, ledger); err == nil {
		t.Fatal("tampered native body accepted")
	}
}

func TestWindowStoppedV5NamespaceRejectsOldRootAndChangedHistory(t *testing.T) {
	fixture := func() (windowActualManifest, windowStoppedV5Evidence, windowStoppedV4Baseline) {
		_, _, p3 := stoppedV4NamespaceFixture()
		p := windowStoppedV4Baseline{base: expectedStoppedV4WindowBaseline(), previous: p3, originalRoot: "/old/fourth", historicalDir: p3.historicalDir, evidencePath: "/old/baseline-v4.json", evidenceSHA256: windowStoppedV5PreviousSHA}
		e := windowStoppedV5Evidence{Schema: "full-window-stopped-baseline.v5", PreviousBaseline: windowActualFile{Path: p.evidencePath, SHA256: p.evidenceSHA256}, OriginalRoot: "/old/fifth", BudgetApproval: windowActualFile{Path: "/old/approval.json", SHA256: windowBudget105ApprovalSHA}}
		m := windowActualManifest{Schema: "full-window-actual-ready.v5", EvidenceKind: "actual", ArchiveCommit: "740db25818eccebe8363b282a109a14ca610a0be", KnowledgeModel: "off", KnowledgeJEV: "off", CampaignRoot: "/old/fifth/campaign", SessionRoot: "/old/fifth/records", ClaimsDir: p.historicalDir, BaselineEvidence: &e.PreviousBaseline, HistoricalClaims: cloneWindowHistoricalClaims(p.base.ClaimSHA256), Scope: fullWindowScope{Schema: "full-window-scope.v5", BudgetApprovalSHA256: windowBudget105ApprovalSHA, BaselineEvidenceSHA256: p.evidenceSHA256}, Limits: windowV4ExpectedLimits()}
		return m, e, p
	}
	m, e, p := fixture()
	if err := validateWindowStoppedV5Namespace(m, e, p); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"root-alias", "changed-claim", "epoch", "paid", "budget"} {
		t.Run(mode, func(t *testing.T) {
			m, e, p := fixture()
			switch mode {
			case "root-alias":
				e.OriginalRoot = "/old/fourth/child"
			case "changed-claim":
				delete(p.base.ClaimSHA256, "full1220-v4-20261029/window-000003")
			case "epoch":
				m.Schema = "full-window-actual-ready.v4"
			case "paid":
				m.Scope.AllowPaid = true
			case "budget":
				m.Scope.BudgetApprovalSHA256 = "changed"
			}
			if validateWindowStoppedV5Namespace(m, e, p) == nil {
				t.Fatal("invalid namespace accepted")
			}
		})
	}
	proof := windowStoppedV5Baseline{previous: p, originalRoot: e.OriginalRoot}
	if proof.windowStoppedPredecessorRootsV6() != [5]string{"/old/first", "/old/second", "/old/third", "/old/fourth", "/old/fifth"} {
		t.Fatal("incorrect predecessor roots")
	}
	if _, ok := any(proof).(windowStoppedV4History); ok {
		t.Fatal("old history capability promoted")
	}
}
