package taskbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diagnosticInputs(t *testing.T) (a, p, b, e []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-26-repeated-diagnostic-entry"
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	a, p, b = read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e = []byte("synthetic evidence only")
	for _, raw := range []*[]byte{&a, &b} {
		*raw = alteredJSON(*raw, "execution_revision", strings.Repeat("a", 40))
	}
	a = alteredJSON(a, "human_authorized", true)
	b = alteredJSON(alteredJSON(alteredJSON(b, "account_tariff_verified", true), "token_envelope_verified", true), "evidence_sha256", conversationHash(e))
	return
}
func TestRepeatedDiagnosticScopeIsSeparate(t *testing.T) {
	a, p, b, e := diagnosticInputs(t)
	pilot, err := CheckAuthorizedRepeatedDiagnostic(a, p, b, e)
	if err != nil || !pilot.Ready() || !pilot.IsRepeated() || !pilot.IsRepeatedDiagnostic() || pilot.CampaignID() != "v5-repeated-export-diagnostic-20261025" {
		t.Fatalf("new scope: %v %v", err, pilot.Blockers())
	}
	if err = pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	no, err := CheckAuthorizedRepeatedDiagnostic(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("total budget substituted for exact consent", err)
	}
	for _, key := range []string{"schema", "campaign_id", "execution_revision", "runtime_contract_sha256", "scope_plan_sha256", "customer_source_revision", "budget_ceiling_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256", "diagnostic_source_revision", "diagnostic_manifest_sha256", "observer_revision", "observer_source_sha256", "synthetic_provenance_sha256", "prior_result_manifest_sha256", "case_key", "prior_journal_sha256", "prior_review_sha256"} {
		if _, err = CheckAuthorizedRepeatedDiagnostic(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("altered auth accepted", key)
		}
	}
	for _, key := range []string{"automatic_scope_expansion", "automatic_paid_retries"} {
		if _, err = CheckAuthorizedRepeatedDiagnostic(alteredJSON(a, key, true), p, b, e); err == nil {
			t.Fatal("expanded permission", key)
		}
	}
	for _, key := range []string{"schema", "campaign_id", "execution_revision", "runtime_contract_sha256", "scope_plan_sha256"} {
		if _, err = CheckAuthorizedRepeatedDiagnostic(a, p, alteredJSON(b, key, "changed"), e); err == nil {
			t.Fatal("billing scope accepted", key)
		}
	}
	if _, err = CheckAuthorizedRepeatedDiagnostic(a, p, b, []byte("changed")); err == nil {
		t.Fatal("changed billing evidence")
	}
	oldA, oldP, oldB, oldE := repeatedInputs(t)
	old, err := CheckAuthorizedRepeated(oldA, oldP, oldB, oldE)
	if err != nil || !old.Ready() || old.IsRepeatedDiagnostic() {
		t.Fatal("old checker changed", err)
	}
	if _, err = CheckAuthorizedRepeatedDiagnostic(oldA, oldP, oldB, oldE); err == nil {
		t.Fatal("old approval reused")
	}
	for _, check := range []func([]byte, []byte, []byte, []byte) (AuthorizedPilot, error){CheckAuthorizedRepeated, CheckAuthorizedCohort, CheckAuthorizedRetest, CheckAuthorizedPilot} {
		if _, err = check(a, p, b, e); err == nil {
			t.Fatal("new approval entered historical checker")
		}
	}
	if _, err = RunAuthorizedRepeated(context.Background(), "absent", AuthorizedFirstBatchOptions{Pilot: pilot, Revision: strings.Repeat("a", 40)}); err == nil {
		t.Fatal("historical runner accepted new scope")
	}
	if _, err = RunAuthorizedRepeatedDiagnostic(context.Background(), "absent", AuthorizedFirstBatchOptions{Pilot: old, Revision: strings.Repeat("a", 40)}); err == nil {
		t.Fatal("new runner accepted old scope")
	}
	observer := "4d887382300a906ae6d1a33972f3fe4d4aca4e0d"
	if _, err = CheckAuthorizedRepeatedDiagnostic(alteredJSON(a, "execution_revision", observer), p, alteredJSON(b, "execution_revision", observer), e); err == nil {
		t.Fatal("observer revision used as new executor")
	}
	for _, mutation := range []string{"cap", "key", "order", "arm", "block", "pair", "extra", "target"} {
		t.Run(mutation, func(t *testing.T) {
			var au, pl map[string]any
			json.Unmarshal(a, &au)
			json.Unmarshal(p, &pl)
			cs := au["cases"].([]any)
			bs := pl["batches"].([]any)
			switch mutation {
			case "cap":
				au["authorized_spending"].(map[string]any)["max_model_calls"] = 249
				pl["campaign_limits"].(map[string]any)["max_model_calls"] = 249
			case "key":
				cs[0].(map[string]any)["key"] = "plans-001/answer_complete"
				bs[0].(map[string]any)["key"] = "plans-001/answer_complete"
			case "order":
				cs[0], cs[1] = cs[1], cs[0]
				bs[0], bs[1] = bs[1], bs[0]
			case "arm":
				bs[0].(map[string]any)["arm_order"] = []string{"jev_selective", "model"}
			case "block":
				bs[0].(map[string]any)["block_id"] = "block-03"
			case "pair":
				bs[1].(map[string]any)["pair_id"] = bs[0].(map[string]any)["pair_id"]
			case "extra":
				au["cases"] = append(cs, cs[0])
				pl["batches"] = append(bs, bs[0])
			case "target":
				au["target"].(map[string]any)["model"] = "deepseek-chat"
				pl["target"].(map[string]any)["model"] = "deepseek-chat"
			}
			cp, _ := json.Marshal(pl)
			au["scope_plan_sha256"] = conversationHash(cp)
			ca, _ := json.Marshal(au)
			if _, err := CheckAuthorizedRepeatedDiagnostic(ca, cp, alteredJSON(b, "scope_plan_sha256", conversationHash(cp)), e); err == nil {
				t.Fatal("coherent scope expansion", mutation)
			}
		})
	}
	historical, _ := json.Marshal(AuthorizedFirstBatchReceipt{})
	if bytes.Contains(historical, []byte("observer_revision")) {
		t.Fatal("legacy receipt shape changed")
	}
}
func TestRepeatedDiagnosticBindingUsesCommittedArchiveBytes(t *testing.T) {
	a, p, b, e := diagnosticInputs(t)
	pilot, err := CheckAuthorizedRepeatedDiagnostic(a, p, b, e)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(pilot.RepeatedDiagnosticBinding())
	var binding map[string]string
	json.Unmarshal(raw, &binding)
	for key, path := range map[string]string{"observer_source_sha256": "2026-10-25-observer-export-diagnostics/source-sha256.json", "synthetic_provenance_sha256": "2026-10-25-observer-synthetic-delivery/verification-provenance.json", "prior_result_manifest_sha256": "2026-10-24-v5-repeated-live-result/manifest.json"} {
		source, err := os.ReadFile(filepath.Join("../../eval/paired-pilots", path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if binding[key] != hex.EncodeToString(sum[:]) {
			t.Fatal("archive binding mismatch", key)
		}
	}
}
