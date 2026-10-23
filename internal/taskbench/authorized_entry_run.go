package taskbench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

type AuthorizedFirstBatchOptions struct {
	Pilot          AuthorizedPilot
	DefinitionsDir string
	Environment    *PairedFixtureEnvironment
	Revision       string
	Observer       *observability.Observer // caller owns shutdown
	Model          PairModelSettings
	Upstream       http.RoundTripper
	JEVFactory     func() jev.Evaluator
}
type AuthorizedFirstBatchReceipt struct {
	DiagnosticSourceRevision     string                      `json:"diagnostic_source_revision,omitempty"`
	DiagnosticManifestSHA        string                      `json:"diagnostic_manifest_sha256,omitempty"`
	BudgetCeilingSHA             string                      `json:"budget_ceiling_sha256,omitempty"`
	OfflineManifestSHA           string                      `json:"prior_offline_manifest_sha256,omitempty"`
	OfflineAcceptanceSHA         string                      `json:"prior_offline_acceptance_sha256,omitempty"`
	CustomerRevision             string                      `json:"customer_source_revision,omitempty"`
	RuntimeContractSHA           string                      `json:"runtime_contract_sha256,omitempty"`
	CampaignID                   string                      `json:"campaign_id,omitempty"`
	PriorJournalSHA              string                      `json:"prior_journal_sha256,omitempty"`
	PriorReviewSHA               string                      `json:"prior_review_sha256,omitempty"`
	PreviousModelCalls           int                         `json:"previous_model_calls,omitempty"`
	PreviousJEVAttempts          int                         `json:"previous_jev_attempts,omitempty"`
	CampaignReservedModelCalls   int                         `json:"campaign_reserved_model_calls"`
	CampaignReservedJEVCalls     int                         `json:"campaign_reserved_jev_calls"`
	CampaignReservedMicroCNY     int64                       `json:"campaign_reserved_micro_cny"`
	CampaignReservedMicroUSD     int64                       `json:"campaign_reserved_micro_usd"`
	PreviousReceiptSHA           string                      `json:"previous_receipt_sha256,omitempty"`
	ReviewSHA                    string                      `json:"review_sha256,omitempty"`
	JEVFailurePolicy             string                      `json:"jev_failure_policy,omitempty"`
	VendorFailureChargesVerified bool                        `json:"jev_failure_charges_verified"`
	Schema                       string                      `json:"schema"`
	Status                       string                      `json:"status"`
	FailedStage                  string                      `json:"failed_stage,omitempty"`
	AuthorizationSHA             string                      `json:"authorization_sha256"`
	PlanSHA                      string                      `json:"plan_sha256"`
	BillingSHA                   string                      `json:"billing_attestation_sha256"`
	EvidenceSHA                  string                      `json:"billing_evidence_sha256"`
	Revision                     string                      `json:"execution_revision"`
	Batches                      []LocalCampaignBatchReceipt `json:"batches"`
	ModelState                   PairBudgetState             `json:"model_budget"`
	JEVAttempts                  int                         `json:"jev_attempts"`
	Report                       *SmallPairReport            `json:"report,omitempty"`
	ObserverStats                observability.Stats         `json:"observer_stats"`
}

// RunAuthorizedFirstBatch never advances to batch-002 or resumes any directory.
// The caller supplies trusted dependencies; the CLI pins real provider settings.
func RunAuthorizedFirstBatch(ctx context.Context, dir string, o AuthorizedFirstBatchOptions) (AuthorizedFirstBatchReceipt, error) {
	if o.Pilot.IsRetest() {
		return AuthorizedFirstBatchReceipt{}, fmt.Errorf("retest requires its separate runner")
	}
	return runAuthorizedBatch(ctx, dir, o, nil)
}
func runAuthorizedBatch(ctx context.Context, dir string, o AuthorizedFirstBatchOptions, continuation *AuthorizedContinuation) (r AuthorizedFirstBatchReceipt, runErr error) {
	index := 0
	if continuation != nil {
		index = 1
	}

	if !o.Pilot.Ready() || (o.Pilot.IsRetest() && (o.Revision != o.Pilot.retestRevision || continuation != nil)) {
		return r, fmt.Errorf("verified authorized pilot required")
	}
	if o.Environment == nil || o.Environment.closed || o.Observer == nil || o.Upstream == nil || o.JEVFactory == nil || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(o.Revision) || dir == "" {
		return r, fmt.Errorf("explicit fixtures, observer, providers, revision and new path required")
	}
	bound := o.Pilot.ModelSettings()
	if o.Model.BaseURL != bound.BaseURL || o.Model.Model != bound.Model || o.Model.Protocol != bound.Protocol || o.Model.APIKey == "" {
		return r, fmt.Errorf("model configuration differs from authorization")
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
		return r, fmt.Errorf("new batch output directory required")
	}
	r = AuthorizedFirstBatchReceipt{Schema: "authorized-first-batch.v1", Status: "stopped", AuthorizationSHA: o.Pilot.authorizationSHA, PlanSHA: o.Pilot.planSHA, BillingSHA: o.Pilot.billingSHA, EvidenceSHA: o.Pilot.evidenceSHA, Revision: o.Revision, JEVFailurePolicy: o.Pilot.JEVFailurePolicy(), VendorFailureChargesVerified: o.Pilot.JEVFailureChargesVerified()}
	if o.Pilot.IsRetest() {
		r.Schema = o.Pilot.retestScope.ReceiptSchema
		r.DiagnosticSourceRevision = o.Pilot.retestScope.DiagnosticSourceRevision
		r.DiagnosticManifestSHA = o.Pilot.retestScope.DiagnosticManifestSHA
		r.BudgetCeilingSHA = o.Pilot.retestScope.BudgetCeilingSHA
		r.OfflineManifestSHA = o.Pilot.retestScope.OfflineManifestSHA
		r.OfflineAcceptanceSHA = o.Pilot.retestScope.OfflineAcceptanceSHA
		r.CampaignID = o.Pilot.CampaignID()
		r.PriorJournalSHA = o.Pilot.retestScope.PriorJournalSHA
		r.PriorReviewSHA = o.Pilot.retestScope.PriorReviewSHA
		r.CustomerRevision = o.Pilot.retestScope.CustomerRevision
		r.RuntimeContractSHA = o.Pilot.retestScope.RuntimeContractSHA
	}
	for _, b := range proposal.Batches {
		r.Batches = append(r.Batches, LocalCampaignBatchReceipt{ID: b.ID, Key: b.Key, Status: "not_started"})
	}
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
			runErr = errors.Join(runErr, fmt.Errorf("receipt persistence failed"))
		}
	}()
	// Bind permission/evidence to the parent ledger; child contracts bind the
	// exact live configuration, including AllowPaid, before reservation.
	parentDir := filepath.Join(dir, "campaign")
	bindingRevision := r.Revision
	if continuation != nil {
		r.Schema = "authorized-second-batch.v1"
		r.PreviousModelCalls = continuation.PreviousModelCalls()
		r.PreviousJEVAttempts = continuation.PreviousJEVCalls()
		r.PreviousReceiptSHA = continuation.receiptSHA
		r.ReviewSHA = continuation.reviewSHA
		r.Batches[0] = continuation.receipt.Batches[0]
		parentDir = filepath.Join(continuation.dir, "campaign")
		bindingRevision = continuation.receipt.Revision
	}
	binding, _ := json.Marshal([]string{r.AuthorizationSHA, r.PlanSHA, r.BillingSHA, r.EvidenceSHA, bindingRevision})
	campaign, err := OpenCampaignBudget(parentDir, conversationHash(binding), o.Pilot.plan.Limits)
	if err != nil {
		return r, err
	}
	defer func() {
		if err := campaign.Close(); err != nil {
			stage = "campaign_close"
			runErr = errors.Join(runErr, err)
		}
	}()
	batch := proposal.Batches[index]
	cfg := batch.Configuration
	cfg.AllowPaid = true
	stage = "reservation"
	lease, err := campaign.ReserveBatch(batch.ID, PairRunContract(cfg), batch.ModelLimits, batch.MaxJEVCalls)
	if err != nil {
		return r, err
	}
	r.CampaignReservedModelCalls = campaign.used.ModelCalls
	r.CampaignReservedJEVCalls = campaign.used.JEVCalls
	r.CampaignReservedMicroCNY = campaign.used.ModelMicroCNY
	r.CampaignReservedMicroUSD = campaign.used.JEVMicroUSD
	stage = "model_budget"
	budget, err := OpenPairBudget(filepath.Join(lease.Dir, "budget.jsonl"), PairRunContract(cfg), batch.ModelLimits, o.Upstream)
	if err != nil {
		return r, errors.Join(err, lease.Finish(true))
	}
	defer func() {
		if e := budget.Close(); e != nil {
			stage = "budget_close"
			runErr = errors.Join(runErr, e)
		}
	}()
	stage = "execution"
	err = RunCampaignBatch(ctx, lease, AgentPairOptions{AllowExecution: true, RatesVerified: true, StopOnJEVFailure: o.Pilot.JEVFailurePolicy() == fullAttemptFailurePolicy, AllowJEVCalls: true, MaxJEVCalls: batch.MaxJEVCalls, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: o.Model, Observer: o.Observer, JEVFactory: func() jev.Evaluator { return &authorizedBillingJEV{inner: o.JEVFactory()} }})
	r.ModelState = budget.Snapshot()
	path := filepath.Join(lease.Dir, "run.jsonl")
	report, reportErr := ReportPreparedPair(path)
	if reportErr == nil {
		r.Report = &report
		r.Batches[index].Report = &report
		reportErr = writeLocalCampaignJSON(filepath.Join(lease.Dir, "report.json"), report)
	}
	attempts, jevErr := authorizedJEVAttempts(path + ".jev.jsonl")
	r.JEVAttempts = attempts
	if combined := errors.Join(err, reportErr, jevErr); combined != nil {
		return r, fmt.Errorf("selected batch execution/evidence failed")
	}
	if r.ModelState.Stopped != "" || r.ModelState.CostMicroCNY == nil {
		return r, fmt.Errorf("model charge evidence incomplete")
	}
	for _, row := range report.Rows {
		if row.FactErrors > 0 || row.ModelAttempts == nil || row.PricedModelMicroCNY == nil {
			return r, fmt.Errorf("selected batch report incomplete")
		}
	}
	stage = "checkpoint"
	r.Batches[index].Status = "checkpoint_pending"
	r.Status = "checkpoint_pending"
	return r, nil
}

func authorizedJEVAttempts(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		var e pairJEVEvent
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return n, fmt.Errorf("invalid JEV journal")
		}
		if e.Kind == "reserve" {
			n++
		}
	}
	return n, s.Err()
}

type authorizedBillingJEV struct{ inner jev.Evaluator }

func (j *authorizedBillingJEV) Evaluate(ctx context.Context, state any, q map[string]jev.Question) (*jev.Result, error) {
	if j.inner == nil {
		return nil, jev.NewFailure("configuration", 0, false)
	}
	raw, err := json.Marshal(map[string]any{"model": AuthorizedPilotJEVModel, "state": state, "questions": q})
	if err != nil || len(raw) > 16384 {
		return nil, jev.NewFailure("request_bound", 0, false)
	}
	r, err := j.inner.Evaluate(ctx, state, q)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, jev.NewFailure("result_mismatch", 0, true)
	}
	var body struct {
		Usage struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(r.Raw), &body) != nil || body.Usage.Input == nil || body.Usage.Output == nil || *body.Usage.Input < 0 || *body.Usage.Input > 64000 || *body.Usage.Output < 0 {
		return nil, jev.NewFailure("usage_unknown_or_out_of_bound", 0, true)
	}
	return r, nil
}
