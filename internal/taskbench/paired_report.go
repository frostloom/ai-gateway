package taskbench

import (
	"context"
	"fmt"

	"github.com/frostloom/ai-gateway/internal/agent"
)

type PairClarification struct {
	Turn           int    `json:"turn"`
	Action         string `json:"recorded_action"`
	SemanticReview string `json:"semantic_review"`
}
type PairReportRow struct {
	LangfuseTraceID         string                        `json:"langfuse_trace_id,omitempty"`
	Key                     string                        `json:"key"`
	Arm                     string                        `json:"arm"`
	Status                  string                        `json:"status"`
	StructuralPass          bool                          `json:"structural_pass"`
	FactChecks              []ConversationFactCheck       `json:"fact_checks,omitempty"`
	FactErrors              int                           `json:"fact_errors"`
	ReviewRequirements      []ConversationRequirement     `json:"review_requirements,omitempty"`
	NecessaryClarifications []PairClarification           `json:"necessary_clarifications"`
	HTTPFailures            int                           `json:"http_failures"`
	Errors                  []string                      `json:"errors,omitempty"`
	ExecutionFailures       []PairExecutionFailure        `json:"execution_failures,omitempty"`
	RunnerElapsedMS         *float64                      `json:"runner_elapsed_ms"`
	Telemetry               ConversationComparisonMetrics `json:"telemetry"`
	ModelAttempts           *int                          `json:"guarded_upstream_model_attempts"`
	PricedModelMicroCNY     *int64                        `json:"priced_model_usage_micro_cny"`
	ReservedModelMicroCNY   *int64                        `json:"reserved_model_micro_cny"`
}
type SmallPairReport struct {
	Schema              string                        `json:"schema"`
	JournalSHA256       string                        `json:"journal_sha256"`
	EvidenceKind        string                        `json:"evidence_kind"`
	PlannedCases        int                           `json:"planned_cases"`
	PlannedArms         int                           `json:"planned_arms"`
	FinishedArms        int                           `json:"finished_arms"`
	UnrunArms           int                           `json:"unrun_arms"`
	Rows                []PairReportRow               `json:"rows"`
	Comparison          *ConversationComparisonReport `json:"comparison"`
	CompletionRate      *float64                      `json:"completion_rate"`
	EffectClaimsAllowed bool                          `json:"effect_claims_allowed"`
	Limitations         []string                      `json:"limitations"`
}

func ReportPreparedPair(path string) (SmallPairReport, error) {
	j, err := loadPairJournal(path)
	r := SmallPairReport{Schema: "small-paired-report.v1", Limitations: []string{
		"Independent semantic review is required; a supported factual match is not whole-answer success.",
		"Unrun/interrupted arms remain in the planned denominator; failure is never dropped or retried automatically.",
		"runner_elapsed_ms includes factory/setup, tool/state oracle and cleanup; excludes human think time and cannot be presented as provider latency.",
		"Priced model usage uses configured worst-case rates, not a supplier invoice; reservations are not actual spend. Missing usage is unknown.",
		"JEV pricing is not assumed free. Currency totals and savings for the entire system are not computed.",
		"local_stub proves implementation only, not model quality, JEV confidence calibration or real benefits.",
		"Recorder/oracle/driver and configuration are trusted inputs; journals are not cryptographic execution attestations.",
	}}
	if err != nil {
		return r, err
	}
	r.EvidenceKind = j.cfg.Model.EvidenceKind
	r.JournalSHA256 = j.sha
	r.PlannedCases = len(j.cfg.Suite.Cases)
	r.PlannedArms = 2 * r.PlannedCases
	model := ConversationComparisonRun{Config: j.cfg.Model}
	selective := ConversationComparisonRun{Config: j.cfg.Selective}
	executionIncomplete := false
	for _, c := range j.cfg.Suite.Cases {
		for _, arm := range []string{"model", "jev_selective"} {
			row := PairReportRow{Key: c.Key(), Arm: arm, Status: "not_started", NecessaryClarifications: []PairClarification{}}
			for i, rule := range c.Scenario.Turns {
				if rule.NecessaryClarification {
					row.NecessaryClarifications = append(row.NecessaryClarifications, PairClarification{i + 1, "not_observed", "not_evaluated"})
				}
			}
			e, ok := j.finished[pairEventKey(pairRunEvent{Key: c.Key(), Arm: arm})]
			if !ok {
				if j.pending != nil && j.pending.Key == c.Key() && j.pending.Arm == arm {
					row.Status = "interrupted_unknown"
				}
				r.UnrunArms++
				r.Rows = append(r.Rows, row)
				continue
			}
			r.FinishedArms++
			row.LangfuseTraceID = e.LangfuseTraceID
			result := *e.Result
			for _, obs := range result.Observations {
				for _, review := range obs.Reviews {
					if r.EvidenceKind == "recorded_run" && review.Method == "local_stub" {
						return r, fmt.Errorf("recorded run cannot contain synthetic local_stub reviews")
					}
				}
			}
			driver := &pairReplayDriver{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: c.Key(), Fixture: result.Fixture, Initial: result.Initial, Final: result.Final, Steps: result.Observations}}, failures: e.ExecutionFailures}
			result = RunConversation(context.Background(), c, driver)
			if driver.next != len(e.Result.Observations) {
				return r, fmt.Errorf("unconsumed recorded observations")
			}
			row.ExecutionFailures = e.ExecutionFailures
			if len(e.ExecutionFailures) > 0 {
				executionIncomplete = true
				result.StructuralPass = false
				result.Success = false
				if !result.SetupError {
					result.Status = "failed"
				}
				for _, fault := range e.ExecutionFailures {
					if fault.Stage == "cleanup" {
						result.Errors = append(result.Errors, "recorded cleanup failure")
					}
				}
			}
			row.FactChecks = CheckConversationFacts(result)
			for _, f := range row.FactChecks {
				if f.Status == "mismatch" {
					row.FactErrors++
					result.Status = "failed"
					result.Success = false
					result.Errors = append(result.Errors, "fact mismatch: "+f.Fact)
				}
			}
			row.Status = result.Status
			row.StructuralPass = result.StructuralPass
			row.HTTPFailures = result.HTTPFailures
			row.Errors = result.Errors
			row.ReviewRequirements = result.ReviewRequirements
			ms := e.ElapsedMS
			row.RunnerElapsedMS = &ms
			for i := range row.NecessaryClarifications {
				q := &row.NecessaryClarifications[i]
				if q.Turn <= len(result.Observations) {
					q.Action = result.Observations[q.Turn-1].Action
					q.SemanticReview = "pending_independent_review"
					if result.Status == "passed" {
						q.SemanticReview = "reviewed"
					}
				}
			}
			armcfg := j.cfg.Model
			if arm == "jev_selective" {
				armcfg = j.cfg.Selective
			}
			complete := true
			var shared *agent.RuntimeMetadata
			for _, obs := range result.Observations {
				valid, e := comparisonRuntime(armcfg, obs.Turn.TelemetryRaw, &shared)
				if e != nil {
					return r, e
				}
				if !valid {
					complete = false
				}
			}
			metrics, lat, calls, tokens := comparisonTelemetry([]ConversationResult{result})
			if complete && lat {
				row.Telemetry.ResponseLatencyMS = metrics.ResponseLatencyMS
			}
			if complete && calls {
				row.Telemetry.LLMCalls = metrics.LLMCalls
				row.Telemetry.JEVCalls = metrics.JEVCalls
			}
			if complete && tokens {
				row.Telemetry.Tokens = metrics.Tokens
			}
			if e.Before != nil && e.After != nil {
				delta := e.After.Calls - e.Before.Calls
				reserved := e.After.ReservedMicroCNY - e.Before.ReservedMicroCNY
				if delta < 0 || reserved < 0 {
					return r, fmt.Errorf("budget counters moved backwards")
				}
				row.ModelAttempts = &delta
				row.ReservedModelMicroCNY = &reserved
				if e.Before.CostMicroCNY != nil && e.After.CostMicroCNY != nil {
					cost := *e.After.CostMicroCNY - *e.Before.CostMicroCNY
					if cost < 0 {
						return r, fmt.Errorf("cost moved backwards")
					}
					row.PricedModelMicroCNY = &cost
				}
			}
			if arm == "model" {
				model.Results = append(model.Results, result)
			} else {
				selective.Results = append(selective.Results, result)
			}
			r.Rows = append(r.Rows, row)
		}
	}
	if r.UnrunArms == 0 && !executionIncomplete {
		comparison, e := CompareConversations(j.cfg.Suite, model, selective)
		if e != nil {
			return r, e
		}
		r.Comparison = &comparison
	}
	// A paired result has two different rates, never one combined accuracy.
	// CompletionRate intentionally remains null; compare holds per-arm rates.
	return r, nil
}
