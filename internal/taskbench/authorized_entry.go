package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const (
	AuthorizedPilotModelBase = "https://maas.qianwenaiapi.com/apps/anthropic"
	AuthorizedPilotModel     = "deepseek-v4.1-flash"
	AuthorizedPilotJEVModel  = "jev-1.13.0"
)

type authorizedCase struct {
	ID         string `json:"batch_id"`
	Key        string `json:"key"`
	ModelCalls int    `json:"max_model_calls"`
	JEVCalls   int    `json:"max_jev_calls"`
}
type pilotAuthorization struct {
	Schema   string           `json:"schema"`
	Approved bool             `json:"human_authorized"`
	Limits   CampaignLimits   `json:"authorized_spending"`
	Cases    []authorizedCase `json:"cases"`
	PlanSHA  string           `json:"scope_plan_sha256"`
	Base     string           `json:"model_base"`
	Model    string           `json:"model"`
	Protocol string           `json:"protocol"`
	JEVModel string           `json:"jev_model"`
	Expand   bool             `json:"automatic_scope_expansion"`
	Retry    bool             `json:"automatic_paid_retries"`
}
type pilotBilling struct {
	CampaignID         string `json:"campaign_id,omitempty"`
	RuntimeContractSHA string `json:"runtime_contract_sha256,omitempty"`
	Revision           string `json:"execution_revision,omitempty"`
	Schema             string `json:"schema"`
	PlanSHA            string `json:"scope_plan_sha256"`
	Base               string `json:"model_base"`
	Model              string `json:"model"`
	Protocol           string `json:"protocol"`
	JEVModel           string `json:"jev_model"`
	Tariff             bool   `json:"account_tariff_verified"`
	Tokens             bool   `json:"token_envelope_verified"`
	Failure            bool   `json:"jev_failure_charges_verified"`
	FailurePolicy      string `json:"jev_failure_policy"`
	JEVMaxInput        int64  `json:"jev_max_input_tokens"`
	JEVInputRate       int64  `json:"jev_input_micro_usd_per_million"`
	JEVFullAttempt     int64  `json:"jev_full_attempt_micro_usd"`
	EvidenceSHA        string `json:"evidence_sha256"`
}

// AuthorizedPilot can only become valid through an authorization checker. Its
// private snapshot cannot be upgraded by mutating public readiness flags.
type AuthorizedPilot struct {
	valid                                              bool
	retestApproved                                     bool
	retestRevision                                     string
	retestScope                                        cashRetestScope
	plan                                               CampaignPreparationPlan
	model                                              PairModelSettings
	authorizationSHA, planSHA, billingSHA, evidenceSHA string
	blockers                                           []string
	failurePolicy                                      string
	vendorFailureVerified                              bool
}

const fullAttemptFailurePolicy = "reserve_full_attempt_stop_on_failure"

func (p AuthorizedPilot) JEVFailurePolicy() string        { return p.failurePolicy }
func (p AuthorizedPilot) JEVFailureChargesVerified() bool { return p.vendorFailureVerified }

func (p AuthorizedPilot) Ready() bool        { return p.valid && len(p.blockers) == 0 }
func (p AuthorizedPilot) Blockers() []string { return append([]string(nil), p.blockers...) }

// ModelSettings returns the validated public binding; it never includes a key.
func (p AuthorizedPilot) ModelSettings() PairModelSettings { return p.model }

// ValidateFrozenInputs performs only local immutable archive and preparation
// checks. It needs no credentials, database, observer or provider transport.
func (p AuthorizedPilot) ValidateFrozenInputs(definitions, profiles, revision string) error {
	if !p.valid || hasString(p.blockers, "runtime_contract_mismatch") || (p.IsRetest() && revision != p.retestRevision) {
		return fmt.Errorf("validated authorization required")
	}
	source, err := LoadConversations(definitions)
	if err != nil {
		return err
	}
	fixtures, err := LoadPairedProfiles(profiles)
	if err != nil {
		return err
	}
	_, err = PrepareCampaignProposal(source, fixtures, p.model, revision, AuthorizedPilotJEVModel, p.plan)
	return err
}

// CheckAuthorizedPilot is pure: it reads neither credentials nor the filesystem.
// Evidence is trusted operator input, not cryptographic proof of supplier rates.
func CheckAuthorizedPilot(authRaw, planRaw, billingRaw, evidenceRaw []byte) (AuthorizedPilot, error) {
	var p AuthorizedPilot
	var a pilotAuthorization
	if len(authRaw) > 1<<20 || len(planRaw) > 1<<20 || len(billingRaw) > 1<<20 || len(evidenceRaw) > 1<<20 || json.Unmarshal(authRaw, &a) != nil || json.Unmarshal(planRaw, &p.plan) != nil {
		return p, fmt.Errorf("bounded valid authorization and plan JSON required")
	}
	if p.plan.CampaignID != "" || p.plan.CustomerRevision != "" || p.plan.RuntimeContractSHA != "" {
		return AuthorizedPilot{}, fmt.Errorf("retest plan cannot authorize original pilot")
	}
	limits := CampaignLimits{MaxBatches: 6, MaxModelCalls: 24, MaxModelMicroCNY: 1000000, MaxJEVCalls: 6, MaxJEVMicroUSD: 20000, JEVMicroUSDPerAttempt: 2688}
	switch {
	case a.Schema == "small-pilot-authorization.v1" && p.plan.Schema == "campaign-preflight-plan.v1" && p.plan.Target == nil && (a.Base == "" || a.Base == AuthorizedPilotModelBase):
		p.model = PairModelSettings{BaseURL: AuthorizedPilotModelBase, Model: AuthorizedPilotModel, Protocol: "anthropic"}
	case a.Schema == "small-pilot-authorization.v2" && p.plan.Schema == "campaign-preflight-plan.v2" && p.plan.Target != nil && p.plan.Target.validPi() && a.Base == p.plan.Target.BaseURL:
		p.model = p.plan.Target.modelSettings()
	default:
		return AuthorizedPilot{}, fmt.Errorf("authorization model target or version differs")
	}
	if !a.Approved || a.Expand || a.Retry || a.Model != p.model.Model || a.Protocol != p.model.Protocol || a.JEVModel != AuthorizedPilotJEVModel || a.Limits != limits || p.plan.Limits != limits || a.PlanSHA != conversationHash(planRaw) {
		return AuthorizedPilot{}, fmt.Errorf("authorization scope/plan differs from approved pilot")
	}
	keys := []string{"balance-001/answer_complete", "balance-020/explicit_scope_followup", "recharge-002/confirm", "recharge-002/cancel", "recharge-001/supplement_then_confirm", "refund-001/balance_cap"}
	calls := []int{3, 7, 3, 3, 4, 4}
	reserves := []int64{101376, 236544, 101376, 101376, 135168, 135168}
	if len(a.Cases) != 6 || len(p.plan.Batches) != 6 {
		return AuthorizedPilot{}, fmt.Errorf("exact six-case authorization required")
	}
	for i, b := range p.plan.Batches {
		want := PairBudgetLimits{MaxCalls: calls[i], MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: reserves[i], InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
		c := authorizedCase{ID: fmt.Sprintf("batch-%03d", i+1), Key: keys[i], ModelCalls: calls[i], JEVCalls: 1}
		if !reflect.DeepEqual(a.Cases[i], c) || b.Key != keys[i] || b.MaxJEVCalls != 1 || b.ModelLimits != want {
			return AuthorizedPilot{}, fmt.Errorf("case/batch allowance differs from authorization")
		}
	}
	p.authorizationSHA, p.planSHA = conversationHash(authRaw), a.PlanSHA
	return p.checkBilling(billingRaw, evidenceRaw, a.Schema == "small-pilot-authorization.v2")
}

func (p AuthorizedPilot) checkBilling(billingRaw, evidenceRaw []byte, allowRisk bool) (AuthorizedPilot, error) {
	var b pilotBilling
	if len(billingRaw) > 0 {
		if json.Unmarshal(billingRaw, &b) != nil || b.PlanSHA != p.planSHA || b.Base != p.model.BaseURL || b.Model != p.model.Model || b.Protocol != p.model.Protocol || b.JEVModel != AuthorizedPilotJEVModel {
			return AuthorizedPilot{}, fmt.Errorf("billing attestation scope/model/version differs")
		}
		if p.IsRetest() && (b.Schema != p.retestScope.BillingSchema || b.CampaignID != p.CampaignID() || b.Revision != p.retestRevision || b.RuntimeContractSHA != p.retestScope.RuntimeContractSHA) {
			return AuthorizedPilot{}, fmt.Errorf("retest billing identity or revision differs")
		}
		if !p.IsRetest() && (b.Schema == "paired-retest-billing-attestation.v1" || b.Schema == "paired-retest-billing-attestation.v2" || b.Schema == "recharge-confirm-billing-attestation.v1" || b.Schema == "recharge-confirm-billing-attestation.v2" || b.CampaignID != "" || b.Revision != "" || b.RuntimeContractSHA != "") {
			return AuthorizedPilot{}, fmt.Errorf("retest billing cannot authorize original pilot")
		}
		switch b.Schema {
		case "paired-billing-attestation.v1":
			// Legacy attestations continue to require verified vendor terms.
		case "paired-billing-attestation.v2", "paired-retest-billing-attestation.v1", "paired-retest-billing-attestation.v2", "recharge-confirm-billing-attestation.v1", "recharge-confirm-billing-attestation.v2":
			// A reviewed risk reservation is distinct from a vendor guarantee.
			// This policy is only valid for the pinned Pi pilot and its unchanged
			// maximum-context reservation; every JEV failure must halt fallback.
			if !allowRisk || b.Failure || b.FailurePolicy != fullAttemptFailurePolicy || b.JEVMaxInput != 64000 || b.JEVInputRate != 42000 || b.JEVFullAttempt != 2688 || b.JEVFullAttempt != p.plan.Limits.JEVMicroUSDPerAttempt {
				return AuthorizedPilot{}, fmt.Errorf("reviewed full-attempt failure policy required")
			}
			p.failurePolicy = fullAttemptFailurePolicy
		default:
			return AuthorizedPilot{}, fmt.Errorf("billing attestation version differs")
		}
		p.vendorFailureVerified = b.Failure
		p.billingSHA = conversationHash(billingRaw)
	}
	if !b.Tariff {
		p.blockers = append(p.blockers, "account_tariff_unverified")
	}
	if !b.Tokens {
		p.blockers = append(p.blockers, "token_envelope_unverified")
	}
	if !b.Failure && p.failurePolicy != fullAttemptFailurePolicy {
		p.blockers = append(p.blockers, "jev_failure_charges_unverified")
	}
	if len(p.blockers) == 0 {
		if len(evidenceRaw) == 0 || !comparisonDigest(b.EvidenceSHA) || b.EvidenceSHA != conversationHash(evidenceRaw) {
			return AuthorizedPilot{}, fmt.Errorf("billing evidence file missing or changed")
		}
		p.evidenceSHA = b.EvidenceSHA
	}
	p.valid = true
	return p, nil
}
