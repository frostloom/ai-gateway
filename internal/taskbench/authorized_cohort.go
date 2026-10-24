package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"

	"github.com/frostloom/ai-gateway/internal/agent"
)

const V5SixCohortID = "v5-six-case-cohort-20261024"
const RefundCapClaimSHA = "9a026381dea69bc1feb9712d58a41e305877701e1355ba2464543aaaa0857b30"

func v5CohortScope() cashRetestScope {
	return cashRetestScope{
		ID: V5SixCohortID, BillingSchema: "v5-six-case-cohort-billing-attestation.v1", ReceiptSchema: "authorized-v5-six-case-cohort.v1",
		CustomerRevision: "50e3c8dab14b4b6cc25322fd340a274e6970200b", RuntimeContractSHA: "a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f",
		BudgetCeilingSHA: RechargeBudgetCeilingSHA, OfflineManifestSHA: "7643f8e596e879e27b7b025e0c91e6c245a449645ac05a1df6b610b3978a1bf3", OfflineAcceptanceSHA: "0cf9d0b8587709f607a55d7fe175a7da927e279f8893a60fc7dde63800210360", DiagnosticSourceRevision: RechargeDiagnosticSourceRevision, DiagnosticManifestSHA: RechargeDiagnosticManifestSHA,
	}
}
func v5CohortCases() []authorizedCase {
	keys := []string{"balance-001/answer_complete", "balance-020/explicit_scope_followup", "recharge-002/confirm", "recharge-002/cancel", "recharge-001/supplement_then_confirm", "refund-001/balance_cap"}
	caps := []int{6, 12, 10, 10, 12, 12}
	out := make([]authorizedCase, 6)
	for i := range keys {
		out[i] = authorizedCase{ID: fmt.Sprintf("batch-%03d", i+1), Key: keys[i], ModelCalls: caps[i], JEVCalls: 1}
	}
	return out
}
func v5CohortLimits() CampaignLimits {
	return CampaignLimits{MaxBatches: 6, MaxModelCalls: 62, MaxModelMicroCNY: 2095104, MaxJEVCalls: 6, MaxJEVMicroUSD: 16128, JEVMicroUSDPerAttempt: 2688}
}
func (p AuthorizedPilot) IsCohort() bool { return p.cohort }

// The reviewed cohort cannot enter through a single-case or historical checker.
// Pure checking reads no credentials, fixture, filesystem or mutable budget.
func CheckAuthorizedCohort(authRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error) {
	var p AuthorizedPilot
	var a struct {
		cashRetestAuthorization
		Cases []authorizedCase `json:"cases"`
	}
	fail := func() (AuthorizedPilot, error) {
		return AuthorizedPilot{}, fmt.Errorf("exact fixed six-case cohort authorization required")
	}
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return fail()
	}
	scope := v5CohortScope()
	if a.Schema != "v5-six-case-cohort-authorization.v1" || a.CampaignID != scope.ID || a.CustomerRevision != scope.CustomerRevision || a.RuntimeContractSHA != scope.RuntimeContractSHA || a.BudgetCeilingSHA != scope.BudgetCeilingSHA || a.OfflineManifestSHA != scope.OfflineManifestSHA || a.OfflineAcceptanceSHA != scope.OfflineAcceptanceSHA || a.DiagnosticSourceRevision != scope.DiagnosticSourceRevision || a.DiagnosticManifestSHA != scope.DiagnosticManifestSHA || a.Key != "" || a.PriorJournalSHA != "" || a.PriorReviewSHA != "" || a.Expand || a.Retry || !a.Target.validPi() || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.Revision) || a.PlanSHA != conversationHash(planRaw) || a.Limits != v5CohortLimits() || !reflect.DeepEqual(a.Cases, v5CohortCases()) {
		return fail()
	}
	if p.plan.Schema != "campaign-preflight-plan.v2" || p.plan.CampaignID != scope.ID || p.plan.CustomerRevision != scope.CustomerRevision || p.plan.RuntimeContractSHA != scope.RuntimeContractSHA || p.plan.Target == nil || *p.plan.Target != a.Target || p.plan.Limits != v5CohortLimits() || len(p.plan.Batches) != 6 {
		return fail()
	}
	for i, c := range v5CohortCases() {
		want := PairBudgetLimits{MaxCalls: c.ModelCalls, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: int64(c.ModelCalls) * 33792, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
		b := p.plan.Batches[i]
		if b.Key != c.Key || b.ModelLimits != want || b.MaxJEVCalls != 1 {
			return fail()
		}
	}
	p.cohort = true
	p.retestScope = scope
	p.retestRevision = a.Revision
	p.retestApproved = a.Approved
	p.model = a.Target.modelSettings()
	p.authorizationSHA = conversationHash(authRaw)
	p.planSHA = a.PlanSHA
	if !a.Approved {
		p.blockers = append(p.blockers, "cohort_not_authorized")
	}
	checked, err := p.checkBilling(billingRaw, evidenceRaw, true)
	if err == nil && (&agent.Handler{}).EvaluationRuntimeMetadata().ContractSHA256 != scope.RuntimeContractSHA {
		checked.blockers = append(checked.blockers, "runtime_contract_mismatch")
	}
	return checked, err
}
