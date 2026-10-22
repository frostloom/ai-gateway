package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorizedFrozenInputsRejectBeforeResourceCreation(t *testing.T) {
	a, p, b, e := authorizedEntryInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, b, e)
	if err != nil {
		t.Fatal(err)
	}
	defs := "../../eval/task-definitions/2026-10-12-full-dev-v1"
	profiles := "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"
	if err := pilot.ValidateFrozenInputs(defs, profiles, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profiles.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if pilot.ValidateFrozenInputs(defs, dir, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") == nil {
		t.Fatal("changed profile accepted")
	}
	if pilot.ValidateFrozenInputs(dir, profiles, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") == nil {
		t.Fatal("changed definitions accepted")
	}
}

func authorizedEntryInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	a, err := os.ReadFile("../../eval/paired-pilots/2026-10-22-authorized-small-pilot/authorization.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := os.ReadFile("../../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	evidence := []byte("loopback-test-only tariff/token/failed-charge evidence")
	b, _ := json.Marshal(map[string]any{"schema": "paired-billing-attestation.v1", "scope_plan_sha256": conversationHash(p), "model_base": "https://maas.qianwenaiapi.com/apps/anthropic", "model": "deepseek-v4.1-flash", "protocol": "anthropic", "jev_model": "jev-1.13.0", "account_tariff_verified": true, "token_envelope_verified": true, "jev_failure_charges_verified": true, "evidence_sha256": conversationHash(evidence)})
	return a, p, b, evidence
}

// Removing the separation between permission and evidence would enable a paid
// request just because the user supplied a spending authorization.
func TestAuthorizedEntryPermissionDoesNotAttestBilling(t *testing.T) {
	a, p, _, _ := authorizedEntryInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, nil, nil)
	if err != nil || pilot.Ready() || len(pilot.Blockers()) != 3 {
		t.Fatalf("unknown billing became ready: %v %v", pilot.Blockers(), err)
	}
}

func TestAuthorizedEntryBindsBillingEvidence(t *testing.T) {
	a, p, b, e := authorizedEntryInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, b, e)
	if err != nil || !pilot.Ready() {
		t.Fatalf("valid explicit attestation rejected: %v", err)
	}
	for _, key := range []string{"account_tariff_verified", "token_envelope_verified", "jev_failure_charges_verified"} {
		var fields map[string]any
		json.Unmarshal(b, &fields)
		fields[key] = false
		raw, _ := json.Marshal(fields)
		got, err := CheckAuthorizedPilot(a, p, raw, e)
		if err != nil || got.Ready() || len(got.Blockers()) != 1 {
			t.Fatalf("unverified %s accepted: %v", key, err)
		}
	}
	if _, err = CheckAuthorizedPilot(a, p, b, []byte("changed evidence")); err == nil {
		t.Fatal("changed evidence accepted")
	}
	for _, key := range []string{"scope_plan_sha256", "model_base", "model", "protocol", "jev_model"} {
		var fields map[string]any
		json.Unmarshal(b, &fields)
		fields[key] = "changed"
		raw, _ := json.Marshal(fields)
		if _, err = CheckAuthorizedPilot(a, p, raw, e); err == nil {
			t.Fatalf("changed billing binding %s accepted", key)
		}
	}
}

func TestAuthorizedEntryRejectsScopeExpansion(t *testing.T) {
	a, p, b, e := authorizedEntryInputs(t)
	for _, kind := range []string{"permission", "hash", "model", "retry", "calls", "currency", "later-case", "later-budget"} {
		t.Run(kind, func(t *testing.T) {
			var auth map[string]any
			var plan map[string]any
			json.Unmarshal(a, &auth)
			json.Unmarshal(p, &plan)
			switch kind {
			case "permission":
				auth["human_authorized"] = false
			case "hash":
				auth["scope_plan_sha256"] = "changed"
			case "model":
				auth["model"] = "changed"
			case "retry":
				auth["automatic_paid_retries"] = true
			case "calls":
				auth["authorized_spending"].(map[string]any)["max_model_calls"] = 25
			case "currency":
				auth["authorized_spending"].(map[string]any)["max_jev_micro_usd"] = 20001
			case "later-case":
				auth["cases"].([]any)[5].(map[string]any)["key"] = "different"
			case "later-budget":
				plan["batches"].([]any)[5].(map[string]any)["max_jev_calls"] = 2
			}
			aa, _ := json.Marshal(auth)
			pp := p
			if kind == "later-budget" {
				pp, _ = json.Marshal(plan)
				auth["scope_plan_sha256"] = conversationHash(pp)
				aa, _ = json.Marshal(auth)
			}
			if _, err := CheckAuthorizedPilot(aa, pp, b, e); err == nil {
				t.Fatal("expanded/mismatched scope accepted")
			}
		})
	}
}
