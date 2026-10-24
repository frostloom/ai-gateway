package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repeatedInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-24-v5-repeated-entry"
	read := func(n string) []byte {
		b, e := os.ReadFile(filepath.Join(base, n))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	a, p, b := read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e := []byte("synthetic evidence only")
	a = alteredJSON(alteredJSON(a, "human_authorized", true), "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(alteredJSON(b, "execution_revision", strings.Repeat("a", 40)), "account_tariff_verified", true)
	b = alteredJSON(alteredJSON(b, "token_envelope_verified", true), "evidence_sha256", conversationHash(e))
	return a, p, b, e
}
func TestRepeatedFixedScopeBindings(t *testing.T) {
	a, p, b, e := repeatedInputs(t)
	pilot, err := CheckAuthorizedRepeated(a, p, b, e)
	if err != nil || !pilot.Ready() || !pilot.IsRepeated() || !pilot.HumanAuthorized() || pilot.CampaignID() != "v5-repeated-cohort-20261024" {
		t.Fatalf("cohort not ready: %v %v", err, pilot.Blockers())
	}
	if err = pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "campaign_id", "scope_plan_sha256", "customer_source_revision", "runtime_contract_sha256", "budget_ceiling_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256", "diagnostic_source_revision", "diagnostic_manifest_sha256", "execution_revision", "case_key", "prior_journal_sha256", "prior_review_sha256"} {
		if _, err = CheckAuthorizedRepeated(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("bad auth", key)
		}
	}
	for _, key := range []string{"automatic_scope_expansion", "automatic_paid_retries"} {
		if _, err = CheckAuthorizedRepeated(alteredJSON(a, key, true), p, b, e); err == nil {
			t.Fatal(key)
		}
	}
	no, err := CheckAuthorizedRepeated(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("prior total budget authorized cohort")
	}
	for _, key := range []string{"schema", "campaign_id", "execution_revision", "runtime_contract_sha256", "scope_plan_sha256"} {
		if _, err = CheckAuthorizedRepeated(a, p, alteredJSON(b, key, "changed"), e); err == nil {
			t.Fatal("billing binding", key)
		}
	}
	if _, err = CheckAuthorizedRepeated(a, p, b, []byte("changed")); err == nil {
		t.Fatal("evidence change accepted")
	}
	if _, err = CheckAuthorizedRetest(a, p, b, e); err == nil {
		t.Fatal("cohort through single retest")
	}
	if _, err = CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("cohort through legacy")
	}
	// Matching auth+plan digests cannot widen a reviewed fixed allocation.
	for _, change := range []string{"cap", "case", "order", "extra", "arm", "pair", "block"} {
		t.Run(change, func(t *testing.T) {
			var au, pl map[string]any
			json.Unmarshal(a, &au)
			json.Unmarshal(p, &pl)
			cs := au["cases"].([]any)
			bs := pl["batches"].([]any)
			switch change {
			case "arm":
				bs[0].(map[string]any)["arm_order"] = []string{"jev_selective", "model"}
			case "pair":
				bs[1].(map[string]any)["pair_id"] = bs[0].(map[string]any)["pair_id"]
			case "block":
				bs[0].(map[string]any)["block_id"] = "block-03"
			case "cap":
				au["authorized_spending"].(map[string]any)["max_model_calls"] = 249
				au["authorized_spending"].(map[string]any)["max_model_micro_cny"] = 8414208
				pl["campaign_limits"].(map[string]any)["max_model_calls"] = 249
				pl["campaign_limits"].(map[string]any)["max_model_micro_cny"] = 8414208
				cs[0].(map[string]any)["max_model_calls"] = 7
				bs[0].(map[string]any)["model_limits"].(map[string]any)["max_calls"] = 7
				bs[0].(map[string]any)["model_limits"].(map[string]any)["max_micro_cny"] = 236544
			case "case":
				cs[0].(map[string]any)["key"] = "plans-001/answer_complete"
				bs[0].(map[string]any)["key"] = "plans-001/answer_complete"
			case "order":
				cs[0], cs[1] = cs[1], cs[0]
				bs[0], bs[1] = bs[1], bs[0]
			case "extra":
				au["cases"] = append(cs, cs[0])
				pl["batches"] = append(bs, bs[0])
			}
			cp, _ := json.Marshal(pl)
			au["scope_plan_sha256"] = conversationHash(cp)
			ca, _ := json.Marshal(au)
			if _, err := CheckAuthorizedRepeated(ca, cp, alteredJSON(b, "scope_plan_sha256", conversationHash(cp)), e); err == nil {
				t.Fatal("coherent edit accepted", change)
			}
		})
	}
	oa, op, ob, oe := cohortInputs(t)
	if _, err := CheckAuthorizedRepeated(oa, op, ob, oe); err == nil {
		t.Fatal("old cohort permission reused")
	}
	var historical CampaignPreparationPlan
	json.Unmarshal(op, &historical)
	historical.Batches[0].PairID = "block-00-pair-00"
	historical.Batches[0].BlockID = "block-00"
	historical.Batches[0].ArmOrder = []string{"jev_selective", "model"}
	hp, _ := json.Marshal(historical)
	if _, err := CheckAuthorizedCohort(alteredJSON(oa, "scope_plan_sha256", conversationHash(hp)), hp, alteredJSON(ob, "scope_plan_sha256", conversationHash(hp)), oe); err == nil {
		t.Fatal("historical cohort accepted repeated metadata")
	}
}
