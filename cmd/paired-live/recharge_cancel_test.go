package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCancelRechargeClaimPredecessorsAndPermanentBudget(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-24-recharge-cancel-entry"
	p := syntheticRetestPilotAt(t, base, strings.Repeat("a", 40), false)
	parents := []struct{ id, file string }{{"recharge-confirm-20261023", "prior-confirm-claim.json"}, {"recharge-confirm-diagnostic-retest-20261023", "prior-diagnostic-claim.json"}}
	for _, broken := range []int{0, 1} {
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
				if claimRetest(root, p, "first") == nil {
					t.Fatal("broken predecessor accepted")
				}
				if _, err := os.Stat(filepath.Join(dir, "recharge-cancel-20261024.json")); !os.IsNotExist(err) {
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
	if err := claimRetest(root, p, "first-failed-run"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recharge-cancel-20261024.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(before, &m)
	for key, want := range map[string]float64{"reserved_model_calls": 10, "reserved_model_micro_cny": 337920, "reserved_jev_calls": 1, "reserved_jev_micro_usd": 2688, "cumulative_reserved_model_micro_cny": 1013760, "cumulative_reserved_jev_micro_usd": 8064} {
		if m[key] != want {
			t.Fatal("reservation lost", key, m)
		}
	}
	if m["prior_confirm_claim_sha256"] != "2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc" || m["prior_diagnostic_claim_sha256"] != "23047a143039f719531cfe403f1769abda7a6091b2ee51cecf6184eb4768e654" {
		t.Fatal("prior hashes lost")
	}
	changed := syntheticRetestPilotAt(t, base, strings.Repeat("b", 40), true)
	if claimRetest(root, changed, "new-output") == nil {
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

func TestCancelRechargeUnreadyDoesNotReadSecrets(t *testing.T) {
	base := "../../eval/paired-pilots/2026-10-24-recharge-cancel-entry"
	root := t.TempDir()
	output := filepath.Join(root, "absent")
	args := []string{"-retest", "-authorization", filepath.Join(base, "authorization.example.json"), "-plan", filepath.Join(base, "plan.json"), "-billing-evidence", filepath.Join(base, "billing.example.json"), "-env-file", filepath.Join(root, "missing-secret"), "-out", output}
	var out bytes.Buffer
	if runWithOutput(args, &out) != 0 {
		t.Fatal(out.String())
	}
	var m map[string]any
	json.Unmarshal(out.Bytes(), &m)
	if m["ready_for_paid_run"] != false || m["human_authorized"] != false || m["campaign_id"] != "recharge-cancel-20261024" {
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
