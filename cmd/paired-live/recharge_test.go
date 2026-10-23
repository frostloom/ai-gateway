package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRechargeEntryClaimPermanentAndIndependent(t *testing.T) {
	root := t.TempDir()
	p := syntheticRetestPilotAt(t, "../../eval/paired-pilots/2026-10-23-recharge-confirm-entry", strings.Repeat("a", 40), false)
	if err := claimRetest(root, p, "first"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".tmp", "paired-live-retests", "recharge-confirm-20261023.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(before, &m)
	if m["budget_ceiling_sha256"] != "4669a31fe6f74f7f6901ed9a4dcb114016d7ed4aac85be949d9b0b6e74c0c00b" || m["reserved_model_micro_cny"] != float64(337920) || m["reserved_jev_micro_usd"] != float64(2688) {
		t.Fatal("claim lost permanent cumulative reservation", m)
	}
	changed := syntheticRetestPilotAt(t, "../../eval/paired-pilots/2026-10-23-recharge-confirm-entry", strings.Repeat("b", 40), true)
	if claimRetest(root, changed, "second") == nil {
		t.Fatal("changed version/output reset claim")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("claim overwritten")
	}
	old := syntheticRetestPilot(t, strings.Repeat("a", 40), false)
	if err := claimRetest(root, old, "old"); err != nil {
		t.Fatal("legacy claim no longer independent", err)
	}
}

func TestRechargeEntryUnreadyStopsBeforeSecretsOrOutput(t *testing.T) {
	root := t.TempDir()
	base := "../../eval/paired-pilots/2026-10-23-recharge-confirm-entry"
	outpath := filepath.Join(root, "no-output")
	args := []string{"-retest", "-authorization", filepath.Join(base, "authorization.example.json"), "-plan", filepath.Join(base, "plan.json"), "-billing-evidence", filepath.Join(base, "billing.example.json"), "-env-file", filepath.Join(root, "missing-secret"), "-out", outpath}
	var out bytes.Buffer
	if runWithOutput(args, &out) != 0 {
		t.Fatal(out.String())
	}
	var m map[string]any
	if json.Unmarshal(out.Bytes(), &m) != nil || m["ready_for_paid_run"] != false || m["campaign_id"] != "recharge-confirm-20261023" {
		t.Fatal(out.String())
	}
	out.Reset()
	if runWithOutput(append(args, "-execute"), &out) != 2 || !strings.Contains(out.String(), "before credentials or fixtures") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(outpath); !os.IsNotExist(err) {
		t.Fatal("output created")
	}
}
