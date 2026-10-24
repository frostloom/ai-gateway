package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rechargeEntryInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-23-recharge-confirm-entry"
	read := func(n string) []byte {
		r, e := os.ReadFile(filepath.Join(base, n))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	a, p, b := read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e := []byte("synthetic evidence only; not vendor proof")
	a = alteredJSON(a, "human_authorized", true)
	a = alteredJSON(a, "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(b, "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(b, "account_tariff_verified", true)
	b = alteredJSON(b, "token_envelope_verified", true)
	b = alteredJSON(b, "evidence_sha256", conversationHash(e))
	return a, p, b, e
}

// Catches rejecting the new scope, permitting larger/rebound scopes, or dropping
// the independent consent, cumulative ceiling and offline acceptance bindings.
func TestRechargeEntryFixedScope(t *testing.T) {
	a, p, b, e := rechargeEntryInputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || pilot.Ready() || !hasString(pilot.Blockers(), "runtime_contract_mismatch") || pilot.CampaignID() != "recharge-confirm-20261023" {
		t.Fatalf("new scope not ready: %v %v", err, pilot.Blockers())
	}
	if err = pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err == nil {
		t.Fatal("historical contract accepted execution")
	}
	no, err := CheckAuthorizedRetest(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("budget ceiling enabled exact execution consent")
	}
	for _, key := range []string{"schema", "campaign_id", "case_key", "customer_source_revision", "runtime_contract_sha256", "budget_ceiling_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256", "scope_plan_sha256", "execution_revision"} {
		if _, err = CheckAuthorizedRetest(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("changed binding accepted", key)
		}
	}
	for _, key := range []string{"automatic_scope_expansion", "automatic_paid_retries"} {
		if _, err = CheckAuthorizedRetest(alteredJSON(a, key, true), p, b, e); err == nil {
			t.Fatal(key)
		}
	}
	var plan map[string]any
	json.Unmarshal(p, &plan)
	plan["batches"].([]any)[0].(map[string]any)["key"] = "recharge-002/duplicate_confirm"
	changed, _ := json.Marshal(plan)
	if _, err = CheckAuthorizedRetest(alteredJSON(a, "scope_plan_sha256", conversationHash(changed)), changed, alteredJSON(b, "scope_plan_sha256", conversationHash(changed)), e); err == nil {
		t.Fatal("rebound different case accepted")
	}
	var auth map[string]any
	json.Unmarshal(a, &auth)
	auth["authorized_spending"].(map[string]any)["max_model_calls"] = 11
	changed, _ = json.Marshal(auth)
	if _, err = CheckAuthorizedRetest(changed, p, b, e); err == nil {
		t.Fatal("expanded call cap accepted")
	}
	if _, err = CheckAuthorizedRetest(a, p, b, []byte("tampered")); err == nil {
		t.Fatal("billing evidence ignored")
	}
	_, _, oldB, oldE := recoveryRetestInputs(t)
	if _, err = CheckAuthorizedRetest(a, p, oldB, oldE); err == nil {
		t.Fatal("inherited consumed billing")
	}
	if _, err = CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("legacy entry accepted recharge")
	}
}

// Exercises the real controlled runner, Handler, database fixtures and budgets;
// only external model/JEV transports are replaced. Missing preview/confirmation,
// lost receipt provenance, fallback after vendor failure or output reuse fails.
func TestRechargeEntryLocalRunner(t *testing.T) {
	testRechargeScopeLocalRunner(t, rechargeEntryInputs, "authorized-recharge-confirm.v1")
}

// Historical scopes remain inspectable but cannot invoke the changed Handler.
func testRechargeScopeLocalRunner(t *testing.T, inputs func(*testing.T) ([]byte, []byte, []byte, []byte), _ string) {
	a, p, b, e := inputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || pilot.Ready() || !hasString(pilot.Blockers(), "runtime_contract_mismatch") {
		t.Fatal("historical execution enabled", err)
	}
	if _, err := RunAuthorizedRetest(context.Background(), filepath.Join(t.TempDir(), "absent"), AuthorizedFirstBatchOptions{Pilot: pilot}); err == nil || !strings.Contains(err.Error(), "verified authorized pilot required") {
		t.Fatal("old scope reached dependencies", err)
	}
}

func TestRechargeBillingCannotAuthorizeLegacyWithoutIdentity(t *testing.T) {
	a, p, b, e := conservativePiBilling(t)
	b = alteredJSON(b, "schema", "recharge-confirm-billing-attestation.v1")
	if _, err := CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("recharge billing schema accepted without fixed identity in legacy mode")
	}
}
