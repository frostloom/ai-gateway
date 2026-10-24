package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func supplementRechargeInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-24-recharge-supplement-entry"
	read := func(n string) []byte {
		r, e := os.ReadFile(filepath.Join(base, n))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	a, p, b := read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e := []byte("synthetic evidence only; no vendor proof")
	a = alteredJSON(alteredJSON(a, "human_authorized", true), "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(alteredJSON(b, "execution_revision", strings.Repeat("a", 40)), "account_tariff_verified", true)
	b = alteredJSON(alteredJSON(b, "token_envelope_verified", true), "evidence_sha256", conversationHash(e))
	return a, p, b, e
}

// Rejects accidental confirmation reuse, widened limits or loss of fixed provenance.
func TestSupplementRechargeScopeBinding(t *testing.T) {
	a, p, b, e := supplementRechargeInputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || !pilot.Ready() || pilot.CampaignID() != "recharge-supplement-20261024" {
		t.Fatalf("supplement rejected: %v %v", err, pilot.Blockers())
	}
	if err := pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err != nil {
		t.Fatal("frozen supplement rejected", err)
	}
	for _, key := range []string{"schema", "campaign_id", "case_key", "execution_revision", "customer_source_revision", "runtime_contract_sha256", "budget_ceiling_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256", "diagnostic_source_revision", "diagnostic_manifest_sha256", "scope_plan_sha256", "prior_journal_sha256", "prior_review_sha256"} {
		if _, err := CheckAuthorizedRetest(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("binding accepted", key)
		}
	}
	for _, key := range []string{"automatic_scope_expansion", "automatic_paid_retries"} {
		if _, err := CheckAuthorizedRetest(alteredJSON(a, key, true), p, b, e); err == nil {
			t.Fatal(key)
		}
	}
	no, err := CheckAuthorizedRetest(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("total budget/prior approval enabled supplement")
	}
	var plan map[string]any
	json.Unmarshal(p, &plan)
	plan["batches"].([]any)[0].(map[string]any)["key"] = "recharge-002/confirm"
	cp, _ := json.Marshal(plan)
	if _, err := CheckAuthorizedRetest(alteredJSON(a, "scope_plan_sha256", conversationHash(cp)), cp, alteredJSON(b, "scope_plan_sha256", conversationHash(cp)), e); err == nil {
		t.Fatal("rebound confirm accepted for supplement")
	}
	var auth map[string]any
	json.Unmarshal(a, &auth)
	auth["authorized_spending"].(map[string]any)["max_model_calls"] = 13
	ca, _ := json.Marshal(auth)
	if _, err := CheckAuthorizedRetest(ca, p, b, e); err == nil {
		t.Fatal("expanded allowance accepted")
	}
	// Even coherent edits to every allowance and digest cannot widen the fixed
	// reviewed allocation. An authorization-only mismatch is not this threat.
	json.Unmarshal(p, &plan)
	plan["campaign_limits"].(map[string]any)["max_model_calls"] = 13
	plan["campaign_limits"].(map[string]any)["max_model_micro_cny"] = 439296
	batchLimits := plan["batches"].([]any)[0].(map[string]any)["model_limits"].(map[string]any)
	batchLimits["max_calls"] = 13
	batchLimits["max_micro_cny"] = 439296
	cp, _ = json.Marshal(plan)
	auth["authorized_spending"].(map[string]any)["max_model_micro_cny"] = 439296
	auth["scope_plan_sha256"] = conversationHash(cp)
	ca, _ = json.Marshal(auth)
	if _, err := CheckAuthorizedRetest(ca, cp, alteredJSON(b, "scope_plan_sha256", conversationHash(cp)), e); err == nil {
		t.Fatal("coherently expanded plan and authorization accepted")
	}
	oa, op, ob, oe := diagnosticRechargeInputs(t)
	for _, v := range []struct{ a, p, b, e []byte }{{oa, p, b, e}, {a, op, b, e}, {a, p, ob, oe}} {
		if _, err := CheckAuthorizedRetest(v.a, v.p, v.b, v.e); err == nil {
			t.Fatal("confirmation reused")
		}
	}
	if _, err := CheckAuthorizedRetest(a, p, b, []byte("tampered")); err == nil {
		t.Fatal("evidence not bound")
	}
	for _, key := range []string{"schema", "campaign_id", "execution_revision", "runtime_contract_sha256", "scope_plan_sha256"} {
		if _, err := CheckAuthorizedRetest(a, p, alteredJSON(b, key, "changed"), e); err == nil {
			t.Fatal("billing binding", key)
		}
	}
}

func TestSupplementRechargeBillingCannotAuthorizeLegacy(t *testing.T) {
	a, p, b, e := conservativePiBilling(t)
	b = alteredJSON(b, "schema", "recharge-supplement-billing-attestation.v1")
	if _, err := CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("supplement billing without identity accepted")
	}
}
