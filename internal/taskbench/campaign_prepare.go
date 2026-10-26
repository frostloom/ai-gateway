package taskbench

import "fmt"

// CampaignPreparationPlan supplies declarations only, never paid permission.
type CampaignPreparationPlan struct {
	CampaignID         string                     `json:"campaign_id,omitempty"`
	CustomerRevision   string                     `json:"customer_source_revision,omitempty"`
	RuntimeContractSHA string                     `json:"runtime_contract_sha256,omitempty"`
	Schema             string                     `json:"schema"`
	Target             *PilotTarget               `json:"target,omitempty"`
	Limits             CampaignLimits             `json:"campaign_limits"`
	Batches            []CampaignPreparationBatch `json:"batches"`
}

// PilotTarget is public channel identity, never credentials or spending permission.
// v2 is the Pi successor; v1 archives retain their original model binding.
type PilotTarget struct {
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	JEVModel string `json:"jev_model"`
}

func (t PilotTarget) validPi() bool {
	return t == (PilotTarget{"https://api.deepseek.com/v1", "deepseek-flash", "openai", "jev-1.13.0"})
}

func (t PilotTarget) modelSettings() PairModelSettings {
	return PairModelSettings{BaseURL: t.BaseURL, Model: t.Model, Protocol: t.Protocol}
}

func (p CampaignPreparationPlan) validateTarget(model PairModelSettings, jevModel string) error {
	if p.CampaignID != "" || p.CustomerRevision != "" || p.RuntimeContractSHA != "" {
		scope, ok := checkedCashRetestScope("cash-read-retest-authorization.v2", p.CampaignID)
		if !ok {
			scope, ok = checkedRechargeScope("recharge-confirm-authorization.v1", p.CampaignID)
		}
		if !ok {
			scope, ok = checkedRechargeScope("recharge-confirm-authorization.v2", p.CampaignID)
		}
		if !ok {
			scope, ok = checkedRechargeCancelScope("recharge-cancel-authorization.v1", p.CampaignID)
		}
		if !ok {
			scope, ok = checkedRechargeSupplementScope("recharge-supplement-authorization.v1", p.CampaignID)
		}
		if !ok {
			scope, ok = checkedRefundCapScope("refund-cap-authorization.v1", p.CampaignID)
		}
		if !ok && p.CampaignID == V5RepeatedID {
			scope = v5RepeatedScope()
			ok = true
		}
		if !ok && p.CampaignID == V5ReceiverRepeatedID {
			scope = v5ReceiverRepeatedScope()
			ok = true
		}
		if !ok && p.CampaignID == V5RepeatedDiagnosticID {
			scope = v5RepeatedDiagnosticScope()
			ok = true
		}
		if !ok && p.CampaignID == V5SixCohortID {
			scope = v5CohortScope()
			ok = true
		}
		if !ok || p.CustomerRevision != scope.CustomerRevision || p.RuntimeContractSHA != scope.RuntimeContractSHA || p.Schema != "campaign-preflight-plan.v2" {
			return fmt.Errorf("recovery retest plan markers differ")
		}
	}

	switch p.Schema {
	case "campaign-preflight-plan.v1":
		if p.Target == nil {
			return nil
		}
	case "campaign-preflight-plan.v2":
		if p.Target != nil && p.Target.validPi() && p.Target.BaseURL == model.BaseURL && p.Target.Model == model.Model && p.Target.Protocol == model.Protocol && p.Target.JEVModel == jevModel {
			return nil
		}
	}
	return fmt.Errorf("campaign schema or model target differs")
}

type CampaignPreparationBatch struct {
	PairID      string           `json:"pair_id,omitempty"`
	BlockID     string           `json:"block_id,omitempty"`
	ArmOrder    []string         `json:"arm_order,omitempty"`
	Key         string           `json:"key"`
	ModelLimits PairBudgetLimits `json:"model_limits"`
	MaxJEVCalls int              `json:"max_jev_calls"`
}

type CampaignProposalBatch struct {
	CampaignPreparationBatch
	ID                  string                     `json:"id"`
	Configuration       PairRunConfig              `json:"configuration"`
	Contract            string                     `json:"pair_run_contract"`
	ModelEstimate       DeclaredPairBudgetEstimate `json:"model_estimate"`
	ReservedJEVMicroUSD int64                      `json:"reserved_jev_micro_usd"`
}

type CampaignProposal struct {
	CampaignID                string                  `json:"campaign_id,omitempty"`
	CustomerRevision          string                  `json:"customer_source_revision,omitempty"`
	RuntimeContractSHA        string                  `json:"runtime_contract_sha256,omitempty"`
	Schema                    string                  `json:"schema"`
	Target                    *PilotTarget            `json:"target,omitempty"`
	ReviewedRevision          string                  `json:"reviewed_revision"`
	Limits                    CampaignLimits          `json:"proposed_campaign_limits"`
	Batches                   []CampaignProposalBatch `json:"batches"`
	PlannedCases              int                     `json:"planned_cases"`
	PlannedArms               int                     `json:"planned_arms"`
	PlannedUserTurns          int                     `json:"planned_user_turns"`
	ModelReservedMicroCNY     int64                   `json:"declared_reserved_model_micro_cny"`
	JEVReservedMicroUSD       int64                   `json:"declared_reserved_jev_micro_usd"`
	AllowPaid                 bool                    `json:"allow_paid"`
	ReadyForPaidRun           bool                    `json:"ready_for_paid_run"`
	ProviderCalls             int                     `json:"provider_calls"`
	AuthorizedSpending        *CampaignLimits         `json:"authorized_spending"`
	AccountTariffVerified     bool                    `json:"account_tariff_verified"`
	TokenEnvelopeVerified     bool                    `json:"token_envelope_verified"`
	JEVFailureChargesVerified bool                    `json:"jev_failure_charges_verified"`
	AutomaticScopeExpansion   bool                    `json:"automatic_scope_expansion"`
}

// PrepareCampaignProposal binds selected frozen cases using integer arithmetic
// and disabled-provider configuration preparation. It creates no execution
// lease, journal, database connection or network request.
func PrepareCampaignProposal(source ConversationSuite, profiles PairedProfiles, model PairModelSettings, revision, jevModel string, plan CampaignPreparationPlan) (CampaignProposal, error) {
	repeated := plan.CampaignID == V5RepeatedID || plan.CampaignID == V5RepeatedDiagnosticID || plan.CampaignID == V5ReceiverRepeatedID
	if repeated {
		if err := validateRepeatedSchedule(plan, plan.CampaignID); err != nil {
			return CampaignProposal{}, err
		}
	} else {
		for _, b := range plan.Batches {
			if b.PairID != "" || b.BlockID != "" || b.ArmOrder != nil {
				return CampaignProposal{}, fmt.Errorf("repeated metadata forbidden in historical proposal")
			}
		}
	}
	if len(plan.Batches) == 0 {
		return CampaignProposal{}, fmt.Errorf("explicit campaign preparation schema/batches required")
	}
	if err := plan.validateTarget(model, jevModel); err != nil {
		return CampaignProposal{}, err
	}
	if err := plan.Limits.validate(); err != nil {
		return CampaignProposal{}, err
	}
	small, err := PrepareSmallPair(source)
	if err != nil {
		return CampaignProposal{}, err
	}
	byKey := map[string]ConversationCase{}
	for _, c := range small.Suite.Cases {
		byKey[c.Key()] = c
	}
	seen := map[string]bool{}
	proposal := CampaignProposal{CampaignID: plan.CampaignID, CustomerRevision: plan.CustomerRevision, RuntimeContractSHA: plan.RuntimeContractSHA, Schema: "small-pilot-execution-proposal.v1", ReviewedRevision: revision, Limits: plan.Limits}
	if plan.Target != nil {
		target := *plan.Target
		proposal.Target = &target
		proposal.Schema = "small-pilot-execution-proposal.v2"
	}
	// Reuse the ledger's pure reservation checks without opening a ledger or
	// issuing leases; these in-memory entries cannot be used for execution.
	check := CampaignBudget{limits: plan.Limits, reserved: map[string]campaignEvent{}}
	for i, b := range plan.Batches {
		c, ok := byKey[b.Key]
		identity := b.Key
		if repeated {
			identity = b.PairID
		}
		if !ok || seen[identity] {
			return CampaignProposal{}, fmt.Errorf("campaign case missing or repeated: %s", b.Key)
		}
		seen[identity] = true
		suite := small.Suite
		suite.Cases = []ConversationCase{c}
		cfg, err := PrepareAgentPairConfig(suite, profiles, model, "recorded_run", revision, jevModel)
		if err != nil {
			return CampaignProposal{}, err
		}
		if repeated {
			cfg.PairID = b.PairID
			cfg.BlockID = b.BlockID
			cfg.ArmOrder = append([]string{}, b.ArmOrder...)
		}
		estimate, err := EstimateDeclaredPairBudget(b.ModelLimits)
		if err != nil {
			return CampaignProposal{}, err
		}
		if !estimate.FitsBudget || b.ModelLimits.MaxOutputTokens != cfg.Model.OutputBudget || b.MaxJEVCalls < 1 || b.MaxJEVCalls > 256 {
			return CampaignProposal{}, fmt.Errorf("campaign child budget/output differs")
		}
		batch := CampaignProposalBatch{CampaignPreparationBatch: b, ID: fmt.Sprintf("batch-%03d", i+1), Configuration: cfg, Contract: PairRunContract(cfg), ModelEstimate: estimate, ReservedJEVMicroUSD: int64(b.MaxJEVCalls) * plan.Limits.JEVMicroUSDPerAttempt}
		if err = check.applyReservation(campaignEvent{ID: batch.ID, Binding: batch.Contract, ModelCalls: b.ModelLimits.MaxCalls, ModelMicroCNY: estimate.AllAttemptsMicroCNY, JEVCalls: b.MaxJEVCalls, JEVMicroUSD: batch.ReservedJEVMicroUSD}); err != nil {
			return CampaignProposal{}, err
		}
		proposal.Batches = append(proposal.Batches, batch)
		proposal.PlannedUserTurns += 2 * len(c.Scenario.Turns)
	}
	proposal.PlannedCases = len(proposal.Batches)
	proposal.PlannedArms = 2 * proposal.PlannedCases
	proposal.ModelReservedMicroCNY = check.used.ModelMicroCNY
	proposal.JEVReservedMicroUSD = check.used.JEVMicroUSD
	return proposal, nil
}
