package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func diagnosticMaterials(approved bool, revision string) (a, p, b, e []byte) {
	scope := v5RepeatedScope()
	target := PilotTarget{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai", JEVModel: "jev-1.13.0"}
	plan := CampaignPreparationPlan{CampaignID: V5RepeatedDiagnosticID, CustomerRevision: scope.CustomerRevision, RuntimeContractSHA: scope.RuntimeContractSHA, Schema: "campaign-preflight-plan.v2", Target: &target, Limits: v5RepeatedLimits(), Batches: v5RepeatedBatches()}
	p, _ = json.Marshal(plan)
	e = []byte("synthetic evidence only")
	digest := conversationHash(p)
	auth := map[string]any{
		"schema": "v5-repeated-export-diagnostic-authorization.v1", "campaign_id": V5RepeatedDiagnosticID, "human_authorized": approved,
		"scope_plan_sha256": digest, "target": target, "authorized_spending": v5RepeatedLimits(), "cases": v5RepeatedCases(),
		"case_key": "", "execution_revision": revision, "automatic_scope_expansion": false, "automatic_paid_retries": false,
		"customer_source_revision": scope.CustomerRevision, "runtime_contract_sha256": scope.RuntimeContractSHA,
		"budget_ceiling_sha256": scope.BudgetCeilingSHA, "prior_offline_manifest_sha256": scope.OfflineManifestSHA, "prior_offline_acceptance_sha256": scope.OfflineAcceptanceSHA,
		"prior_journal_sha256": "", "prior_review_sha256": "", "diagnostic_source_revision": scope.DiagnosticSourceRevision, "diagnostic_manifest_sha256": scope.DiagnosticManifestSHA,
		"observer_revision": "4d887382300a906ae6d1a33972f3fe4d4aca4e0d", "observer_source_sha256": "16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9",
		"synthetic_provenance_sha256": "08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409", "prior_result_manifest_sha256": "41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8",
	}
	a, _ = json.Marshal(auth)
	billing := map[string]any{
		"schema": "v5-repeated-export-diagnostic-billing-attestation.v1", "campaign_id": V5RepeatedDiagnosticID, "execution_revision": revision,
		"runtime_contract_sha256": scope.RuntimeContractSHA, "scope_plan_sha256": digest, "model_base": target.BaseURL, "model": target.Model,
		"protocol": target.Protocol, "jev_model": target.JEVModel, "account_tariff_verified": true, "token_envelope_verified": true,
		"jev_failure_charges_verified": false, "jev_failure_policy": fullAttemptFailurePolicy, "jev_max_input_tokens": 64000,
		"jev_input_micro_usd_per_million": 42000, "jev_full_attempt_micro_usd": 2688, "evidence_sha256": conversationHash(e),
	}
	b, _ = json.Marshal(billing)
	return a, p, b, e
}

func TestRepeatedDiagnosticScopeIsSeparate(t *testing.T) {
	revision := strings.Repeat("b", 40)
	a, p, b, e := diagnosticMaterials(true, revision)
	pilot, err := CheckAuthorizedRepeatedDiagnostic(a, p, b, e)
	wantBinding := RepeatedDiagnosticBinding{ObserverRevision: "4d887382300a906ae6d1a33972f3fe4d4aca4e0d", ObserverSourceSHA: "16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9", SyntheticProvenanceSHA: "08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409", PriorResultManifestSHA: "41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8"}
	if err != nil || !pilot.Ready() || !pilot.IsRepeated() || !pilot.IsRepeatedDiagnostic() || !pilot.HumanAuthorized() || pilot.CampaignID() != V5RepeatedDiagnosticID || pilot.ExecutionRevision() != revision || pilot.RepeatedDiagnosticBinding() != wantBinding {
		t.Fatalf("diagnostic scope not ready: %v %v rev=%s binding=%+v", err, pilot.Blockers(), pilot.ExecutionRevision(), pilot.RepeatedDiagnosticBinding())
	}
	if pilot.ExecutionRevision() == wantBinding.ObserverRevision || pilot.RepeatedDiagnosticBinding().ObserverRevision != "4d887382300a906ae6d1a33972f3fe4d4aca4e0d" {
		t.Fatal("observer binding replaced execution revision")
	}
	if err = pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", revision); err != nil {
		t.Fatal(err)
	}
	falseAuth, fp, fb, fe := diagnosticMaterials(false, revision)
	no, err := CheckAuthorizedRepeatedDiagnostic(falseAuth, fp, fb, fe)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("false template became ready", err, no.Ready(), no.HumanAuthorized())
	}
	for _, key := range []string{"schema", "campaign_id", "scope_plan_sha256", "customer_source_revision", "runtime_contract_sha256", "budget_ceiling_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256", "diagnostic_source_revision", "diagnostic_manifest_sha256", "execution_revision", "observer_revision", "observer_source_sha256", "synthetic_provenance_sha256", "prior_result_manifest_sha256"} {
		if _, err = CheckAuthorizedRepeatedDiagnostic(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("bad auth", key)
		}
	}
	if _, err = CheckAuthorizedRepeatedDiagnostic(a, p, alteredJSON(b, "schema", "v5-repeated-cohort-billing-attestation.v1"), e); err == nil {
		t.Fatal("old billing schema accepted")
	}
	if _, err = CheckAuthorizedRepeatedDiagnostic(a, p, alteredJSON(b, "campaign_id", V5RepeatedID), e); err == nil {
		t.Fatal("billing campaign accepted")
	}
	if _, err = CheckAuthorizedRepeatedDiagnostic(a, p, alteredJSON(b, "execution_revision", strings.Repeat("c", 40)), e); err == nil {
		t.Fatal("billing revision accepted")
	}
	ra, rp, rb, re := repeatedInputs(t)
	if _, err = CheckAuthorizedRepeatedDiagnostic(ra, rp, rb, re); err == nil {
		t.Fatal("historical repeated schema accepted")
	}
	old, err := CheckAuthorizedRepeated(ra, rp, rb, re)
	if err != nil || !old.Ready() || !old.IsRepeated() || old.IsRepeatedDiagnostic() || old.CampaignID() != V5RepeatedID {
		t.Fatal("historical repeated checker changed", err, old.IsRepeatedDiagnostic())
	}
	ca, cp, cb, ce := cohortInputs(t)
	if _, err = CheckAuthorizedRepeatedDiagnostic(ca, cp, cb, ce); err == nil {
		t.Fatal("cohort schema accepted")
	}
	ta, tp, tb, te := retestInputs(t)
	if _, err = CheckAuthorizedRepeatedDiagnostic(ta, tp, tb, te); err == nil {
		t.Fatal("retest schema accepted")
	}
	if _, err = CheckAuthorizedRepeated(a, p, b, e); err == nil {
		t.Fatal("diagnostic through repeated checker")
	}
	if _, err = CheckAuthorizedCohort(a, p, b, e); err == nil {
		t.Fatal("diagnostic through cohort checker")
	}
	if _, err = CheckAuthorizedRetest(a, p, b, e); err == nil {
		t.Fatal("diagnostic through retest checker")
	}
	if _, err = CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("diagnostic through legacy checker")
	}
	oa, op, ob, oe := diagnosticMaterials(true, wantBinding.ObserverRevision)
	if _, err = CheckAuthorizedRepeatedDiagnostic(oa, op, ob, oe); err == nil {
		t.Fatal("observer revision accepted as execution revision")
	}
	opts := AuthorizedFirstBatchOptions{Pilot: pilot, Revision: wantBinding.ObserverRevision}
	if _, err = RunAuthorizedRepeatedDiagnostic(context.Background(), "absent", opts); err == nil {
		t.Fatal("observer revision executed as current revision")
	}
	if _, err = RunAuthorizedRepeated(context.Background(), "absent", AuthorizedFirstBatchOptions{Pilot: pilot, Revision: revision}); err == nil {
		t.Fatal("diagnostic through historical runner")
	}
	if _, err = RunAuthorizedRepeatedDiagnostic(context.Background(), "absent", AuthorizedFirstBatchOptions{Pilot: old, Revision: old.ExecutionRevision()}); err == nil {
		t.Fatal("historical repeated through diagnostic runner")
	}
	historical, _ := json.Marshal(AuthorizedFirstBatchReceipt{Schema: "authorized-v5-repeated-cohort.v1"})
	if bytes.Contains(historical, []byte("observer_revision")) || bytes.Contains(historical, []byte("observer_source_sha256")) {
		t.Fatal("historical receipt gained diagnostic binding")
	}
	bound, _ := json.Marshal(AuthorizedFirstBatchReceipt{Schema: "authorized-v5-repeated-export-diagnostic.v1", RepeatedDiagnosticBinding: wantBinding})
	for _, key := range []string{`"observer_revision":"4d887382300a906ae6d1a33972f3fe4d4aca4e0d"`, `"observer_source_sha256":"16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9"`, `"synthetic_provenance_sha256":"08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409"`, `"prior_result_manifest_sha256":"41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8"`, `"diagnostic_source_revision"`} {
		if key == `"diagnostic_source_revision"` {
			continue
		}
		if !bytes.Contains(bound, []byte(key)) {
			t.Fatal("receipt dropped binding", key)
		}
	}
	for _, change := range []string{"cap", "case", "order", "block", "pair", "refund", "target"} {
		t.Run(change, func(t *testing.T) {
			var au, pl map[string]any
			json.Unmarshal(a, &au)
			json.Unmarshal(p, &pl)
			cs := au["cases"].([]any)
			bs := pl["batches"].([]any)
			switch change {
			case "cap":
				au["authorized_spending"].(map[string]any)["max_model_calls"] = 249
				pl["campaign_limits"].(map[string]any)["max_model_calls"] = 249
				cs[0].(map[string]any)["max_model_calls"] = 7
				bs[0].(map[string]any)["model_limits"].(map[string]any)["max_calls"] = 7
			case "case":
				cs[0].(map[string]any)["key"] = "plans-001/answer_complete"
				bs[0].(map[string]any)["key"] = "plans-001/answer_complete"
			case "order":
				cs[0], cs[1] = cs[1], cs[0]
				bs[0], bs[1] = bs[1], bs[0]
			case "block":
				bs[0].(map[string]any)["block_id"] = "block-03"
			case "pair":
				bs[1].(map[string]any)["pair_id"] = bs[0].(map[string]any)["pair_id"]
			case "refund":
				bs[5].(map[string]any)["model_limits"].(map[string]any)["max_micro_cny"] = 405505
			case "target":
				au["target"].(map[string]any)["model"] = "deepseek-chat"
				pl["target"].(map[string]any)["model"] = "deepseek-chat"
			}
			cp, _ := json.Marshal(pl)
			au["scope_plan_sha256"] = conversationHash(cp)
			ca, _ := json.Marshal(au)
			if _, err := CheckAuthorizedRepeatedDiagnostic(ca, cp, alteredJSON(b, "scope_plan_sha256", conversationHash(cp)), e); err == nil {
				t.Fatal("coherent edit accepted", change)
			}
		})
	}
	source, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadPairedProfiles("../../eval/runtime-profiles/2026-10-13-six-profiles-v1")
	if err != nil {
		t.Fatal(err)
	}
	model := pilot.ModelSettings()
	var exact CampaignPreparationPlan
	json.Unmarshal(p, &exact)
	if _, err = PrepareCampaignProposal(source, profiles, model, revision, AuthorizedPilotJEVModel, exact); err != nil {
		t.Fatal("exact diagnostic schedule rejected", err)
	}
	exact.CampaignID = V5RepeatedID
	if _, err = PrepareCampaignProposal(source, profiles, model, revision, AuthorizedPilotJEVModel, exact); err != nil {
		t.Fatal("exact historical schedule rejected", err)
	}
	arbitrary := exact
	arbitrary.CampaignID = ""
	arbitrary.CustomerRevision = ""
	arbitrary.RuntimeContractSHA = ""
	for i := range arbitrary.Batches {
		arbitrary.Batches[i].PairID = ""
		arbitrary.Batches[i].BlockID = ""
		arbitrary.Batches[i].ArmOrder = nil
	}
	if _, err = PrepareCampaignProposal(source, profiles, model, revision, AuthorizedPilotJEVModel, arbitrary); err == nil {
		t.Fatal("arbitrary campaign accepted duplicate case keys")
	}
}
