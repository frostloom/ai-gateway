package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"

	"github.com/frostloom/ai-gateway/internal/agent"
)

const V5RepeatedID = "v5-repeated-cohort-20261024"
const V5SixCohortClaimSHA = "b49808feb6a124c60c7f3fd88a7965589368a0ce8d8f4336e94ebaee9a025d34"

func v5RepeatedScope() cashRetestScope {
	return cashRetestScope{
		ID: V5RepeatedID, BillingSchema: "v5-repeated-cohort-billing-attestation.v1", ReceiptSchema: "authorized-v5-repeated-cohort.v1",
		CustomerRevision: "50e3c8dab14b4b6cc25322fd340a274e6970200b", RuntimeContractSHA: "a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f",
		BudgetCeilingSHA: RechargeBudgetCeilingSHA, OfflineManifestSHA: "f7eda76ecdaa846b329f0ed067360417c2ac1a2e492910326f7b33cc6ade525c", OfflineAcceptanceSHA: "8a63f5e7815c10dab9d5eee1dab6dc981fc85fec25efc319936b098bd4d120d6", DiagnosticSourceRevision: RechargeDiagnosticSourceRevision, DiagnosticManifestSHA: RechargeDiagnosticManifestSHA,
	}
}
func v5RepeatedCases() []authorizedCase {
	var out []authorizedCase
	for block := 0; block < 4; block++ {
		for pos := 0; pos < 6; pos++ {
			c := v5CohortCases()[(block+pos)%6]
			c.ID = fmt.Sprintf("batch-%03d", len(out)+1)
			out = append(out, c)
		}
	}
	return out
}
func v5RepeatedBatches() []CampaignPreparationBatch {
	var out []CampaignPreparationBatch
	for i, c := range v5RepeatedCases() {
		block, pos := i/6, i%6
		arms := []string{"model", "jev_selective"}
		if block%2 == 1 {
			arms = []string{"jev_selective", "model"}
		}
		out = append(out, CampaignPreparationBatch{Key: c.Key, ModelLimits: PairBudgetLimits{MaxCalls: c.ModelCalls, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: int64(c.ModelCalls) * 33792, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}, MaxJEVCalls: 1, PairID: fmt.Sprintf("block-%02d-pair-%02d", block, pos), BlockID: fmt.Sprintf("block-%02d", block), ArmOrder: arms})
	}
	return out
}
func validateRepeatedSchedule(p CampaignPreparationPlan) error {
	scope := v5RepeatedScope()
	if (p.CampaignID != V5RepeatedID && p.CampaignID != V5RepeatedDiagnosticID) || p.Schema != "campaign-preflight-plan.v2" || p.CustomerRevision != scope.CustomerRevision || p.RuntimeContractSHA != scope.RuntimeContractSHA || p.Limits != v5RepeatedLimits() || !reflect.DeepEqual(p.Batches, v5RepeatedBatches()) {
		return fmt.Errorf("fixed repeated schedule required")
	}
	return nil
}
func validateRepeatedPlan(p CampaignPreparationPlan) error {
	if p.CampaignID != V5RepeatedID {
		return fmt.Errorf("fixed repeated schedule required")
	}
	return validateRepeatedSchedule(p)
}
func v5RepeatedLimits() CampaignLimits {
	return CampaignLimits{MaxBatches: 24, MaxModelCalls: 248, MaxModelMicroCNY: 8380416, MaxJEVCalls: 24, MaxJEVMicroUSD: 64512, JEVMicroUSDPerAttempt: 2688}
}
func (p AuthorizedPilot) IsRepeated() bool { return p.repeated }

// The reviewed cohort cannot enter through a single-case or historical checker.
// Pure checking reads no credentials, fixture, filesystem or mutable budget.
func CheckAuthorizedRepeated(authRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error) {
	var p AuthorizedPilot
	var a struct {
		cashRetestAuthorization
		Cases []authorizedCase `json:"cases"`
	}
	fail := func() (AuthorizedPilot, error) {
		return AuthorizedPilot{}, fmt.Errorf("exact fixed 24-pair repeated authorization required")
	}
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return fail()
	}
	scope := v5RepeatedScope()
	if a.Schema != "v5-repeated-cohort-authorization.v1" || a.CampaignID != scope.ID || a.CustomerRevision != scope.CustomerRevision || a.RuntimeContractSHA != scope.RuntimeContractSHA || a.BudgetCeilingSHA != scope.BudgetCeilingSHA || a.OfflineManifestSHA != scope.OfflineManifestSHA || a.OfflineAcceptanceSHA != scope.OfflineAcceptanceSHA || a.DiagnosticSourceRevision != scope.DiagnosticSourceRevision || a.DiagnosticManifestSHA != scope.DiagnosticManifestSHA || a.Key != "" || a.PriorJournalSHA != "" || a.PriorReviewSHA != "" || a.Expand || a.Retry || !a.Target.validPi() || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.Revision) || a.PlanSHA != conversationHash(planRaw) || a.Limits != v5RepeatedLimits() || !reflect.DeepEqual(a.Cases, v5RepeatedCases()) {
		return fail()
	}
	if p.plan.Schema != "campaign-preflight-plan.v2" || p.plan.CampaignID != scope.ID || p.plan.CustomerRevision != scope.CustomerRevision || p.plan.RuntimeContractSHA != scope.RuntimeContractSHA || p.plan.Target == nil || *p.plan.Target != a.Target || p.plan.Limits != v5RepeatedLimits() || len(p.plan.Batches) != 24 {
		return fail()
	}
	if err := validateRepeatedPlan(p.plan); err != nil {
		return fail()
	}
	p.repeated = true
	p.retestScope = scope
	p.retestRevision = a.Revision
	p.retestApproved = a.Approved
	p.model = a.Target.modelSettings()
	p.authorizationSHA = conversationHash(authRaw)
	p.planSHA = a.PlanSHA
	if !a.Approved {
		p.blockers = append(p.blockers, "repeated_not_authorized")
	}
	checked, err := p.checkBilling(billingRaw, evidenceRaw, true)
	if err == nil && (&agent.Handler{}).EvaluationRuntimeMetadata().ContractSHA256 != scope.RuntimeContractSHA {
		checked.blockers = append(checked.blockers, "runtime_contract_mismatch")
	}
	return checked, err
}

func validRepeatedIdentity(pair, block string) bool {
	return regexp.MustCompile(`^block-0[0-3]$`).MatchString(block) && regexp.MustCompile(`^block-0[0-3]-pair-0[0-5]$`).MatchString(pair) && pair[:8] == block
}
