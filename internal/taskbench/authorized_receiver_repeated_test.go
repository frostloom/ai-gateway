package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func receiverInputs(t *testing.T) (a, p, b, e []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-27-receiver-repeated-entry"
	read := func(f string) []byte {
		raw, err := os.ReadFile(filepath.Join(base, f))
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
func TestReceiverRepeatedScopeBindings(t *testing.T) {
	a, p, b, e := receiverInputs(t)
	pilot, err := CheckAuthorizedReceiverRepeated(a, p, b, e)
	if err != nil || !pilot.Ready() || !pilot.IsReceiverRepeated() || pilot.IsRepeatedDiagnostic() {
		t.Fatal("new isolated scope", err, pilot.Blockers())
	}
	if err = pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	no, err := CheckAuthorizedReceiverRepeated(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() {
		t.Fatal("false consent bypass", err)
	}
	for _, key := range []string{"receiver_image_id", "receiver_deployment_revision", "receiver_deployment_manifest_sha256", "receiver_adapter_manifest_sha256", "prior_receiver_diagnostic_result_manifest_sha256", "user_preauthorization_sha256", "observer_revision", "campaign_id", "schema", "scope_plan_sha256", "runtime_contract_sha256"} {
		if _, err = CheckAuthorizedReceiverRepeated(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("altered binding accepted", key)
		}
	}
	for _, check := range []func([]byte, []byte, []byte, []byte) (AuthorizedPilot, error){CheckAuthorizedRepeatedDiagnostic, CheckAuthorizedRepeated, CheckAuthorizedCohort, CheckAuthorizedRetest, CheckAuthorizedPilot} {
		if _, err = check(a, p, b, e); err == nil {
			t.Fatal("new scope entered old checker")
		}
	}
	oa, op, ob, oe := diagnosticInputs(t)
	if _, err = CheckAuthorizedReceiverRepeated(oa, op, ob, oe); err == nil {
		t.Fatal("old approval reused")
	}
	var auth, plan map[string]any
	json.Unmarshal(a, &auth)
	json.Unmarshal(p, &plan)
	bs := plan["batches"].([]any)
	bs[0].(map[string]any)["arm_order"] = []string{"jev_selective", "model"}
	changed, _ := json.Marshal(plan)
	auth["scope_plan_sha256"] = conversationHash(changed)
	ca, _ := json.Marshal(auth)
	if _, err = CheckAuthorizedReceiverRepeated(ca, changed, alteredJSON(b, "scope_plan_sha256", conversationHash(changed)), e); err == nil {
		t.Fatal("coherent schedule tamper accepted")
	}
}
func TestReceiverRepeatedBindingArchiveHashes(t *testing.T) {
	b := v5ReceiverRepeatedBinding()
	for file, want := range map[string]string{"../../eval/paired-pilots/2026-10-27-receiver-deployment/manifest.json": b.ReceiverDeploymentManifestSHA, "../../eval/paired-pilots/2026-10-26-otel-receiver-adapter/manifest.json": b.ReceiverAdapterManifestSHA, "../../eval/paired-pilots/2026-10-26-repeated-diagnostic-live-result/manifest.json": b.PriorReceiverDiagnosticResultManifestSHA} {
		raw, err := os.ReadFile(file)
		if err != nil || conversationHash(raw) != want {
			t.Fatal("receiver evidence binding changed", file, err)
		}
	}
	a, p, bl, e := receiverInputs(t)
	if _, err := CheckAuthorizedReceiverRepeated(alteredJSON(a, "execution_revision", b.ReceiverDeploymentRevision), p, alteredJSON(bl, "execution_revision", b.ReceiverDeploymentRevision), e); err == nil {
		t.Fatal("deployment-only revision cannot be executor")
	}
}
func TestReceiverReceiptOmitHistoricalFields(t *testing.T) {
	raw, err := json.Marshal(AuthorizedFirstBatchReceipt{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(raw, &m)
	for key := range map[string]any{"receiver_image_id": nil, "receiver_deployment_revision": nil, "receiver_deployment_manifest_sha256": nil, "receiver_adapter_manifest_sha256": nil, "prior_receiver_diagnostic_result_manifest_sha256": nil, "user_preauthorization_sha256": nil} {
		if _, ok := m[key]; ok {
			t.Fatal("old JSON changed", key)
		}
	}
}
