package taskbench

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

type FullLocalCampaignOptions struct {
	DefinitionsDir string
	Environment    *PairedFixtureEnvironment
	Revision       string
	Keys           []string
}

type FullLocalWindowPlan struct {
	Schema           string                  `json:"schema"`
	SelectionScope   string                  `json:"selection_scope"`
	SelectionSHA256  string                  `json:"selection_sha256"`
	DefinitionSHA256 string                  `json:"definition_sha256"`
	ManifestSHA256   string                  `json:"manifest_sha256"`
	ProfileSHA256    string                  `json:"profile_sha256"`
	Revision         string                  `json:"execution_revision"`
	Limits           CampaignLimits          `json:"synthetic_campaign_limits"`
	Batches          []CampaignProposalBatch `json:"batches"`
	PlannedUserTurns int                     `json:"planned_user_turns"`
	AllowPaid        bool                    `json:"allow_paid"`
}

type FullLocalBatchReceipt struct {
	LocalCampaignBatchReceipt
	Contract      string `json:"pair_run_contract"`
	JournalSHA256 string `json:"journal_sha256,omitempty"`
	ReportSHA256  string `json:"report_sha256,omitempty"`
}

type FullLocalCampaignReceipt struct {
	LocalOTLPSHA256        string                    `json:"local_otlp_sha256,omitempty"`
	Schema                 string                    `json:"schema"`
	EvidenceKind           string                    `json:"evidence_kind"`
	Revision               string                    `json:"execution_revision"`
	SelectionScope         string                    `json:"selection_scope"`
	SelectionSHA256        string                    `json:"selection_sha256"`
	DefinitionSHA256       string                    `json:"definition_sha256"`
	ManifestSHA256         string                    `json:"manifest_sha256"`
	ProfileSHA256          string                    `json:"profile_sha256"`
	Status                 string                    `json:"status"`
	FailedStage            string                    `json:"failed_stage,omitempty"`
	PlannedCases           int                       `json:"planned_cases"`
	PlannedArms            int                       `json:"planned_arms"`
	PlannedUserTurns       int                       `json:"planned_user_turns"`
	CompletedBatches       int                       `json:"completed_batches"`
	ModelAttempts          int                       `json:"synthetic_model_attempts"`
	JEVAttempts            int                       `json:"fake_jev_attempts"`
	Observations           LocalCampaignObservations `json:"checked_local_otlp"`
	Batches                []FullLocalBatchReceipt   `json:"batches"`
	AllowPaid              bool                      `json:"allow_paid"`
	ReadyForPaidRun        bool                      `json:"ready_for_paid_run"`
	ActualFull1220Complete bool                      `json:"actual_full1220_complete"`
	ExternalModelCalls     int                       `json:"external_model_calls"`
	ExternalJEVCalls       int                       `json:"external_jev_calls"`
}

// PrepareFullLocalCustomerWindow only declares bounded synthetic budgets.
func PrepareFullLocalCustomerWindow(source ConversationSuite, profiles PairedProfiles, keys []string, revision string) (FullLocalWindowPlan, error) {
	var p FullLocalWindowPlan
	if len(keys) < 1 || len(keys) > 100 || strings.TrimSpace(revision) == "" {
		return p, fmt.Errorf("explicit 1..100 keys and revision required")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			return p, fmt.Errorf("duplicate window key")
		}
		seen[key] = true
	}
	p = FullLocalWindowPlan{Schema: "full-primary-local-window-plan.v1", Revision: revision, ProfileSHA256: profiles.SHA256, DefinitionSHA256: source.DefinitionSHA256, ManifestSHA256: source.ManifestSHA256}
	model := PairModelSettings{BaseURL: "http://127.0.0.1", APIKey: "local-synthetic-placeholder", Model: "local-full-customer", Protocol: "anthropic"}
	for i, key := range keys {
		cfg, err := PrepareFullCustomerAgentPairConfig(source, []string{key}, profiles, model, "local_stub", revision, "")
		if err != nil {
			return FullLocalWindowPlan{}, err
		}
		cfg.ArmOrder = []string{"model", "jev_selective"}
		if i%2 == 1 {
			cfg.ArmOrder = []string{"jev_selective", "model"}
		}
		p.SelectionScope, p.SelectionSHA256 = cfg.SelectionScope, cfg.SelectionSHA256
		turns := len(cfg.Suite.Cases[0].Scenario.Turns)
		limits := PairBudgetLimits{MaxCalls: 2 * 5 * turns, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, InputMicroCNYPerMillion: 1000000, OutputMicroCNYPerMillion: 2000000}
		limits.MaxMicroCNY = int64(limits.MaxCalls) * limits.cost(limits.InputTokenCeiling, int64(limits.MaxOutputTokens))
		estimate, err := EstimateDeclaredPairBudget(limits)
		if err != nil || !estimate.FitsBudget {
			return FullLocalWindowPlan{}, fmt.Errorf("invalid synthetic child budget")
		}
		batch := CampaignProposalBatch{CampaignPreparationBatch: CampaignPreparationBatch{Key: key, ModelLimits: limits, MaxJEVCalls: turns, ArmOrder: cfg.ArmOrder}, ID: fmt.Sprintf("batch-%03d", i+1), Configuration: cfg, Contract: PairRunContract(cfg), ModelEstimate: estimate, ReservedJEVMicroUSD: int64(turns) * 2688}
		p.Batches = append(p.Batches, batch)
		p.PlannedUserTurns += 2 * turns
		p.Limits.MaxBatches++
		p.Limits.MaxModelCalls += limits.MaxCalls
		p.Limits.MaxModelMicroCNY += estimate.AllAttemptsMicroCNY
		p.Limits.MaxJEVCalls += turns
		p.Limits.MaxJEVMicroUSD += batch.ReservedJEVMicroUSD
	}
	p.Limits.JEVMicroUSDPerAttempt = 2688
	return p, p.Limits.validate()
}

func loadFullLocalReadParameters(dir, definitionSHA string) (map[string]map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "definitions.json"))
	if err != nil || conversationHash(raw) != definitionSHA {
		return nil, fmt.Errorf("frozen parameter source differs")
	}
	var rows []struct {
		ID         string         `json:"source_case_id"`
		Parameters map[string]any `json:"parameters_from_original"`
	}
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 1220 {
		return nil, fmt.Errorf("invalid frozen parameter rows")
	}
	out := map[string]map[string]any{}
	for _, r := range rows {
		if r.ID == "" {
			return nil, fmt.Errorf("parameter source missing identity")
		}
		if _, ok := out[r.ID]; ok {
			return nil, fmt.Errorf("duplicate parameter identity")
		}
		out[r.ID] = r.Parameters
	}
	return out, nil
}

func newFullLocalDependencies(ctx context.Context, readArgs map[string]map[string]any, planNames map[string]string) (*localCampaignDependencies, error) {
	d, err := newLocalCampaignDependencies(ctx)
	if err != nil {
		return nil, err
	}
	d.model = func(c ConversationCase) http.RoundTripper {
		return &fullLocalModel{c: c, readArgs: readArgs[c.SourceID], planNames: planNames}
	}
	d.judge = func(ConversationCase) jev.Evaluator {
		s := jev.FakeScript{Answers: map[string]jev.Answer{"intent": jev.AnswerChoice("get_balance", .5), "legit": jev.AnswerNoul(.99), "risk": jev.AnswerScore(.1, .99, []string{"normal", "caution", "high"}), "route_shape": jev.AnswerChoice("complex", .99)}}
		return jev.NewFake(s, s, s)
	}
	return d, nil
}

func RunFullLocalCustomerWindow(ctx context.Context, dir string, o FullLocalCampaignOptions) (FullLocalCampaignReceipt, error) {
	return runFullLocalCustomerWindow(ctx, dir, o, nil)
}

func runFullLocalCustomerWindow(ctx context.Context, dir string, o FullLocalCampaignOptions, dependencies func(context.Context) (*localCampaignDependencies, error)) (r FullLocalCampaignReceipt, runErr error) {
	if o.Environment == nil || o.Environment.closed || dir == "" {
		return r, fmt.Errorf("owned fixtures and new directory required")
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return r, err
	}
	source, err := LoadConversations(o.DefinitionsDir)
	if err != nil {
		return r, err
	}
	plan, err := PrepareFullLocalCustomerWindow(source, o.Environment.profiles, o.Keys, o.Revision)
	if err != nil {
		return r, err
	}
	parameters, err := loadFullLocalReadParameters(o.DefinitionsDir, source.DefinitionSHA256)
	if err != nil {
		return r, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return r, fmt.Errorf("new window directory required")
	}
	r = FullLocalCampaignReceipt{Schema: "full-primary-local-window-receipt.v1", EvidenceKind: "local_stub", Revision: o.Revision, SelectionScope: plan.SelectionScope, SelectionSHA256: plan.SelectionSHA256, DefinitionSHA256: plan.DefinitionSHA256, ManifestSHA256: plan.ManifestSHA256, ProfileSHA256: plan.ProfileSHA256, Status: "stopped", PlannedCases: len(plan.Batches), PlannedArms: 2 * len(plan.Batches), PlannedUserTurns: plan.PlannedUserTurns}
	for _, b := range plan.Batches {
		r.Batches = append(r.Batches, FullLocalBatchReceipt{LocalCampaignBatchReceipt: LocalCampaignBatchReceipt{ID: b.ID, Key: b.Key, ArmOrder: b.Configuration.ArmOrder, Status: "not_started"}, Contract: b.Contract})
	}
	stage := "plan_write"
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
	if err := writeLocalCampaignJSON(filepath.Join(dir, "plan.json"), plan); err != nil {
		return r, err
	}
	stage = "dependencies"
	if dependencies == nil {
		names := map[string]string{}
		for ref, p := range o.Environment.profiles.archive.Catalogue.Plans {
			names[ref] = p.Name
		}
		dependencies = func(ctx context.Context) (*localCampaignDependencies, error) {
			return newFullLocalDependencies(ctx, parameters, names)
		}
	}
	d, err := dependencies(ctx)
	if err != nil {
		return r, fmt.Errorf("closed local dependencies unavailable")
	}
	if d == nil || d.model == nil || d.judge == nil || d.observer == nil || d.capture == nil || d.close == nil {
		if d != nil && d.close != nil {
			_ = d.close()
		}
		return r, fmt.Errorf("incomplete local dependencies")
	}
	defer func() {
		if err := d.close(); err != nil {
			stage = "cleanup"
			runErr = errors.Join(runErr, err)
		}
		if err := writeFullLocalCapture(dir, d.capture, &r); err != nil {
			stage = "capture_write"
			runErr = errors.Join(runErr, err)
		}
	}()
	stage = "campaign"
	raw, _ := json.Marshal(plan)
	campaign, err := OpenCampaignBudget(filepath.Join(dir, "campaign"), conversationHash(raw), plan.Limits)
	if err != nil {
		return r, err
	}
	defer func() {
		if err := campaign.Close(); err != nil {
			stage = "cleanup"
			runErr = errors.Join(runErr, err)
		}
	}()
	for i, b := range plan.Batches {
		stage = "context"
		if err := ctx.Err(); err != nil {
			return r, err
		}
		row := &r.Batches[i]
		stage = "reservation"
		lease, err := campaign.ReserveBatch(b.ID, b.Contract, b.ModelLimits, b.MaxJEVCalls)
		if err != nil {
			row.Status = "failed"
			row.FailedStage = stage
			return r, err
		}
		row.Status = "failed"
		stage = "execution"
		report, state, attempts, err := runFullLocalBatch(ctx, lease, o, b.Configuration, d)
		r.ModelAttempts += state.Calls
		r.JEVAttempts += attempts
		if report.Schema != "" {
			row.Report = &report
			row.JournalSHA256 = report.JournalSHA256
		}
		if err == nil {
			stage = "checkpoint"
			counts, checkErr := checkLocalCampaignBatch(report, state, d.capture)
			err = checkErr
			if err == nil {
				err = checkFullLocalCaptureContract(report, d.capture, b.Contract)
			}
			if err == nil {
				r.Observations.Tasks += counts.Tasks
				r.Observations.Turns += counts.Turns
				r.Observations.Generations += counts.Generations
			}
		}
		if row.Report != nil {
			path := filepath.Join(lease.Dir, "report.json")
			if e := writeLocalCampaignJSON(path, report); e != nil {
				stage = "report_write"
				err = errors.Join(err, e)
			} else {
				data, e := os.ReadFile(path)
				if e != nil {
					err = errors.Join(err, e)
				} else {
					row.ReportSHA256 = conversationHash(data)
				}
			}
		}
		if err != nil {
			row.FailedStage = stage
			return r, fmt.Errorf("local full-primary batch %s stopped at %s: %w", b.ID, stage, err)
		}
		row.Status = "checked_local"
		r.CompletedBatches++
	}
	r.Status = "completed_local"
	return r, nil
}

func runFullLocalBatch(ctx context.Context, lease *CampaignBatchLease, o FullLocalCampaignOptions, cfg PairRunConfig, d *localCampaignDependencies) (report SmallPairReport, state PairBudgetState, attempts int, runErr error) {
	budget, err := OpenPairBudget(filepath.Join(lease.Dir, "budget.jsonl"), PairRunContract(cfg), lease.limits, d.model(cfg.Suite.Cases[0]))
	if err != nil {
		return report, state, 0, errors.Join(err, lease.Finish(true))
	}
	defer func() { runErr = errors.Join(runErr, budget.Close()) }()
	model := PairModelSettings{BaseURL: "http://127.0.0.1", APIKey: "local-synthetic-placeholder", Model: "local-full-customer", Protocol: "anthropic"}
	runErr = RunFullCustomerCampaignBatch(ctx, lease, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: lease.jevCalls, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: d.observer, StopOnStructuralFailure: true, StopOnObserverFailure: true, StopOnFactFailure: true, StopOnJEVFailure: true, JEVFactory: func() jev.Evaluator { return d.judge(cfg.Suite.Cases[0]) }})
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
	s.Buffer(make([]byte, 4096), 1<<20)
	for s.Scan() {
		var e pairJEVEvent
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return report, state, attempts, errors.Join(runErr, fmt.Errorf("invalid local JEV journal"))
		}
		if e.Kind == "reserve" {
			attempts++
		}
	}
	return report, state, attempts, errors.Join(runErr, s.Err())
}

func checkFullLocalCaptureContract(report SmallPairReport, capture *localCampaignCapture, contract string) error {
	capture.Lock()
	defer capture.Unlock()
	for _, row := range report.Rows {
		for _, span := range capture.spans {
			if hex.EncodeToString(span.TraceId) != row.LangfuseTraceID {
				continue
			}
			found := false
			for _, attr := range span.Attributes {
				if attr.Key == "langfuse.observation.metadata.run_id" {
					found = attr.Value.GetStringValue() == contract
				}
			}
			if !found {
				return fmt.Errorf("local full selection contract lost")
			}
		}
	}
	return nil
}

// Only identity/association metadata is persisted here. Prompts and tool bodies
// stay in the private execution journal; they are not copied to this index.
type fullLocalSpanEvidence struct {
	TraceID  string            `json:"trace_id"`
	SpanID   string            `json:"span_id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
}

func writeFullLocalCapture(dir string, c *localCampaignCapture, r *FullLocalCampaignReceipt) error {
	evidence := struct {
		Spans []fullLocalSpanEvidence `json:"spans"`
	}{Spans: []fullLocalSpanEvidence{}}
	c.Lock()
	for _, span := range c.spans {
		row := fullLocalSpanEvidence{TraceID: hex.EncodeToString(span.TraceId), SpanID: hex.EncodeToString(span.SpanId), Name: span.Name, Metadata: map[string]string{}}
		for _, a := range span.Attributes {
			if key, ok := strings.CutPrefix(a.Key, "langfuse.observation.metadata."); ok {
				row.Metadata[key] = a.Value.GetStringValue()
			}
		}
		evidence.Spans = append(evidence.Spans, row)
	}
	c.Unlock()
	path := filepath.Join(dir, "local-observations.json")
	if err := writeLocalCampaignJSON(path, evidence); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	r.LocalOTLPSHA256 = conversationHash(raw)
	return nil
}
