package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticRechargeClaimPredecessorAndPermanentBudget(t *testing.T) {
	root := t.TempDir()
	base := "../../eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry"
	p := syntheticRetestPilotAt(t, base, strings.Repeat("a", 40), false)
	newPath := filepath.Join(root, ".tmp", "paired-live-retests", "recharge-confirm-diagnostic-retest-20261023.json")
	if claimRetest(root, p, "first") == nil {
		t.Fatal("missing parent claim accepted")
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatal("new claim created before parent validation")
	}
	parent := filepath.Join(root, ".tmp", "paired-live-retests", "recharge-confirm-20261023.json")
	os.MkdirAll(filepath.Dir(parent), 0700)
	os.WriteFile(parent, []byte("tampered"), 0600)
	if claimRetest(root, p, "first") == nil {
		t.Fatal("tampered parent claim accepted")
	}
	raw, err := os.ReadFile(filepath.Join(base, "prior-claim.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(parent, raw, 0600)
	if err := claimRetest(root, p, "first"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(got, &m)
	if m["prior_claim_sha256"] != "2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc" || m["cumulative_reserved_model_micro_cny"] != float64(675840) || m["cumulative_reserved_jev_micro_usd"] != float64(5376) {
		t.Fatal("cumulative permanent reservation lost", m)
	}
	changed := syntheticRetestPilotAt(t, base, strings.Repeat("b", 40), true)
	if claimRetest(root, changed, "new-output") == nil {
		t.Fatal("new output/revision reset claim")
	}
	after, _ := os.ReadFile(parent)
	if !bytes.Equal(raw, after) {
		t.Fatal("parent claim modified")
	}
	after, _ = os.ReadFile(newPath)
	if !bytes.Equal(got, after) {
		t.Fatal("new claim overwritten")
	}
}
func TestDiagnosticRechargeUnreadyDoesNotReadSecrets(t *testing.T) {
	root := t.TempDir()
	base := "../../eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry"
	output := filepath.Join(root, "absent")
	args := []string{"-retest", "-authorization", filepath.Join(base, "authorization.example.json"), "-plan", filepath.Join(base, "plan.json"), "-billing-evidence", filepath.Join(base, "billing.example.json"), "-env-file", filepath.Join(root, "missing-secret"), "-out", output}
	var out bytes.Buffer
	if runWithOutput(args, &out) != 0 {
		t.Fatal(out.String())
	}
	var m map[string]any
	json.Unmarshal(out.Bytes(), &m)
	if m["ready_for_paid_run"] != false || m["human_authorized"] != false {
		t.Fatal(out.String())
	}
	out.Reset()
	if runWithOutput(append(args, "-execute"), &out) != 2 || !strings.Contains(out.String(), "before credentials or fixtures") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("output created")
	}
}
