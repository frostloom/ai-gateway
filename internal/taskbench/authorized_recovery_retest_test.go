package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoveryRetestInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-23-cash-read-recovery-retest-offline"
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	a, p, b := read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e := []byte("synthetic recovery retest evidence only")
	a = alteredJSON(a, "human_authorized", true)
	a = alteredJSON(a, "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(b, "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(b, "account_tariff_verified", true)
	b = alteredJSON(b, "token_envelope_verified", true)
	b = alteredJSON(b, "evidence_sha256", conversationHash(e))
	return a, p, b, e
}

func TestRecoveryRetestExactNewScope(t *testing.T) {
	a, p, b, e := recoveryRetestInputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || !pilot.Ready() || pilot.CampaignID() != "cash-read-recovery-retest-20261023" {
		t.Fatalf("v2 rejected: %v", err)
	}
	unapproved, err := CheckAuthorizedRetest(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || unapproved.Ready() || unapproved.HumanAuthorized() {
		t.Fatal("false consent enabled execution")
	}
	for _, key := range []string{"schema", "campaign_id", "customer_source_revision", "runtime_contract_sha256", "prior_journal_sha256", "prior_review_sha256", "scope_plan_sha256", "execution_revision"} {
		if _, err := CheckAuthorizedRetest(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("accepted changed authorization", key)
		}
	}
	for _, key := range []string{"campaign_id", "customer_source_revision", "runtime_contract_sha256"} {
		changed := alteredJSON(p, key, "changed")
		aa := alteredJSON(a, "scope_plan_sha256", conversationHash(changed))
		bb := alteredJSON(b, "scope_plan_sha256", conversationHash(changed))
		if _, err := CheckAuthorizedRetest(aa, changed, bb, e); err == nil {
			t.Fatal("rebound changed plan accepted", key)
		}
	}
	for _, key := range []string{"schema", "campaign_id", "runtime_contract_sha256", "execution_revision", "scope_plan_sha256"} {
		if _, err := CheckAuthorizedRetest(a, p, alteredJSON(b, key, "changed"), e); err == nil {
			t.Fatal("billing changed", key)
		}
	}
	oldA, oldP, oldB, oldE := retestInputs(t)
	if _, err := CheckAuthorizedRetest(oldA, oldP, oldB, oldE); err != nil {
		t.Fatal("legacy broken", err)
	}
	if _, err := CheckAuthorizedRetest(a, p, oldB, oldE); err == nil {
		t.Fatal("v2 inherited old billing")
	}
	if _, err := CheckAuthorizedRetest(oldA, oldP, b, e); err == nil {
		t.Fatal("v1 inherited new billing")
	}
	// Rebinding byte hashes cannot turn a v2 plan into the consumed v1 scope.
	oa := alteredJSON(oldA, "scope_plan_sha256", conversationHash(p))
	ob := alteredJSON(oldB, "scope_plan_sha256", conversationHash(p))
	if _, err := CheckAuthorizedRetest(oa, p, ob, oldE); err == nil {
		t.Fatal("v1 accepted v2 plan")
	}
	if _, err := CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("legacy pilot accepted new retest")
	}
}

func TestRecoveryRetestReferencesActualFailedEvidence(t *testing.T) {
	a, _, _, _ := recoveryRetestInputs(t)
	var auth cashRetestAuthorization
	if json.Unmarshal(a, &auth) != nil {
		t.Fatal("auth")
	}
	raw, err := os.ReadFile("../../eval/paired-pilots/2026-10-23-cash-read-retest-live-result/independent-review.json")
	if err != nil {
		t.Fatal(err)
	}
	var review struct {
		JournalSHA string `json:"journal_sha256"`
	}
	json.Unmarshal(raw, &review)
	if auth.PriorReviewSHA != conversationHash(raw) || auth.PriorJournalSHA != review.JournalSHA {
		t.Fatal("scope not bound to actual latest failed review")
	}
}
