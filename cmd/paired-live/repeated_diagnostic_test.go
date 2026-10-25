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

func originalRepeatedClaimBytes(t *testing.T) []byte {
	t.Helper()
	record := map[string]any{"schema": "paired-live-repeated-claim.v1", "campaign_id": taskbench.V5RepeatedID, "execution_revision": "9ff31b162ebb02b1f89ad7ae1d8c2fe914965d33", "output": "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/v5-repeated-live-20261024/run-9ff31b1", "status": "consumed_before_execution", "budget_ceiling_sha256": taskbench.RechargeBudgetCeilingSHA, "reserved_model_calls": 248, "reserved_model_micro_cny": 8380416, "reserved_jev_calls": 24, "reserved_jev_micro_usd": 64512, "cumulative_reserved_model_micro_cny": 12300288, "cumulative_reserved_jev_micro_usd": 94080, "prior_confirm_claim_sha256": taskbench.RechargePriorClaimSHA, "prior_diagnostic_claim_sha256": taskbench.RechargeDiagnosticClaimSHA, "prior_cancel_claim_sha256": taskbench.RechargeCancelClaimSHA, "prior_supplement_claim_sha256": taskbench.RechargeSupplementClaimSHA, "prior_refund_claim_sha256": taskbench.RefundCapClaimSHA, "prior_cohort_claim_sha256": taskbench.V5SixCohortClaimSHA}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != taskbench.V5RepeatedClaimSHA || taskbench.V5RepeatedClaimSHA != "c7f14ee3b310cec23cf71b7dc004fe73b29ec5e8efa4b6c4167eb91159c8dd58" {
		t.Fatalf("predecessor bytes are not the consumed repeated claim: %s", hex.EncodeToString(sum[:]))
	}
	return raw
}

func diagnosticExampleFiles(t *testing.T, dir string, approved bool) (auth, plan, billing string) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-24-v5-repeated-entry"
	var pl, au, bi map[string]any
	read := func(n string, dest any) {
		b, err := os.ReadFile(filepath.Join(base, n))
		if err != nil {
			t.Fatal(err)
		}
		if json.Unmarshal(b, dest) != nil {
			t.Fatal(n)
		}
	}
	read("plan.json", &pl)
	read("authorization.example.json", &au)
	read("billing.example.json", &bi)
	pl["campaign_id"] = taskbench.V5RepeatedDiagnosticID
	rawPlan, _ := json.Marshal(pl)
	digest := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	au["schema"] = "v5-repeated-export-diagnostic-authorization.v1"
	au["campaign_id"] = taskbench.V5RepeatedDiagnosticID
	au["human_authorized"] = approved
	au["scope_plan_sha256"] = digest(rawPlan)
	au["execution_revision"] = strings.Repeat("0", 40)
	au["observer_revision"] = "4d887382300a906ae6d1a33972f3fe4d4aca4e0d"
	au["observer_source_sha256"] = "16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9"
	au["synthetic_provenance_sha256"] = "08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409"
	au["prior_result_manifest_sha256"] = "41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8"
	bi["schema"] = "v5-repeated-export-diagnostic-billing-attestation.v1"
	bi["campaign_id"] = taskbench.V5RepeatedDiagnosticID
	bi["scope_plan_sha256"] = digest(rawPlan)
	bi["execution_revision"] = strings.Repeat("0", 40)
	auth, plan, billing = filepath.Join(dir, "authorization.json"), filepath.Join(dir, "plan.json"), filepath.Join(dir, "billing.json")
	write := func(path string, v any) {
		b, _ := json.Marshal(v)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(plan, pl)
	write(auth, au)
	write(billing, bi)
	return auth, plan, billing
}

func TestRepeatedDiagnosticClaimChain(t *testing.T) {
	if taskbench.V5SixCohortClaimSHA != "b49808feb6a124c60c7f3fd88a7965589368a0ce8d8f4336e94ebaee9a025d34" || taskbench.RechargePriorClaimSHA != "2c9d64b98bd985a24da5aed31c049d63311ebdd7f8dc4c862600ccd77e80dcbc" || taskbench.RechargeDiagnosticClaimSHA != "23047a143039f719531cfe403f1769abda7a6091b2ee51cecf6184eb4768e654" || taskbench.RechargeCancelClaimSHA != "a9ce7a22d79e4e42e8ded0b932c34648df3db0d0574ad0710c098d2b02cbc8f4" || taskbench.RechargeSupplementClaimSHA != "824e1e44882b1043bba833ec6af5b88980d6d89b4ce8e4023fd12c68c98fb11c" || taskbench.RefundCapClaimSHA != "9a026381dea69bc1feb9712d58a41e305877701e1355ba2464543aaaa0857b30" {
		t.Fatal("historical predecessor constants changed")
	}
	base := "../../eval/paired-pilots/2026-10-24-v5-repeated-entry"
	parents := []struct{ id, file string }{{"recharge-confirm-20261023", "prior-confirm-claim.json"}, {"recharge-confirm-diagnostic-retest-20261023", "prior-diagnostic-claim.json"}, {"recharge-cancel-20261024", "prior-cancel-claim.json"}, {"recharge-supplement-20261024", "prior-supplement-claim.json"}, {"refund-cap-20261024", "prior-refund-claim.json"}, {"v5-six-case-cohort-20261024", "prior-cohort-claim.json"}, {taskbench.V5RepeatedID, ""}}
	p := syntheticDiagnosticPilot(t, strings.Repeat("a", 40))
	historical := syntheticRepeatedPilotAt(t, base, strings.Repeat("a", 40), false)
	for broken := range parents {
		for _, kind := range []string{"missing", "tampered"} {
			t.Run(parents[broken].id+kind, func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, ".tmp", "paired-live-retests")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for i, v := range parents {
					var raw []byte
					var err error
					if v.file == "" {
						raw = originalRepeatedClaimBytes(t)
					} else {
						raw, err = os.ReadFile(filepath.Join(base, v.file))
						if err != nil {
							t.Fatal(err)
						}
					}
					if i == broken {
						if kind == "missing" {
							continue
						}
						raw = []byte("tampered\n")
					}
					if err = os.WriteFile(filepath.Join(dir, v.id+".json"), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if claimRepeatedDiagnostic(root, p, "first") == nil {
					t.Fatal("broken predecessor accepted")
				}
				if _, err := os.Stat(filepath.Join(dir, taskbench.V5RepeatedDiagnosticID+".json")); !os.IsNotExist(err) {
					t.Fatal("new claim created on rejection")
				}
			})
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".tmp", "paired-live-retests")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, v := range parents[:6] {
		raw, err := os.ReadFile(filepath.Join(base, v.file))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, v.id+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := claimRepeated(root, historical, "historical-output"); err != nil {
		t.Fatal("old six-predecessor path changed", err)
	}
	oldRaw, err := os.ReadFile(filepath.Join(dir, taskbench.V5RepeatedID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	json.Unmarshal(oldRaw, &old)
	if old["cumulative_reserved_model_micro_cny"] != float64(12300288) || old["cumulative_reserved_jev_micro_usd"] != float64(94080) || old["prior_repeated_claim_sha256"] != nil {
		t.Fatal("historical claim reservation changed", old)
	}
	if err = os.WriteFile(filepath.Join(dir, taskbench.V5RepeatedID+".json"), originalRepeatedClaimBytes(t), 0600); err != nil {
		t.Fatal(err)
	}
	if claimRepeatedDiagnostic(root, historical, "wrong-pilot") == nil {
		t.Fatal("historical pilot created diagnostic claim")
	}
	if err = claimRepeatedDiagnostic(root, p, "first-failed-run"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, taskbench.V5RepeatedDiagnosticID+".json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("claim mode", err, info)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(before, &m)
	for key, want := range map[string]float64{"reserved_model_calls": 248, "reserved_model_micro_cny": 8380416, "reserved_jev_calls": 24, "reserved_jev_micro_usd": 64512, "cumulative_reserved_model_micro_cny": 20680704, "cumulative_reserved_jev_micro_usd": 158592} {
		if m[key] != want {
			t.Fatal("reservation", key, m[key])
		}
	}
	if m["schema"] != "paired-live-repeated-export-diagnostic-claim.v1" || m["campaign_id"] != taskbench.V5RepeatedDiagnosticID || m["prior_repeated_claim_sha256"] != taskbench.V5RepeatedClaimSHA || m["prior_cohort_claim_sha256"] != taskbench.V5SixCohortClaimSHA {
		t.Fatal("claim identity", m)
	}
	changed := syntheticDiagnosticPilot(t, strings.Repeat("b", 40))
	if claimRepeatedDiagnostic(root, changed, "new-output") == nil {
		t.Fatal("second output or revision reclaimed")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("claim overwritten")
	}
}

func syntheticDiagnosticPilot(t *testing.T, rev string) taskbench.AuthorizedPilot {
	t.Helper()
	dir := t.TempDir()
	auth, plan, billing := diagnosticExampleFiles(t, dir, true)
	read := func(n string) []byte {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a, p, b := read(auth), read(plan), read(billing)
	e := []byte("synthetic evidence only")
	var am, bm map[string]any
	json.Unmarshal(a, &am)
	json.Unmarshal(b, &bm)
	digest := func(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
	am["human_authorized"] = true
	am["execution_revision"] = rev
	bm["execution_revision"] = rev
	bm["account_tariff_verified"] = true
	bm["token_envelope_verified"] = true
	bm["evidence_sha256"] = digest(e)
	ar, _ := json.Marshal(am)
	br, _ := json.Marshal(bm)
	pilot, err := taskbench.CheckAuthorizedRepeatedDiagnostic(ar, p, br, e)
	if err != nil || !pilot.Ready() || !pilot.IsRepeatedDiagnostic() {
		t.Fatalf("diagnostic pilot %v %v", err, pilot.Blockers())
	}
	return pilot
}

func TestRepeatedDiagnosticFlagIsolation(t *testing.T) {
	dir := t.TempDir()
	auth, plan, billing := diagnosticExampleFiles(t, dir, false)
	args := []string{"-repeated-diagnostic", "-authorization", auth, "-plan", plan, "-billing-evidence", billing}
	for _, extra := range [][]string{{"-repeated"}, {"-cohort"}, {"-retest"}, {"-continue-from", "unused"}, {"-review", "unused"}, {"-continuation-authorization", "unused"}} {
		var out bytes.Buffer
		if runWithOutput(append(append([]string{}, args...), extra...), &out) != 2 || !strings.Contains(out.String(), "repeated diagnostic cannot use other modes or continuation") {
			t.Fatal(extra, out.String())
		}
	}
	output := filepath.Join(dir, "absent")
	checkArgs := append(append([]string{}, args...), "-env-file", filepath.Join(dir, "missing-secret"), "-out", output)
	var out bytes.Buffer
	if runWithOutput(checkArgs, &out) != 0 {
		t.Fatal(out.String())
	}
	var m map[string]any
	if json.Unmarshal(out.Bytes(), &m) != nil || m["ready_for_paid_run"] != false || m["human_authorized"] != false || m["campaign_id"] != taskbench.V5RepeatedDiagnosticID || m["execution_revision"] != strings.Repeat("0", 40) || m["model_calls"] != float64(0) || m["jev_calls"] != float64(0) {
		t.Fatal(out.String())
	}
	out.Reset()
	if runWithOutput(append(checkArgs, "-execute"), &out) != 2 || !strings.Contains(out.String(), "before credentials or fixtures") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("output created")
	}
	if _, err := os.Stat(filepath.Join(dir, "missing-secret")); !os.IsNotExist(err) {
		t.Fatal("secret file created")
	}
}
