package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Missing mode dispatch must not turn a harmless false-consent check into an error
// or read its deliberately absent credentials/output.
func TestRepeatedDiagnosticFalseCheckDoesNotReadCredentials(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-26-repeated-diagnostic-entry"
	dir := t.TempDir()
	output := filepath.Join(dir, "uncreated")
	args := []string{"-repeated-diagnostic", "-authorization", filepath.Join(base, "authorization.example.json"), "-plan", filepath.Join(base, "plan.json"), "-billing-evidence", filepath.Join(base, "billing.example.json"), "-env-file", filepath.Join(dir, "missing-env"), "-out", output}
	var out bytes.Buffer
	if code := runWithOutput(args, &out); code != 0 {
		t.Fatalf("false-consent diagnostic check rejected: exit%d %s", code, out.String())
	}
	var got map[string]any
	if json.Unmarshal(out.Bytes(), &got) != nil || got["ready_for_paid_run"] != false || got["human_authorized"] != false || got["campaign_id"] != "v5-repeated-export-diagnostic-20261025" {
		t.Fatalf("wrong readiness: %s", out.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("check created output", err)
	}
}

func TestRepeatedDiagnosticClaimChain(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-26-repeated-diagnostic-entry"
	p := syntheticDiagnosticPilotAt(t, base, strings.Repeat("a", 40), false)
	parents := []struct{ id, file string }{{"recharge-confirm-20261023", "prior-confirm-claim.json"}, {"recharge-confirm-diagnostic-retest-20261023", "prior-diagnostic-claim.json"}, {"recharge-cancel-20261024", "prior-cancel-claim.json"}, {"recharge-supplement-20261024", "prior-supplement-claim.json"}, {"refund-cap-20261024", "prior-refund-claim.json"}, {"v5-six-case-cohort-20261024", "prior-cohort-claim.json"}, {"v5-repeated-cohort-20261024", "prior-repeated-claim.json"}}
	for _, broken := range []int{0, 1, 2, 3, 4, 5, 6} {
		for _, kind := range []string{"missing", "tampered"} {
			t.Run(parents[broken].id+kind, func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, ".tmp", "paired-live-retests")
				os.MkdirAll(dir, 0700)
				for i, v := range parents {
					raw, err := os.ReadFile(filepath.Join(base, v.file))
					if err != nil {
						t.Fatal(err)
					}
					if i == broken {
						if kind == "missing" {
							continue
						}
						raw = []byte("tampered")
					}
					if err := os.WriteFile(filepath.Join(dir, v.id+".json"), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if claimRepeatedDiagnostic(root, p, "first") == nil {
					t.Fatal("broken predecessor accepted")
				}
				if _, err := os.Stat(filepath.Join(dir, "v5-repeated-export-diagnostic-20261025.json")); !os.IsNotExist(err) {
					t.Fatal("new claim created on rejection")
				}
			})
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".tmp", "paired-live-retests")
	os.MkdirAll(dir, 0700)
	for _, v := range parents {
		raw, err := os.ReadFile(filepath.Join(base, v.file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, v.id+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := claimRepeatedDiagnostic(root, p, "first-failed-run"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "v5-repeated-export-diagnostic-20261025.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(before, &m)
	if m["prior_repeated_claim_sha256"] != "c7f14ee3b310cec23cf71b7dc004fe73b29ec5e8efa4b6c4167eb91159c8dd58" {
		t.Fatal("seventh predecessor missing")
	}
	if m["prior_cohort_claim_sha256"] != taskbench.V5SixCohortClaimSHA {
		t.Fatal("prior cohort binding lost")
	}
	for key, want := range map[string]float64{"reserved_model_calls": 248, "reserved_model_micro_cny": 8380416, "reserved_jev_calls": 24, "reserved_jev_micro_usd": 64512, "cumulative_reserved_model_micro_cny": 20680704, "cumulative_reserved_jev_micro_usd": 158592} {
		if m[key] != want {
			t.Fatal("reservation lost", key, m)
		}
	}
	if m["prior_refund_claim_sha256"] != "9a026381dea69bc1feb9712d58a41e305877701e1355ba2464543aaaa0857b30" || m["prior_supplement_claim_sha256"] != "824e1e44882b1043bba833ec6af5b88980d6d89b4ce8e4023fd12c68c98fb11c" || m["prior_cancel_claim_sha256"] != "a9ce7a22d79e4e42e8ded0b932c34648df3db0d0574ad0710c098d2b02cbc8f4" || m["prior_confirm_claim_sha256"] != "2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc" || m["prior_diagnostic_claim_sha256"] != "23047a143039f719531cfe403f1769abda7a6091b2ee51cecf6184eb4768e654" {
		t.Fatal("prior hashes lost")
	}
	changed := syntheticDiagnosticPilotAt(t, base, strings.Repeat("b", 40), true)
	if claimRepeatedDiagnostic(root, changed, "new-output") == nil {
		t.Fatal("new revision/output reset consumed claim")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("claim overwritten")
	}
	for _, v := range parents {
		want, _ := os.ReadFile(filepath.Join(base, v.file))
		got, _ := os.ReadFile(filepath.Join(dir, v.id+".json"))
		if !bytes.Equal(want, got) {
			t.Fatal("prior claim changed")
		}
	}
}

func syntheticDiagnosticPilotAt(t *testing.T, base, rev string, reformat bool) taskbench.AuthorizedPilot {
	t.Helper()
	read := func(n string) []byte {
		b, e := os.ReadFile(filepath.Join(base, n))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var a, b map[string]any
	p := read("plan.json")
	if reformat {
		var out bytes.Buffer
		json.Compact(&out, p)
		p = out.Bytes()
	}
	digest := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	json.Unmarshal(read("authorization.example.json"), &a)
	json.Unmarshal(read("billing.example.json"), &b)
	e := []byte("synthetic evidence only")
	a["human_authorized"] = true
	for _, m := range []map[string]any{a, b} {
		m["execution_revision"] = rev
		m["scope_plan_sha256"] = digest(p)
	}
	b["account_tariff_verified"] = true
	b["token_envelope_verified"] = true
	b["evidence_sha256"] = digest(e)
	ar, _ := json.Marshal(a)
	br, _ := json.Marshal(b)
	pilot, err := taskbench.CheckAuthorizedRepeatedDiagnostic(ar, p, br, e)
	if err != nil || !pilot.Ready() {
		t.Fatalf("cohort %v %v", err, pilot.Blockers())
	}
	return pilot
}

func TestRepeatedDiagnosticRejectsOtherModes(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-26-repeated-diagnostic-entry"
	args := []string{"-repeated-diagnostic", "-authorization", filepath.Join(base, "authorization.example.json"), "-plan", filepath.Join(base, "plan.json")}
	for _, extra := range [][]string{{"-repeated"}, {"-cohort"}, {"-retest"}, {"-continue-from", "unused"}, {"-review", "unused"}, {"-continuation-authorization", "unused"}} {
		var out bytes.Buffer
		if runWithOutput(append(append([]string{}, args...), extra...), &out) != 2 || !strings.Contains(out.String(), "repeated diagnostic cannot use other modes or continuation") {
			t.Fatal(out.String())
		}
	}
	pilot := syntheticDiagnosticPilotAt(t, base, strings.Repeat("a", 40), false)
	root := t.TempDir()
	if claimRepeated(root, pilot, "unused") == nil || claimCohort(root, pilot, "unused") == nil || claimRetest(root, pilot, "unused") == nil {
		t.Fatal("diagnostic permission entered historical claim")
	}
	if _, err := os.Stat(filepath.Join(root, ".tmp")); !os.IsNotExist(err) {
		t.Fatal("historical claim rejection created directory", err)
	}
}
