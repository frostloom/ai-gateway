package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

const CashReadRetestID = "cash-read-retest-20261023"
const cashRetestCase = "balance-020/explicit_scope_followup"
const priorCashJournalSHA = "93a6d5df6c9d68fd5202dd023424a8af77de5a1170dd01993e7334b4804b66d2"
const priorCashReviewSHA = "d0ea747fd5f59d10382b8b0cf1b6d94d7df8e68c739a64df8b4c8289b230b673"

type cashRetestAuthorization struct {
	Schema          string         `json:"schema"`
	CampaignID      string         `json:"campaign_id"`
	Approved        bool           `json:"human_authorized"`
	PlanSHA         string         `json:"scope_plan_sha256"`
	Target          PilotTarget    `json:"target"`
	Limits          CampaignLimits `json:"authorized_spending"`
	Key             string         `json:"case_key"`
	Revision        string         `json:"execution_revision"`
	PriorJournalSHA string         `json:"prior_journal_sha256"`
	PriorReviewSHA  string         `json:"prior_review_sha256"`
	Expand          bool           `json:"automatic_scope_expansion"`
	Retry           bool           `json:"automatic_paid_retries"`
}

func (p AuthorizedPilot) IsRetest() bool { return p.retestRevision != "" }
func (p AuthorizedPilot) HumanAuthorized() bool {
	return p.valid && (!p.IsRetest() || p.retestApproved)
}
func (p AuthorizedPilot) CampaignID() string {
	if p.IsRetest() {
		return CashReadRetestID
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
		return AuthorizedPilot{}, fmt.Errorf("exact separately authorized cash retest scope required")
	}
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return fail()
	}
	limits := CampaignLimits{MaxBatches: 1, MaxModelCalls: 12, MaxModelMicroCNY: 405504, MaxJEVCalls: 1, MaxJEVMicroUSD: 2688, JEVMicroUSDPerAttempt: 2688}
	if a.Schema != "cash-read-retest-authorization.v1" || a.CampaignID != CashReadRetestID || a.PlanSHA != conversationHash(planRaw) || a.Key != cashRetestCase || !a.Target.validPi() || a.Limits != limits || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.Revision) || a.PriorJournalSHA != priorCashJournalSHA || a.PriorReviewSHA != priorCashReviewSHA || a.Expand || a.Retry {
		return fail()
	}
	want := PairBudgetLimits{MaxCalls: 12, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: 405504, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
	if p.plan.Schema != "campaign-preflight-plan.v2" || p.plan.Target == nil || *p.plan.Target != a.Target || p.plan.Limits != limits || len(p.plan.Batches) != 1 || p.plan.Batches[0].Key != cashRetestCase || p.plan.Batches[0].ModelLimits != want || p.plan.Batches[0].MaxJEVCalls != 1 {
		return fail()
	}
	p.model = a.Target.modelSettings()
	p.authorizationSHA = conversationHash(authRaw)
	p.planSHA = a.PlanSHA
	p.retestRevision = a.Revision
	p.retestApproved = a.Approved
	if !a.Approved {
		p.blockers = append(p.blockers, "retest_not_authorized")
	}
	return p.checkBilling(billingRaw, evidenceRaw, true)
}

// The CLI must durably claim the fixed identity before constructing fixtures.
// Like the original runner, dependencies are trusted caller-owned resources.
func RunAuthorizedRetest(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt, error) {
	if !o.Pilot.IsRetest() {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("separate cash retest permission required")
	}
	return runAuthorizedBatch(ctx, dir, o, nil)
}
