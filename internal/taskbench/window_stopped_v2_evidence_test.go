package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoppedV2RequiredArtifactPinsCoverSixConsumedWindows(t *testing.T) {
	names := []string{"actual-manifest.json", "records/startup.json", "records/terminal.json"}
	for i := 1; i <= 6; i++ {
		names = append(names, fmt.Sprintf("campaign/claim-%06d.json", i), fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i), fmt.Sprintf("records/window-%06d-summary.json", i), fmt.Sprintf("records/window-%06d/receipt.json", i), fmt.Sprintf("records/window-%06d/payload-manifest.json", i))
	}
	for i := 1; i <= 5; i++ {
		names = append(names, fmt.Sprintf("campaign/checkpoint-%06d.json", i))
	}
	pins := windowStoppedV2ArtifactPins()
	if len(pins) != 38 {
		t.Fatalf("six-window pin set incomplete: got %d want 38", len(pins))
	}
	for _, name := range names {
		if !comparisonDigest(pins[name]) {
			t.Fatal("required predecessor omitted", name)
		}
	}
	if pins["actual-manifest.json"] != "5cd8f9d038e93c3be4c62c3e14a3100850d0e9b03415bd5c65d9f133a2a74071" || pins["campaign/claim-000006.json"] != "0f074c6761a898c46ba831b4a96f9818d27093955a5fb42ae8e662f745b173aa" {
		t.Fatal("different predecessor accepted")
	}
	delete(pins, "campaign/claim-000006.json")
	if len(windowStoppedV2ArtifactPins()) != 38 {
		t.Fatal("pin snapshot shared mutable state")
	}
}

func TestStoppedV2NamespaceRejectsCrossedRootsHistoryAndConfiguration(t *testing.T) {
	first := windowStoppedBaseline{base: expectedStoppedWindowBaseline(), originalRoot: "/old/first", historicalDir: "/old/claims"}
	e := windowStoppedV2Evidence{Schema: "full-window-stopped-baseline.v2", OriginalRoot: "/old/second", FirstBaseline: windowActualFile{Path: "/old/first-proof.json", SHA256: "4c55d694405c61948857dfd72279118243e5ea4dbd2de72b6254687ab08d1dbf"}}
	m := windowActualManifest{Schema: "full-window-actual-ready.v2", Scope: fullWindowScope{Schema: "full-window-scope.v2"}, ArchiveCommit: "3eb22933050d61ff57068964e42a3312bb5d4eec", EvidenceKind: "actual", KnowledgeModel: "off", KnowledgeJEV: "off", SessionRoot: "/old/second/records", CampaignRoot: "/old/second/campaign", ClaimsDir: first.historicalDir, BaselineEvidence: &e.FirstBaseline, HistoricalClaims: cloneWindowHistoricalClaims(first.base.ClaimSHA256)}
	if err := validateWindowStoppedV2Namespace(m, e, first); err != nil {
		t.Fatal("original namespace rejected", err)
	}
	for _, mutate := range []func(*windowActualManifest){
		func(m *windowActualManifest) { m.Schema = "full-window-actual-ready.v1" },
		func(m *windowActualManifest) { m.Scope.Schema = "full-window-scope.v3" },
		func(m *windowActualManifest) { m.Scope.AllowPaid = true },
		func(m *windowActualManifest) { m.KnowledgeJEV = "resident" },
		func(m *windowActualManifest) { m.SessionRoot = "/new/records" },
		func(m *windowActualManifest) { m.CampaignRoot = "/new/campaign" },
		func(m *windowActualManifest) { m.ClaimsDir = "/other/claims" },
		func(m *windowActualManifest) { m.BaselineEvidence = nil },
		func(m *windowActualManifest) { delete(m.HistoricalClaims, windowStoppedClaimID) },
	} {
		raw, _ := json.Marshal(m)
		var bad windowActualManifest
		json.Unmarshal(raw, &bad)
		mutate(&bad)
		if validateWindowStoppedV2Namespace(bad, e, first) == nil {
			t.Fatal("crossed predecessor accepted")
		}
	}
	if !reflect.DeepEqual(m.HistoricalClaims, first.base.ClaimSHA256) {
		t.Fatal("original history mutated")
	}
}

func TestStoppedV2DescriptorRejectsMissingApprovalWithoutCreatingOwners(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	for _, sha := range []string{"", windowBudget105ApprovalSHA} {
		e := windowStoppedV2Evidence{Schema: "full-window-stopped-baseline.v2", OriginalRoot: filepath.Join(root, "absent-owner"), FirstBaseline: windowActualFile{Path: filepath.Join(root, "missing-first-proof"), SHA256: "4c55d694405c61948857dfd72279118243e5ea4dbd2de72b6254687ab08d1dbf"}, BudgetApproval: windowActualFile{Path: filepath.Join(root, "missing-approval"), SHA256: sha}}
		raw, _ := json.Marshal(e)
		path := filepath.Join(root, "descriptor.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadWindowStoppedV2Baseline(path, conversationHash(raw)); err == nil {
			t.Fatal("unverified predecessor accepted")
		}
		if _, err := os.Lstat(e.OriginalRoot); !os.IsNotExist(err) {
			t.Fatal("reader created owner root")
		}
	}
}
