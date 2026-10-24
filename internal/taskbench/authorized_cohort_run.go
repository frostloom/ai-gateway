package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/jev"
	"os"
	"path/filepath"
	"regexp"
)

type CohortBatchModelState struct {
	BatchID string          `json:"batch_id"`
	State   PairBudgetState `json:"model_budget"`
}

// A cohort is one explicitly authorized scope. It never resumes an output or
// advances after a failed arm, dependency, usage, report or campaign operation.
func RunAuthorizedCohort(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (r AuthorizedFirstBatchReceipt, runErr error) {
	if !o.Pilot.Ready() || !o.Pilot.IsCohort() || o.Pilot.CampaignID() != V5SixCohortID || o.Revision != o.Pilot.ExecutionRevision() {
		return r, fmt.Errorf("ready fixed cohort and exact revision required")
	}
	return runAuthorizedFixedCohort(ctx, dir, o)
}

func runAuthorizedFixedCohort(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (r AuthorizedFirstBatchReceipt, runErr error) {
	if o.Environment == nil || o.Environment.closed || o.Observer == nil || o.Upstream == nil || o.JEVFactory == nil || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(o.Revision) || dir == "" {
		return r, fmt.Errorf("explicit owned cohort dependencies required")
	}
	bound := o.Pilot.ModelSettings()
	if o.Model.BaseURL != bound.BaseURL || o.Model.Model != bound.Model || o.Model.Protocol != bound.Protocol || o.Model.APIKey == "" {
		return r, fmt.Errorf("cohort model differs from authorization")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	source, err := LoadConversations(o.DefinitionsDir)
	if err != nil {
		return r, err
	}
	proposal, err := PrepareCampaignProposal(source, o.Environment.profiles, o.Model, o.Revision, AuthorizedPilotJEVModel, o.Pilot.plan)
	if err != nil {
		return r, err
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return r, fmt.Errorf("new cohort output required; no resume")
	}
	scope := o.Pilot.retestScope
	limits := o.Pilot.plan.Limits
	cost := int64(0)
	r = AuthorizedFirstBatchReceipt{Schema: scope.ReceiptSchema, Status: "stopped", AuthorizationSHA: o.Pilot.authorizationSHA, PlanSHA: o.Pilot.planSHA, BillingSHA: o.Pilot.billingSHA, EvidenceSHA: o.Pilot.evidenceSHA, Revision: o.Revision, CampaignID: scope.ID, CustomerRevision: scope.CustomerRevision, RuntimeContractSHA: scope.RuntimeContractSHA, BudgetCeilingSHA: scope.BudgetCeilingSHA, OfflineManifestSHA: scope.OfflineManifestSHA, OfflineAcceptanceSHA: scope.OfflineAcceptanceSHA, DiagnosticSourceRevision: scope.DiagnosticSourceRevision, DiagnosticManifestSHA: scope.DiagnosticManifestSHA, JEVFailurePolicy: o.Pilot.JEVFailurePolicy(), VendorFailureChargesVerified: o.Pilot.JEVFailureChargesVerified(), ScopeReservedModelCalls: limits.MaxModelCalls, ScopeReservedJEVCalls: limits.MaxJEVCalls, ScopeReservedMicroCNY: limits.MaxModelMicroCNY, ScopeReservedMicroUSD: limits.MaxJEVMicroUSD, ModelState: PairBudgetState{CostMicroCNY: &cost}}
	for _, b := range proposal.Batches {
		r.Batches = append(r.Batches, LocalCampaignBatchReceipt{ID: b.ID, Key: b.Key, Status: "not_started", PairID: b.Configuration.PairID, BlockID: b.Configuration.BlockID, ArmOrder: b.Configuration.ArmOrder})
	}
	index := 0
	stage := "campaign"
	defer func() {
		r.ObserverStats = o.Observer.Stats()
		if runErr != nil {
			r.Status = "stopped"
			r.FailedStage = stage
			r.Batches[index].Status = "failed"
			r.Batches[index].FailedStage = stage
		}
		if e := writeLocalCampaignJSON(filepath.Join(dir, "receipt.json"), r); e != nil {
			r.Status = "stopped"
			r.FailedStage = "receipt_write"
			runErr = errors.Join(runErr, e)
		}
	}()
	binding, _ := json.Marshal([]string{r.AuthorizationSHA, r.PlanSHA, r.BillingSHA, r.EvidenceSHA, r.Revision})
	campaign, err := OpenCampaignBudget(filepath.Join(dir, "campaign"), conversationHash(binding), limits)
	if err != nil {
		return r, err
	}
	defer func() {
		if e := campaign.Close(); e != nil {
			stage = "campaign_close"
			runErr = errors.Join(runErr, e)
		}
	}()
	for i, b := range proposal.Batches {
		index = i
		stage = "reservation"
		cfg := b.Configuration
		cfg.AllowPaid = true
		lease, err := campaign.ReserveBatch(b.ID, PairRunContract(cfg), b.ModelLimits, b.MaxJEVCalls)
		r.CampaignReservedModelCalls = campaign.used.ModelCalls
		r.CampaignReservedJEVCalls = campaign.used.JEVCalls
		r.CampaignReservedMicroCNY = campaign.used.ModelMicroCNY
		r.CampaignReservedMicroUSD = campaign.used.JEVMicroUSD
		if err != nil {
			return r, err
		}
		stage = "batch_execution"
		state, report, attempts, err := runAuthorizedCohortBatch(ctx, lease, b, cfg, o)
		r.CohortBatchModelStates = append(r.CohortBatchModelStates, CohortBatchModelState{b.ID, state})
		r.Batches[i].Report = report
		r.JEVAttempts += attempts
		r.ModelState.Calls += state.Calls
		r.ModelState.ReservedMicroCNY += state.ReservedMicroCNY
		r.ModelState.InputTokens += state.InputTokens
		r.ModelState.OutputTokens += state.OutputTokens
		if r.ModelState.CostMicroCNY != nil && state.CostMicroCNY != nil {
			*r.ModelState.CostMicroCNY += *state.CostMicroCNY
		} else {
			r.ModelState.CostMicroCNY = nil
		}
		if state.Stopped != "" {
			r.ModelState.Stopped = state.Stopped
		}
		if err != nil {
			return r, err
		}
		r.Batches[i].Status = "completed_pending_review"
	}
	stage = "checkpoint"
	r.Status = "checkpoint_pending"
	return r, nil
}

func runAuthorizedCohortBatch(ctx context.Context, lease *CampaignBatchLease, b CampaignProposalBatch, cfg PairRunConfig, o AuthorizedFirstBatchOptions) (state PairBudgetState, report *SmallPairReport, attempts int, runErr error) {
	budget, err := OpenPairBudget(filepath.Join(lease.Dir, "budget.jsonl"), PairRunContract(cfg), b.ModelLimits, o.Upstream)
	if err != nil {
		return state, nil, 0, errors.Join(err, lease.Finish(true))
	}
	defer func() { runErr = errors.Join(runErr, budget.Close()) }()
	runErr = RunCampaignBatch(ctx, lease, AgentPairOptions{AllowExecution: true, RatesVerified: true, StopOnJEVFailure: true, StopOnStructuralFailure: true, StopOnObserverFailure: o.Pilot.IsRepeated(), StopOnFactFailure: o.Pilot.IsRepeated(), AllowJEVCalls: true, MaxJEVCalls: b.MaxJEVCalls, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: o.Model, Observer: o.Observer, JEVFactory: func() jev.Evaluator { return &authorizedBillingJEV{inner: o.JEVFactory()} }})
	state = budget.Snapshot()
	path := filepath.Join(lease.Dir, "run.jsonl")
	value, reportErr := ReportPreparedPair(path)
	if reportErr == nil {
		report = &value
		reportErr = writeLocalCampaignJSON(filepath.Join(lease.Dir, "report.json"), value)
	}
	var jevErr error
	attempts, jevErr = authorizedJEVAttempts(path + ".jev.jsonl")
	if state.Stopped != "" || state.CostMicroCNY == nil {
		runErr = errors.Join(runErr, fmt.Errorf("cohort model charge evidence incomplete"))
	}
	if report != nil {
		for _, row := range report.Rows {
			if row.FactErrors > 0 || row.ModelAttempts == nil || row.PricedModelMicroCNY == nil {
				runErr = errors.Join(runErr, fmt.Errorf("cohort report or pricing incomplete"))
			}
		}
	}
	return state, report, attempts, errors.Join(runErr, reportErr, jevErr)
}
