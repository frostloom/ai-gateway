package taskbench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// LocalCampaignOptions cannot configure external providers. The caller owns
// Environment; the runner owns its synthetic providers, observer and journals.
type LocalCampaignOptions struct {
	DefinitionsDir string
	Environment    *PairedFixtureEnvironment
	Revision       string
	Plan           CampaignPreparationPlan
}

type LocalCampaignObservations struct {
	Tasks       int `json:"tasks"`
	Turns       int `json:"turns"`
	Generations int `json:"generations"`
}

type LocalCampaignBatchReceipt struct {
	PairID      string           `json:"pair_id,omitempty"`
	BlockID     string           `json:"block_id,omitempty"`
	ArmOrder    []string         `json:"arm_order,omitempty"`
	ID          string           `json:"id"`
	Key         string           `json:"key"`
	Status      string           `json:"status"`
	FailedStage string           `json:"failed_stage,omitempty"`
	Report      *SmallPairReport `json:"report,omitempty"`
}

type LocalCampaignReceipt struct {
	Schema             string                      `json:"schema"`
	EvidenceKind       string                      `json:"evidence_kind"`
	Revision           string                      `json:"execution_revision"`
	Status             string                      `json:"status"`
	FailedStage        string                      `json:"failed_stage,omitempty"`
	PlannedArms        int                         `json:"planned_arms"`
	PlannedUserTurns   int                         `json:"planned_user_turns"`
	CompletedBatches   int                         `json:"completed_batches"`
	ModelAttempts      int                         `json:"synthetic_model_attempts"`
	JEVAttempts        int                         `json:"fake_jev_attempts"`
	Observations       LocalCampaignObservations   `json:"checked_local_otlp"`
	Batches            []LocalCampaignBatchReceipt `json:"batches"`
	AllowPaid          bool                        `json:"allow_paid"`
	ReadyForPaidRun    bool                        `json:"ready_for_paid_run"`
	ExternalModelCalls int                         `json:"external_model_calls"`
	ExternalJEVCalls   int                         `json:"external_jev_calls"`
}

// RunLocalCampaign exercises real handlers using only closed local substitutes.
// Every invocation needs a new directory, including after partial failure.
func RunLocalCampaign(ctx context.Context, dir string, o LocalCampaignOptions) (LocalCampaignReceipt, error) {
	return runLocalCampaign(ctx, dir, o, newLocalCampaignDependencies)
}

func runLocalCampaign(ctx context.Context, dir string, o LocalCampaignOptions, dependencies func(context.Context) (*localCampaignDependencies, error)) (r LocalCampaignReceipt, runErr error) {
	if strings.TrimSpace(o.Revision) == "" || dir == "" || o.Environment == nil || o.Environment.closed {
		return r, fmt.Errorf("local campaign requires revision, new directory and owned fixtures")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	source, err := LoadConversations(o.DefinitionsDir)
	if err != nil {
		return r, err
	}
	model := PairModelSettings{BaseURL: "http://127.0.0.1/local-rehearsal", APIKey: "local-synthetic-placeholder", Model: "local-rehearsal", Protocol: "anthropic"}
	proposal, err := PrepareCampaignProposal(source, o.Environment.profiles, model, o.Revision, "", o.Plan)
	if err != nil {
		return r, err
	}
	for _, batch := range proposal.Batches {
		if !localCampaignCaseSupported(batch.Key) {
			return r, fmt.Errorf("local script unavailable for case %s", batch.Key)
		}
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return r, fmt.Errorf("new local campaign directory required: %w", err)
	}
	r = LocalCampaignReceipt{Schema: "local-campaign-rehearsal.v1", EvidenceKind: "local_stub", Revision: o.Revision, Status: "stopped", PlannedArms: proposal.PlannedArms, PlannedUserTurns: proposal.PlannedUserTurns}
	for _, batch := range proposal.Batches {
		r.Batches = append(r.Batches, LocalCampaignBatchReceipt{ID: batch.ID, Key: batch.Key, Status: "not_started"})
	}
	stage := "dependencies"
	// Registered first so every owned resource closes before the final receipt.
	defer func() {
		if runErr != nil {
			r.Status = "stopped"
			r.FailedStage = stage
		}
		if err := writeLocalCampaignJSON(filepath.Join(dir, "receipt.json"), r); err != nil {
			r.Status = "stopped"
			r.FailedStage = "receipt_write"
			runErr = errors.Join(runErr, err)
		}
	}()
	d, err := dependencies(ctx)
	if err != nil {
		return r, fmt.Errorf("local dependencies unavailable")
	}
	defer func() {
		if err := d.close(); err != nil {
			stage = "cleanup"
			runErr = errors.Join(runErr, fmt.Errorf("local dependency cleanup failed"))
		}
	}()
	stage = "campaign"
	raw, _ := json.Marshal(proposal)
	campaign, err := OpenCampaignBudget(filepath.Join(dir, "campaign"), conversationHash(raw), o.Plan.Limits)
	if err != nil {
		return r, err
	}
	defer func() {
		if err := campaign.Close(); err != nil {
			stage = "cleanup"
			runErr = errors.Join(runErr, err)
		}
	}()
	for i, batch := range proposal.Batches {
		row := &r.Batches[i]
		stage = "context"
		if err = ctx.Err(); err != nil {
			return r, err
		}
		cfg := batch.Configuration
		cfg.Model.EvidenceKind, cfg.Selective.EvidenceKind = "local_stub", "local_stub"
		stage = "reservation"
		lease, err := campaign.ReserveBatch(batch.ID, PairRunContract(cfg), batch.ModelLimits, batch.MaxJEVCalls)
		if err != nil {
			row.Status = "failed"
			row.FailedStage = stage
			return r, err
		}
		row.Status = "failed"
		stage = "execution"
		report, state, jevAttempts, err := runLocalCampaignBatch(ctx, lease, o, cfg, model, d)
		r.ModelAttempts += state.Calls
		r.JEVAttempts += jevAttempts
		if report.Schema != "" {
			row.Report = &report
		}
		if err == nil {
			stage = "checkpoint"
			var counts LocalCampaignObservations
			counts, err = checkLocalCampaignBatch(report, state, d.capture)
			if err == nil {
				r.Observations.Tasks += counts.Tasks
				r.Observations.Turns += counts.Turns
				r.Observations.Generations += counts.Generations
			}
		}
		if row.Report != nil {
			if e := writeLocalCampaignJSON(filepath.Join(lease.Dir, "report.json"), report); e != nil {
				stage = "report_write"
				err = errors.Join(err, e)
			}
		}
		if err != nil {
			row.FailedStage = stage
			return r, fmt.Errorf("local batch %s failed at %s: %w", batch.ID, stage, err)
		}
		row.Status = "checked_local"
		r.CompletedBatches++
	}
	r.Status = "completed_local"
	return r, nil
}

func runLocalCampaignBatch(ctx context.Context, lease *CampaignBatchLease, o LocalCampaignOptions, cfg PairRunConfig, model PairModelSettings, d *localCampaignDependencies) (report SmallPairReport, state PairBudgetState, jevAttempts int, runErr error) {
	budget, err := OpenPairBudget(filepath.Join(lease.Dir, "budget.jsonl"), PairRunContract(cfg), lease.limits, d.model(cfg.Suite.Cases[0]))
	if err != nil {
		return report, state, 0, errors.Join(err, lease.Finish(true))
	}
	defer func() { runErr = errors.Join(runErr, budget.Close()) }()
	runErr = RunCampaignBatch(ctx, lease, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: lease.jevCalls, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: d.observer, JEVFactory: func() jev.Evaluator { return d.judge(cfg.Suite.Cases[0]) }})
	state = budget.Snapshot()
	path := filepath.Join(lease.Dir, "run.jsonl")
	report, err = ReportPreparedPair(path)
	runErr = errors.Join(runErr, err)
	f, err := os.Open(path + ".jev.jsonl")
	if err != nil {
		return report, state, 0, errors.Join(runErr, err)
	}
	defer func() { runErr = errors.Join(runErr, f.Close()) }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		var e pairJEVEvent
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return report, state, jevAttempts, errors.Join(runErr, fmt.Errorf("invalid local JEV evidence"))
		}
		if e.Kind == "reserve" {
			jevAttempts++
		}
	}
	runErr = errors.Join(runErr, s.Err())
	return report, state, jevAttempts, runErr
}

func checkLocalCampaignBatch(report SmallPairReport, state PairBudgetState, capture *localCampaignCapture) (LocalCampaignObservations, error) {
	if report.EvidenceKind != "local_stub" || report.FinishedArms != 2 || report.UnrunArms != 0 || state.Stopped != "" || state.CostMicroCNY == nil {
		return LocalCampaignObservations{}, fmt.Errorf("local report/usage incomplete")
	}
	for _, row := range report.Rows {
		if !row.StructuralPass || row.FactErrors != 0 || row.HTTPFailures != 0 || row.ModelAttempts == nil || row.PricedModelMicroCNY == nil || row.Status != "needs_review" {
			return LocalCampaignObservations{}, fmt.Errorf("local row incomplete")
		}
	}
	return capture.check(report)
}

func writeLocalCampaignJSON(path string, value any) (err error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func localCampaignCaseSupported(key string) bool {
	switch key {
	case "balance-001/answer_complete", "balance-020/explicit_scope_followup", "recharge-002/confirm", "recharge-002/cancel", "recharge-001/supplement_then_confirm", "refund-001/balance_cap":
		return true
	}
	return false
}

// Cleanup uses its own deadline even when the run context has been canceled.
func localCampaignCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
