package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diagnosticRechargeInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-23-recharge-diagnostic-retest-entry"
	read := func(n string) []byte {
		r, e := os.ReadFile(filepath.Join(base, n))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	a, p, b := read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e := []byte("synthetic operator evidence only")
	a = alteredJSON(alteredJSON(a, "human_authorized", true), "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(alteredJSON(b, "execution_revision", strings.Repeat("a", 40)), "account_tariff_verified", true)
	b = alteredJSON(alteredJSON(b, "token_envelope_verified", true), "evidence_sha256", conversationHash(e))
	return a, p, b, e
}

func TestDiagnosticRechargeScopeBinding(t *testing.T) {
	a, p, b, e := diagnosticRechargeInputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || pilot.Ready() || !hasString(pilot.Blockers(), "runtime_contract_mismatch") || pilot.CampaignID() != "recharge-confirm-diagnostic-retest-20261023" {
		t.Fatalf("new scope rejected: %v %v", err, pilot.Blockers())
	}
	if err := pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err == nil {
		t.Fatal("historical contract accepted execution")
	}
	for _, key := range []string{"schema", "campaign_id", "diagnostic_source_revision", "diagnostic_manifest_sha256", "prior_journal_sha256", "prior_review_sha256", "budget_ceiling_sha256", "runtime_contract_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256"} {
		if _, err := CheckAuthorizedRetest(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("changed binding accepted", key)
		}
	}
	no, err := CheckAuthorizedRetest(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("budget/prior approval enabled new scope")
	}
	oldA, oldP, oldB, oldE := rechargeEntryInputs(t)
	for _, v := range []struct{ a, p, b, e []byte }{{a, p, oldB, oldE}, {oldA, p, b, e}, {a, oldP, b, e}} {
		if _, err := CheckAuthorizedRetest(v.a, v.p, v.b, v.e); err == nil {
			t.Fatal("old scope rebound")
		}
	}
	if _, err := CheckAuthorizedRetest(a, p, b, []byte("changed")); err == nil {
		t.Fatal("billing evidence not bound")
	}
	var changed map[string]any
	json.Unmarshal(p, &changed)
	changed["batches"].([]any)[0].(map[string]any)["key"] = "recharge-002/duplicate_confirm"
	cp, _ := json.Marshal(changed)
	if _, err := CheckAuthorizedRetest(alteredJSON(a, "scope_plan_sha256", conversationHash(cp)), cp, alteredJSON(b, "scope_plan_sha256", conversationHash(cp)), e); err == nil {
		t.Fatal("different case rebound")
	}
}
func TestDiagnosticRechargeBillingCannotAuthorizeLegacy(t *testing.T) {
	a, p, b, e := conservativePiBilling(t)
	b = alteredJSON(b, "schema", "recharge-confirm-billing-attestation.v2")
	if _, err := CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("v2 billing accepted without independent identity")
	}
}

func TestDiagnosticRechargeLocalRunner(t *testing.T) {
	testRechargeScopeLocalRunner(t, diagnosticRechargeInputs, "authorized-recharge-confirm.v2")
}
