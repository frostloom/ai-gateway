package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowCampaignV7CurrentApprovalFreshBeforeNextClaim(t *testing.T) {
	descriptor := "/Users/a1/项目/ai-gateway/.tmp/stopped-v6-proof-work/stopped-v6.private.json"
	raw, err := os.ReadFile(descriptor)
	if os.IsNotExist(err) {
		t.Skip("original stopped-v6 proof not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	var evidence windowStoppedV6Evidence
	if err = json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	approvalBytes, err := readWindowActualFile(evidence.BudgetApproval.Path, windowBudget130ApprovalSHA, 65536)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := loadWindowStoppedV6Baseline(descriptor, windowV7BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	p, base, _ := fixtureV7LocalStubCampaign(t)
	approvalPath := filepath.Join(filepath.Dir(p.root), "current-approval.json")
	if err = os.WriteFile(approvalPath, approvalBytes, 0600); err != nil {
		t.Fatal(err)
	}
	approval := windowActualFile{Path: approvalPath, SHA256: windowBudget130ApprovalSHA}
	history := windowV7CurrentHistory{stopped: proof, approval: approval}
	fresh, sha, _, err := history.revalidateWindowStoppedHistory()
	if err != nil || sha != windowV7BaselineSHA || fresh.PermanentModel != base.PermanentModel {
		t.Fatal("unchanged copy rejected", err)
	}
	for _, mutation := range []string{"remove", "change"} {
		t.Run(mutation, func(t *testing.T) {
			p, base, _ := fixtureV7LocalStubCampaign(t)
			current := filepath.Join(filepath.Dir(p.root), "current-approval.json")
			if err := os.WriteFile(current, approvalBytes, 0600); err != nil {
				t.Fatal(err)
			}
			c, err := startWindowCampaign(p, base)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			c.prepared.stoppedHistory = windowV7CurrentHistory{stopped: proof, approval: windowActualFile{Path: current, SHA256: windowBudget130ApprovalSHA}}
			if mutation == "remove" {
				err = os.Remove(current)
			} else {
				err = os.WriteFile(current, []byte("changed"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err == nil {
				t.Fatal("changed current approval accepted")
			}
			if _, err = os.Stat(filepath.Join(p.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("claim 2 exists", err)
			}
		})
	}
}

func TestWindowV7CurrentApprovalRejectsMissingOrMismatch(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	path := filepath.Join(root, "approval")
	h := windowV7CurrentHistory{approval: windowActualFile{Path: path, SHA256: windowBudget130ApprovalSHA}}
	if _, _, _, err := h.revalidateWindowStoppedHistory(); err == nil || !strings.Contains(err.Error(), "current approval changed") {
		t.Fatal("missing approval accepted", err)
	}
	if err := os.WriteFile(path, []byte("mismatch"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := h.revalidateWindowStoppedHistory(); err == nil || !strings.Contains(err.Error(), "current approval changed") {
		t.Fatal("modified approval accepted", err)
	}
}
