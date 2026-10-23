package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func TestRetestReadOnlyAndExecutionGate(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-23-cash-read-retest-offline"
	dir := t.TempDir()
	auth := filepath.Join(dir, "authorization.json")
	raw, err := os.ReadFile(filepath.Join(base, "authorization.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-retest", "-authorization", auth, "-plan", filepath.Join(base, "plan.json"), "-env-file", filepath.Join(dir, "missing-secret"), "-out", filepath.Join(dir, "output")}
	var out bytes.Buffer
	if code := runWithOutput(args, &out); code != 0 {
		t.Fatalf("readonly code=%d: %s", code, out.String())
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || result["human_authorized"] != false || result["ready_for_paid_run"] != false || result["campaign_id"] != taskbench.CashReadRetestID {
		t.Fatal(out.String())
	}
	out.Reset()
	if runWithOutput(append(args, "-execute"), &out) != 2 || !strings.Contains(out.String(), "before credentials or fixtures") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "output")); !os.IsNotExist(err) {
		t.Fatal("execution produced output")
	}
	for _, extra := range [][]string{{"-continue-from", "elsewhere"}, {"-review", "elsewhere"}, {"-continuation-authorization", "elsewhere"}} {
		out.Reset()
		if runWithOutput(append(args, extra...), &out) != 2 {
			t.Fatal("mixed retest with continuation")
		}
	}
	out.Reset()
	if runWithOutput([]string{"-authorization", auth, "-plan", filepath.Join(base, "plan.json")}, &out) != 2 {
		t.Fatal("legacy accepted retest")
	}
	out.Reset()
	if runWithOutput([]string{"-retest"}, &out) != 2 {
		t.Fatal("retest accepted legacy")
	}
}

func TestRetestClaimFixedIdentity(t *testing.T) {
	root := t.TempDir()
	// Claim function accepts only a ready validated snapshot; templates cannot consume it.
	if err := claimRetest(root, taskbench.AuthorizedPilot{}, "first"); err == nil {
		t.Fatal("invalid snapshot claimed")
	}
	if _, err := os.Stat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) {
		t.Fatal("invalid claim side effects")
	}
	if err := claimRetest(root, syntheticRetestPilot(t, strings.Repeat("a", 40), false), "first"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".tmp", "paired-live-retests", taskbench.CashReadRetestID+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := claimRetest(root, syntheticRetestPilot(t, strings.Repeat("b", 40), true), "different-output"); err == nil {
		t.Fatal("changed revision/output reset identity")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("claim overwritten")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("claim permissions")
	}
}

// Approved state exists only in test memory, with a synthetic evidence source.
func syntheticRetestPilot(t *testing.T, revision string, reformat bool) taskbench.AuthorizedPilot {
	return syntheticRetestPilotAt(t, "../../eval/paired-pilots/2026-10-23-cash-read-retest-offline", revision, reformat)
}
func syntheticRetestPilotAt(t *testing.T, base, revision string, reformat bool) taskbench.AuthorizedPilot {
	t.Helper()
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	decode := func(raw []byte) map[string]any {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	plan := read("plan.json")
	if reformat {
		var compact bytes.Buffer
		json.Compact(&compact, plan)
		plan = compact.Bytes()
	}
	a, b := decode(read("authorization.example.json")), decode(read("billing.example.json"))
	evidence := []byte("synthetic evidence only")
	a["human_authorized"] = true
	for _, m := range []map[string]any{a, b} {
		m["execution_revision"] = revision
		m["scope_plan_sha256"] = digest(plan)
	}
	b["account_tariff_verified"] = true
	b["token_envelope_verified"] = true
	b["evidence_sha256"] = digest(evidence)
	ar, _ := json.Marshal(a)
	br, _ := json.Marshal(b)
	p, err := taskbench.CheckAuthorizedRetest(ar, plan, br, evidence)
	if err != nil || !p.Ready() {
		t.Fatalf("synthetic scope %v", err)
	}
	return p
}

func TestRecoveryRetestClaimsSeparateAndFixed(t *testing.T) {
	root := t.TempDir()
	old := syntheticRetestPilot(t, strings.Repeat("a", 40), false)
	if err := claimRetest(root, old, "old-output"); err != nil {
		t.Fatal(err)
	}
	oldpath := filepath.Join(root, ".tmp", "paired-live-retests", taskbench.CashReadRetestID+".json")
	before, _ := os.ReadFile(oldpath)
	base := "../../eval/paired-pilots/2026-10-23-cash-read-recovery-retest-offline"
	newPilot := syntheticRetestPilotAt(t, base, strings.Repeat("b", 40), false)
	if err := claimRetest(root, newPilot, "new-output"); err != nil {
		t.Fatalf("independent new scope rejected: %v", err)
	}
	newpath := filepath.Join(root, ".tmp", "paired-live-retests", "cash-read-recovery-retest-20261023.json")
	marker, err := os.ReadFile(newpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := claimRetest(root, syntheticRetestPilotAt(t, base, strings.Repeat("c", 40), true), "changed-output"); err == nil {
		t.Fatal("new identity reset")
	}
	if err := claimRetest(root, old, "different-old-output"); err == nil {
		t.Fatal("old identity reset")
	}
	after, _ := os.ReadFile(oldpath)
	again, _ := os.ReadFile(newpath)
	if !bytes.Equal(before, after) || !bytes.Equal(marker, again) {
		t.Fatal("claims overwritten")
	}
	var m map[string]string
	json.Unmarshal(marker, &m)
	if m["campaign_id"] != newPilot.CampaignID() {
		t.Fatal("wrong claim identity")
	}
}

func TestRecoveryRetestReadOnlyRemainsUnapproved(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-23-cash-read-recovery-retest-offline"
	root := t.TempDir()
	outpath := filepath.Join(root, "output")
	args := []string{"-retest", "-authorization", filepath.Join(base, "authorization.example.json"), "-plan", filepath.Join(base, "plan.json"), "-billing-evidence", filepath.Join(base, "billing.example.json"), "-env-file", filepath.Join(root, "missing.env"), "-out", outpath}
	var out bytes.Buffer
	if runWithOutput(args, &out) != 0 {
		t.Fatal(out.String())
	}
	var m map[string]any
	json.Unmarshal(out.Bytes(), &m)
	if m["human_authorized"] != false || m["ready_for_paid_run"] != false || m["campaign_id"] != "cash-read-recovery-retest-20261023" {
		t.Fatal(out.String())
	}
	out.Reset()
	if runWithOutput(append(args, "-execute"), &out) != 2 || !strings.Contains(out.String(), "before credentials or fixtures") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(outpath); !os.IsNotExist(err) {
		t.Fatal("unapproved output created")
	}
}

func TestRecoveryRetestClaimRejectsUnknownIdentityBeforeIO(t *testing.T) {
	for _, id := range []string{"new-custom-id", "../escape", "", "cash-read-recovery-retest-20261024"} {
		root := t.TempDir()
		if persistRetestClaim(root, id, strings.Repeat("a", 40), "out") == nil {
			t.Fatal("arbitrary retest ID allowed")
		}
		if _, err := os.Stat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) {
			t.Fatal("rejected scope created directory")
		}
	}
}
