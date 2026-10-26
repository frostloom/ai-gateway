package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"reflect"
	"regexp"
)

const V5ReceiverRepeatedID = "v5-repeated-receiver-20261027"
const V5ExportDiagnosticClaimSHA = "8c63988dbd31b09cb523c7e0b067f5978ad0ee91f5736f0fc581752544161e6a"

type ReceiverRepeatedBinding struct {
	ReceiverImageID                          string `json:"receiver_image_id,omitempty"`
	ReceiverDeploymentRevision               string `json:"receiver_deployment_revision,omitempty"`
	ReceiverDeploymentManifestSHA            string `json:"receiver_deployment_manifest_sha256,omitempty"`
	ReceiverAdapterManifestSHA               string `json:"receiver_adapter_manifest_sha256,omitempty"`
	PriorReceiverDiagnosticResultManifestSHA string `json:"prior_receiver_diagnostic_result_manifest_sha256,omitempty"`
	UserPreauthorizationSHA                  string `json:"user_preauthorization_sha256,omitempty"`
}

func v5ReceiverRepeatedBinding() ReceiverRepeatedBinding {
	return ReceiverRepeatedBinding{
		ReceiverImageID:                          "sha256:677a486f0519c772b5db1217b028d4b2c8a9db7cd960468aadf6f71e944f532f",
		ReceiverDeploymentRevision:               "416a4a20625d51f5b2735c7bbcea3d6f29bbebbc",
		ReceiverDeploymentManifestSHA:            "4e56dd73f1326423bc7c3f90dcfd53827d1b4fe72f655d9f2985ee94585db314",
		ReceiverAdapterManifestSHA:               "0c374bfad1a3afb0c64582b76dc1ac63fa23292c20e152c0d91b1dc047d5903f",
		PriorReceiverDiagnosticResultManifestSHA: "ce872ae265498687e630a9bb1937553be5d96263957c748c1b4005c048f2290a",
		UserPreauthorizationSHA:                  "8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95",
	}
}
func v5ReceiverRepeatedScope() cashRetestScope {
	s := v5RepeatedDiagnosticScope()
	s.ID = V5ReceiverRepeatedID
	s.BillingSchema = "v5-receiver-repeated-billing-attestation.v1"
	s.ReceiptSchema = "authorized-v5-receiver-repeated.v1"
	return s
}
func (p AuthorizedPilot) IsReceiverRepeated() bool {
	return p.IsRepeated() && p.CampaignID() == V5ReceiverRepeatedID
}
func (p AuthorizedPilot) ReceiverRepeatedBinding() ReceiverRepeatedBinding {
	return p.receiverRepeatedBinding
}

// CheckAuthorizedReceiverRepeated performs no filesystem, credential or network access.
func CheckAuthorizedReceiverRepeated(authRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error) {
	var p AuthorizedPilot
	var a struct {
		cashRetestAuthorization
		Cases []authorizedCase `json:"cases"`
		RepeatedDiagnosticBinding
		ReceiverRepeatedBinding
	}
	fail := func() (AuthorizedPilot, error) {
		return AuthorizedPilot{}, fmt.Errorf("exact fixed receiver-bound repeated authorization required")
	}
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return fail()
	}
	scope := v5ReceiverRepeatedScope()
	if a.Schema != "v5-receiver-repeated-authorization.v1" || a.CampaignID != scope.ID || a.CustomerRevision != scope.CustomerRevision || a.RuntimeContractSHA != scope.RuntimeContractSHA || a.BudgetCeilingSHA != scope.BudgetCeilingSHA || a.OfflineManifestSHA != scope.OfflineManifestSHA || a.OfflineAcceptanceSHA != scope.OfflineAcceptanceSHA || a.DiagnosticSourceRevision != scope.DiagnosticSourceRevision || a.DiagnosticManifestSHA != scope.DiagnosticManifestSHA || a.RepeatedDiagnosticBinding != v5RepeatedDiagnosticBinding() || a.ReceiverRepeatedBinding != v5ReceiverRepeatedBinding() || a.Revision == v5ReceiverRepeatedBinding().ReceiverDeploymentRevision || a.Revision == v5RepeatedDiagnosticBinding().ObserverRevision || a.Key != "" || a.PriorJournalSHA != "" || a.PriorReviewSHA != "" || a.Expand || a.Retry || !a.Target.validPi() || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.Revision) || a.PlanSHA != conversationHash(planRaw) || a.Limits != v5RepeatedLimits() || !reflect.DeepEqual(a.Cases, v5RepeatedCases()) {
		return fail()
	}
	if p.plan.Schema != "campaign-preflight-plan.v2" || p.plan.CampaignID != scope.ID || p.plan.CustomerRevision != scope.CustomerRevision || p.plan.RuntimeContractSHA != scope.RuntimeContractSHA || p.plan.Target == nil || *p.plan.Target != a.Target || p.plan.Limits != v5RepeatedLimits() || len(p.plan.Batches) != 24 {
		return fail()
	}
	if err := validateRepeatedSchedule(p.plan, V5ReceiverRepeatedID); err != nil {
		return fail()
	}
	p.repeated = true
	p.diagnosticBinding = a.RepeatedDiagnosticBinding
	p.receiverRepeatedBinding = a.ReceiverRepeatedBinding
	p.retestScope = scope
	p.retestRevision = a.Revision
	p.retestApproved = a.Approved
	p.model = a.Target.modelSettings()
	p.authorizationSHA = conversationHash(authRaw)
	p.planSHA = a.PlanSHA
	if !a.Approved {
		p.blockers = append(p.blockers, "receiver_repeated_not_authorized")
	}
	checked, err := p.checkBilling(billingRaw, evidenceRaw, true)
	if err == nil && (&agent.Handler{}).EvaluationRuntimeMetadata().ContractSHA256 != scope.RuntimeContractSHA {
		checked.blockers = append(checked.blockers, "runtime_contract_mismatch")
	}
	return checked, err
}

func RunAuthorizedReceiverRepeated(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt, error) {
	if !o.Pilot.Ready() || !o.Pilot.IsReceiverRepeated() || o.Revision != o.Pilot.ExecutionRevision() {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("ready fixed receiver-bound repeated scope and exact revision required")
	}
	return runAuthorizedFixedCohort(ctx, dir, o)
}
