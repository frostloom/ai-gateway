package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"

	"github.com/frostloom/ai-gateway/internal/agent"
)

const V5RepeatedDiagnosticID = "v5-repeated-export-diagnostic-20261025"
const V5RepeatedClaimSHA = "c7f14ee3b310cec23cf71b7dc004fe73b29ec5e8efa4b6c4167eb91159c8dd58"

// RepeatedDiagnosticBinding pins the already committed observer, synthetic
// delivery and stopped-result archives. It is not an execution revision.
type RepeatedDiagnosticBinding struct {
	ObserverRevision       string `json:"observer_revision,omitempty"`
	ObserverSourceSHA      string `json:"observer_source_sha256,omitempty"`
	SyntheticProvenanceSHA string `json:"synthetic_provenance_sha256,omitempty"`
	PriorResultManifestSHA string `json:"prior_result_manifest_sha256,omitempty"`
}

func v5RepeatedDiagnosticBinding() RepeatedDiagnosticBinding {
	return RepeatedDiagnosticBinding{
		ObserverRevision:       "4d887382300a906ae6d1a33972f3fe4d4aca4e0d",
		ObserverSourceSHA:      "16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9",
		SyntheticProvenanceSHA: "08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409",
		PriorResultManifestSHA: "41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8",
	}
}

func v5RepeatedDiagnosticScope() cashRetestScope {
	scope := v5RepeatedScope()
	scope.ID = V5RepeatedDiagnosticID
	scope.BillingSchema = "v5-repeated-export-diagnostic-billing-attestation.v1"
	scope.ReceiptSchema = "authorized-v5-repeated-export-diagnostic.v1"
	return scope
}

func (p AuthorizedPilot) IsRepeatedDiagnostic() bool { return p.repeatedDiagnostic }
func (p AuthorizedPilot) RepeatedDiagnosticBinding() RepeatedDiagnosticBinding {
	return p.diagnosticBinding
}

// CheckAuthorizedRepeatedDiagnostic is pure. A false template can be checked
// and stays unready; it reads no credentials, fixtures or claims.
func CheckAuthorizedRepeatedDiagnostic(authRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error) {
	var p AuthorizedPilot
	var a struct {
		cashRetestAuthorization
		Cases []authorizedCase `json:"cases"`
		RepeatedDiagnosticBinding
	}
	fail := func() (AuthorizedPilot, error) {
		return AuthorizedPilot{}, fmt.Errorf("exact fixed 24-pair repeated export diagnostic authorization required")
	}
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return fail()
	}
	scope := v5RepeatedDiagnosticScope()
	if a.Schema != "v5-repeated-export-diagnostic-authorization.v1" || a.CampaignID != scope.ID || a.CustomerRevision != scope.CustomerRevision || a.RuntimeContractSHA != scope.RuntimeContractSHA || a.BudgetCeilingSHA != scope.BudgetCeilingSHA || a.OfflineManifestSHA != scope.OfflineManifestSHA || a.OfflineAcceptanceSHA != scope.OfflineAcceptanceSHA || a.DiagnosticSourceRevision != scope.DiagnosticSourceRevision || a.DiagnosticManifestSHA != scope.DiagnosticManifestSHA || a.RepeatedDiagnosticBinding != v5RepeatedDiagnosticBinding() || a.Key != "" || a.PriorJournalSHA != "" || a.PriorReviewSHA != "" || a.Expand || a.Retry || !a.Target.validPi() || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.Revision) || a.PlanSHA != conversationHash(planRaw) || a.Limits != v5RepeatedLimits() || !reflect.DeepEqual(a.Cases, v5RepeatedCases()) {
		return fail()
	}
	if p.plan.Schema != "campaign-preflight-plan.v2" || p.plan.CampaignID != scope.ID || p.plan.CustomerRevision != scope.CustomerRevision || p.plan.RuntimeContractSHA != scope.RuntimeContractSHA || p.plan.Target == nil || *p.plan.Target != a.Target || p.plan.Limits != v5RepeatedLimits() || len(p.plan.Batches) != 24 {
		return fail()
	}
	if err := validateRepeatedSchedule(p.plan); err != nil {
		return fail()
	}
	p.repeated = true
	p.repeatedDiagnostic = true
	p.diagnosticBinding = v5RepeatedDiagnosticBinding()
	p.retestScope = scope
	p.retestRevision = a.Revision
	p.retestApproved = a.Approved
	p.model = a.Target.modelSettings()
	p.authorizationSHA = conversationHash(authRaw)
	p.planSHA = a.PlanSHA
	if !a.Approved {
		p.blockers = append(p.blockers, "repeated_diagnostic_not_authorized")
	}
	checked, err := p.checkBilling(billingRaw, evidenceRaw, true)
	if err == nil && (&agent.Handler{}).EvaluationRuntimeMetadata().ContractSHA256 != scope.RuntimeContractSHA {
		checked.blockers = append(checked.blockers, "runtime_contract_mismatch")
	}
	return checked, err
}

func RunAuthorizedRepeatedDiagnostic(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt, error) {
	if !o.Pilot.Ready() || !o.Pilot.IsRepeatedDiagnostic() || o.Pilot.CampaignID() != V5RepeatedDiagnosticID || o.Revision != o.Pilot.ExecutionRevision() {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("ready fixed repeated export diagnostic scope and exact revision required")
	}
	return runAuthorizedFixedCohort(ctx, dir, o)
}
