package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/frostloom/ai-gateway/internal/agent"
)

const CashReadRetestID = "cash-read-retest-20261023"
const CashReadRecoveryRetestID = "cash-read-recovery-retest-20261023"

type cashRetestScope struct {
	ID, BillingSchema, ReceiptSchema                           string
	CaseKey                                                    string
	ModelCalls                                                 int
	ModelMicroCNY                                              int64
	BudgetCeilingSHA, OfflineManifestSHA, OfflineAcceptanceSHA string
	PriorJournalSHA, PriorReviewSHA                            string
	CustomerRevision, RuntimeContractSHA                       string
}

// Only separately reviewed fixed scopes are accepted, never a user-selected retry ID.
func checkedCashRetestScope(schema, id string) (cashRetestScope, bool) {
	if schema == "cash-read-retest-authorization.v1" && id == CashReadRetestID {
		return cashRetestScope{
			ID: id, BillingSchema: "paired-retest-billing-attestation.v1", ReceiptSchema: "authorized-cash-retest.v1",
			PriorJournalSHA: priorCashJournalSHA, PriorReviewSHA: priorCashReviewSHA,
		}, true
	}
	if schema == "cash-read-retest-authorization.v2" && id == CashReadRecoveryRetestID {
		return cashRetestScope{
			ID: id, BillingSchema: "paired-retest-billing-attestation.v2", ReceiptSchema: "authorized-cash-retest.v2",
			PriorJournalSHA:    "7d48f9caff31099149a06a3b14e3e9a5367b4a4b12067537130bba6453a9f754",
			PriorReviewSHA:     "785834102ecae9104df8e33239a7089ace5503cc8d33c5a6850862e77f24b6f6",
			CustomerRevision:   "4c715556d893a5bf8dadcd6a164310692ec2570a",
			RuntimeContractSHA: "b92458d1bafd3187a2daa6387b0575e9517da84f66df6602b1945a8d47ba9d8e",
		}, true
	}
	return checkedRechargeScope(schema, id)
}

type cashRetestPlanIdentity struct {
	ID                 string `json:"campaign_id"`
	CustomerRevision   string `json:"customer_source_revision"`
	RuntimeContractSHA string `json:"runtime_contract_sha256"`
}

const cashRetestCase = "balance-020/explicit_scope_followup"
const priorCashJournalSHA = "93a6d5df6c9d68fd5202dd023424a8af77de5a1170dd01993e7334b4804b66d2"
const priorCashReviewSHA = "d0ea747fd5f59d10382b8b0cf1b6d94d7df8e68c739a64df8b4c8289b230b673"

type cashRetestAuthorization struct {
	Schema               string         `json:"schema"`
	BudgetCeilingSHA     string         `json:"budget_ceiling_sha256,omitempty"`
	OfflineManifestSHA   string         `json:"prior_offline_manifest_sha256,omitempty"`
	OfflineAcceptanceSHA string         `json:"prior_offline_acceptance_sha256,omitempty"`
	CustomerRevision     string         `json:"customer_source_revision,omitempty"`
	RuntimeContractSHA   string         `json:"runtime_contract_sha256,omitempty"`
	CampaignID           string         `json:"campaign_id"`
	Approved             bool           `json:"human_authorized"`
	PlanSHA              string         `json:"scope_plan_sha256"`
	Target               PilotTarget    `json:"target"`
	Limits               CampaignLimits `json:"authorized_spending"`
	Key                  string         `json:"case_key"`
	Revision             string         `json:"execution_revision"`
	PriorJournalSHA      string         `json:"prior_journal_sha256"`
	PriorReviewSHA       string         `json:"prior_review_sha256"`
	Expand               bool           `json:"automatic_scope_expansion"`
	Retry                bool           `json:"automatic_paid_retries"`
}

func (p AuthorizedPilot) IsRetest() bool { return p.retestRevision != "" }
func (p AuthorizedPilot) HumanAuthorized() bool {
	return p.valid && (!p.IsRetest() || p.retestApproved)
}
func (p AuthorizedPilot) CampaignID() string {
	if p.IsRetest() {
		return p.retestScope.ID
	}
	return ""
}
func (p AuthorizedPilot) ExecutionRevision() string { return p.retestRevision }

// CheckAuthorizedRetest is pure. Unapproved input may be checked but can never
// become Ready; the private snapshot cannot be upgraded through public flags.
func CheckAuthorizedRetest(authRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error) {
	var p AuthorizedPilot
	var a cashRetestAuthorization
	fail := func() (AuthorizedPilot, error) {
		return AuthorizedPilot{}, fmt.Errorf("exact separately authorized evaluation scope required")
	}
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return fail()
	}
	scope, ok := checkedCashRetestScope(a.Schema, a.CampaignID)
	if !ok || a.CustomerRevision != scope.CustomerRevision || a.RuntimeContractSHA != scope.RuntimeContractSHA || a.BudgetCeilingSHA != scope.BudgetCeilingSHA || a.OfflineManifestSHA != scope.OfflineManifestSHA || a.OfflineAcceptanceSHA != scope.OfflineAcceptanceSHA {
		return fail()
	}
	wantIdentity := cashRetestPlanIdentity{}
	contractMismatch := false
	if scope.RuntimeContractSHA != "" {
		wantIdentity = cashRetestPlanIdentity{scope.ID, scope.CustomerRevision, scope.RuntimeContractSHA}
		contractMismatch = (&agent.Handler{}).EvaluationRuntimeMetadata().ContractSHA256 != scope.RuntimeContractSHA
	}
	if (cashRetestPlanIdentity{p.plan.CampaignID, p.plan.CustomerRevision, p.plan.RuntimeContractSHA}) != wantIdentity {
		return fail()
	}
	caseKey, calls, reserve := cashRetestCase, 12, int64(405504)
	if scope.CaseKey != "" {
		caseKey, calls, reserve = scope.CaseKey, scope.ModelCalls, scope.ModelMicroCNY
	}
	limits := CampaignLimits{MaxBatches: 1, MaxModelCalls: calls, MaxModelMicroCNY: reserve, MaxJEVCalls: 1, MaxJEVMicroUSD: 2688, JEVMicroUSDPerAttempt: 2688}
	if a.PlanSHA != conversationHash(planRaw) || a.Key != caseKey || !a.Target.validPi() || a.Limits != limits || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.Revision) || a.PriorJournalSHA != scope.PriorJournalSHA || a.PriorReviewSHA != scope.PriorReviewSHA || a.Expand || a.Retry {
		return fail()
	}
	want := PairBudgetLimits{MaxCalls: calls, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: reserve, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
	if p.plan.Schema != "campaign-preflight-plan.v2" || p.plan.Target == nil || *p.plan.Target != a.Target || p.plan.Limits != limits || len(p.plan.Batches) != 1 || p.plan.Batches[0].Key != caseKey || p.plan.Batches[0].ModelLimits != want || p.plan.Batches[0].MaxJEVCalls != 1 {
		return fail()
	}
	p.model = a.Target.modelSettings()
	p.authorizationSHA = conversationHash(authRaw)
	p.planSHA = a.PlanSHA
	p.retestScope = scope
	p.retestRevision = a.Revision
	p.retestApproved = a.Approved
	if !a.Approved {
		p.blockers = append(p.blockers, "retest_not_authorized")
	}
	checked, err := p.checkBilling(billingRaw, evidenceRaw, true)
	if err == nil && contractMismatch {
		checked.blockers = append(checked.blockers, "runtime_contract_mismatch")
	}
	return checked, err
}

// The CLI must durably claim the fixed identity before constructing fixtures.
// Like the original runner, dependencies are trusted caller-owned resources.
func RunAuthorizedRetest(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt, error) {
	if !o.Pilot.IsRetest() {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("separate evaluation permission required")
	}
	return runAuthorizedBatch(ctx, dir, o, nil)
}
