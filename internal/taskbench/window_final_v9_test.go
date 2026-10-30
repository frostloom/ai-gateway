package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Exercises the real final advance branch with test-only ownership and zero
// native requests. It does not simulate acquisition of the preceding510 windows.
func TestWindowV9FinalAdvanceReturnsCheckpointWithoutClaim512(t *testing.T) {
	a, _ := fixtureWindowV9Authority(t)
	scope := a.manifest.Scope
	root := filepath.Join(filepath.Dir(a.authorizationPath), "synthetic-final-window-only")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	index := len(scope.Windows)
	contract := contractWindowCampaign(scope, a.binding.ScopeSHA256, scope.Windows[index-1], windowV4ExpectedLimits())
	dir := filepath.Join(root, "ledger-000511")
	ledger, err := newWindowAccounting(dir, contract)
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	base := windowV9CandidateBaseline(expectedStoppedV8WindowBaseline())
	c := &windowCampaign{root: root, rootInfo: info, lock: lock, prepared: windowCampaignPrepared{root: root, scope: scope, scopeSHA: a.binding.ScopeSHA256, executionSHA: a.binding.ExecutionSHA256}, baseline: base, index: index, claimSHA: contract.ClaimSHA256, model: base.PermanentModel + 123827032, jev: base.PermanentJEV + 2037474, files: map[string]string{}, ledgerAudits: map[string]string{}, ledger: ledger, ledgerDir: dir}
	defer c.closeWindowCampaign()
	p := &windowCampaignOwnedCheckpoint{owner: c, ledger: ledger, index: index}
	w := scope.Windows[index-1]
	for _, s := range scope.Slots[w.FirstOrdinal-1 : w.LastOrdinal] {
		for _, arm := range s.ArmOrder {
			p.evidence = append(p.evidence, windowCampaignArmEvidence{Ordinal: s.Ordinal, Pass: s.Pass, ObservedTurns: s.UserTurns, Key: s.CaseKey, Arm: arm, Cleanup: true, Observer: true, RecordingSHA256: strings.Repeat("a", 64)})
		}
	}
	token, err := c.advanceWindowCampaignContext(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "checkpoint-000511.json"))
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token != conversationHash(raw) || token != c.files["checkpoint-000511.json"] || c.index != 511 || !c.poisoned {
		t.Fatal("final checkpoint token contract changed")
	}
	if _, err = os.Lstat(filepath.Join(root, "claim-000512.json")); !os.IsNotExist(err) {
		t.Fatal("final window created claim512")
	}
	if out := os.Getenv("WINDOW_V9_EXPORT_SYNTHETIC_FINAL"); out != "" {
		value := map[string]any{"evidence_kind": "synthetic_final_private_advance_branch_not_full511_acquisition", "index": 511, "next_claim_sha256": token, "checkpoint_sha256": conversationHash(raw), "checkpoint_utf8": string(raw), "claim512_absent": true, "actual_model_JEV_calls": 0, "actual_financial_claims": 0}
		body, _ := json.MarshalIndent(value, "", "  ")
		if err = os.WriteFile(out, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
