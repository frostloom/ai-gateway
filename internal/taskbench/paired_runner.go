package taskbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
)

type PairRunConfig struct {
	Schema    string                       `json:"schema"`
	Suite     ConversationSuite            `json:"suite"`
	Model     ConversationComparisonConfig `json:"model"`
	Selective ConversationComparisonConfig `json:"selective"`
	AllowPaid bool                         `json:"allow_paid"`
}
type pairRunEvent struct {
	Kind              string                 `json:"kind"`
	Config            *PairRunConfig         `json:"config,omitempty"`
	Key               string                 `json:"key,omitempty"`
	Arm               string                 `json:"arm,omitempty"`
	Result            *ConversationResult    `json:"result,omitempty"`
	Before            *PairBudgetState       `json:"budget_before,omitempty"`
	After             *PairBudgetState       `json:"budget_after,omitempty"`
	ElapsedMS         float64                `json:"runner_elapsed_ms,omitempty"`
	ExecutionFailures []PairExecutionFailure `json:"execution_failures,omitempty"`
	BudgetBinding     *pairBudgetBinding     `json:"budget_binding,omitempty"`
}

type pairBudgetBinding struct {
	Identity string           `json:"identity"`
	Limits   PairBudgetLimits `json:"limits"`
	Initial  PairBudgetState  `json:"initial"`
}
type PairDriverFactory func(context.Context, string, ConversationCase) (ConversationDriver, error)

func PairRunContract(cfg PairRunConfig) string {
	raw, _ := json.Marshal(cfg)
	return conversationHash(raw)
}
func validatePairRun(cfg PairRunConfig) error {
	if cfg.Schema != "paired-run.v1" || len(cfg.Suite.Cases) < 1 || len(cfg.Suite.Cases) > 12 {
		return fmt.Errorf("paired run requires 1..12 explicit complete tasks")
	}
	a, b := cfg.Model, cfg.Selective
	if a.RoutingMode != "model" || b.RoutingMode != "jev_selective" {
		return fmt.Errorf("model/selective arms required")
	}
	a.RoutingMode = ""
	b.RoutingMode = ""
	if !reflect.DeepEqual(a, b) {
		return fmt.Errorf("paired conditions differ beyond routing")
	}
	if !hasString([]string{"local_stub", "recorded_run"}, a.EvidenceKind) || a.Model == "" || !hasString([]string{"openai", "anthropic"}, a.ProviderProtocol) || a.ExecutionRevision == "" || a.OutputBudget < 1 || a.OutputBudget > 512 {
		return fmt.Errorf("explicit execution metadata and short output bound required")
	}
	for _, h := range []string{a.DefinitionSHA256, a.ManifestSHA256, a.PromptSHA256, a.ToolSchemaSHA256} {
		if !comparisonDigest(h) {
			return fmt.Errorf("invalid paired SHA")
		}
	}
	if a.DefinitionSHA256 != cfg.Suite.DefinitionSHA256 || a.ManifestSHA256 != cfg.Suite.ManifestSHA256 {
		return fmt.Errorf("definition binding differs")
	}
	profiles := map[string]bool{}
	keys := map[string]bool{}
	for _, c := range cfg.Suite.Cases {
		if err := validateConversation(c); err != nil {
			return err
		}
		if !c.Scenario.FullTaskMetricEligible || c.Scenario.MeasurementScope != "complete_task" || keys[c.Key()] {
			return fmt.Errorf("unique full task required")
		}
		keys[c.Key()] = true
		profiles[c.Scenario.Profile] = true
		if !comparisonDigest(a.ProfileSHA256[c.Scenario.Profile]) {
			return fmt.Errorf("missing profile SHA")
		}
	}
	if len(profiles) != len(a.ProfileSHA256) {
		return fmt.Errorf("profile scope mismatch")
	}
	return nil
}
func pairOrder(cfg PairRunConfig) []pairRunEvent {
	order := []pairRunEvent{}
	for i, c := range cfg.Suite.Cases {
		arms := []string{"model", "jev_selective"}
		if i%2 == 1 {
			arms[0], arms[1] = arms[1], arms[0]
		}
		for _, arm := range arms {
			order = append(order, pairRunEvent{Key: c.Key(), Arm: arm})
		}
	}
	return order
}
func pairEventKey(e pairRunEvent) string { return e.Key + ":" + e.Arm }

type pairJournal struct {
	cfg           PairRunConfig
	finished      map[string]pairRunEvent
	pending       *pairRunEvent
	sha           string
	budgetBinding *pairBudgetBinding
}

func loadPairJournal(path string) (pairJournal, error) {
	j := pairJournal{finished: map[string]pairRunEvent{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return j, err
	}
	j.sha = conversationHash(raw)
	s := bufio.NewScanner(bytes.NewReader(raw))
	s.Buffer(make([]byte, 4096), 16<<20)
	line := 0
	next := 0
	var order []pairRunEvent
	for s.Scan() {
		var e pairRunEvent
		if err = json.Unmarshal(s.Bytes(), &e); err != nil {
			return j, fmt.Errorf("invalid pair journal; no automatic retry")
		}
		if line == 0 {
			if e.Kind != "config" || e.Config == nil {
				return j, fmt.Errorf("missing pair config")
			}
			j.cfg = *e.Config
			j.budgetBinding = e.BudgetBinding
			if err = validatePairRun(j.cfg); err != nil {
				return j, err
			}
			order = pairOrder(j.cfg)
		} else {
			if next >= len(order) || e.Key != order[next].Key || e.Arm != order[next].Arm {
				return j, fmt.Errorf("pair journal order/coverage mismatch")
			}
			switch e.Kind {
			case "start":
				if j.pending != nil || e.Result != nil {
					return j, fmt.Errorf("duplicate unfinished start")
				}
				copy := e
				j.pending = &copy
			case "finish":
				if j.pending == nil || e.Result == nil || e.Result.Case.Key() != e.Key || e.ElapsedMS < 0 {
					return j, fmt.Errorf("finish without matching start/result")
				}
				want, _ := json.Marshal(j.cfg.Suite.Cases[next/2])
				got, _ := json.Marshal(e.Result.Case)
				if string(want) != string(got) {
					return j, fmt.Errorf("recorded case definition changed")
				}
				e.Before = j.pending.Before
				j.finished[pairEventKey(e)] = e
				j.pending = nil
				next++
			default:
				return j, fmt.Errorf("unknown pair journal event")
			}
		}
		line++
	}
	if err = s.Err(); err != nil {
		return j, err
	}
	if line == 0 {
		return j, fmt.Errorf("empty pair journal")
	}
	return j, nil
}
func writePairEvent(f *os.File, e pairRunEvent) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// This is an explicit execution interface, never a provider/credential discovery
// function. A recorded_run requires caller authorization and the SAME transport
// budget across both arms. Factory must use ConfigureEvaluationClient for every
// model client; it is a trusted adapter, not a sandbox for arbitrary Go code.
// Fixture construction must produce separate equivalent tenants per arm.
func RunPreparedPair(ctx context.Context, path string, cfg PairRunConfig, budget *PairBudgetTransport, factory PairDriverFactory) error {
	if err := validatePairRun(cfg); err != nil {
		return err
	}
	if factory == nil {
		return fmt.Errorf("explicit driver factory required")
	}
	if cfg.Model.EvidenceKind == "recorded_run" && (!cfg.AllowPaid || budget == nil) {
		return fmt.Errorf("paid authorization and durable shared model budget required")
	}
	if budget != nil && (budget.contract != PairRunContract(cfg) || budget.limits.MaxOutputTokens != cfg.Model.OutputBudget) {
		return fmt.Errorf("budget execution/output contract differs")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("exclusive pair lock required: %w", err)
	}
	defer func() { lock.Close(); os.Remove(path + ".lock") }()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		header := pairRunEvent{Kind: "config", Config: &cfg}
		if budget != nil {
			header.BudgetBinding = &pairBudgetBinding{budget.identity(), budget.limits, budget.Snapshot()}
		}
		if err = writePairEvent(f, header); err != nil {
			return err
		}
	}
	j, err := loadPairJournal(path)
	if err != nil {
		return err
	}
	if PairRunContract(j.cfg) != PairRunContract(cfg) {
		return fmt.Errorf("resume execution conditions changed")
	}
	if j.pending != nil {
		return fmt.Errorf("unfinished arm may have executed/charged: manual reconciliation required")
	}
	if j.budgetBinding != nil {
		if budget == nil || j.budgetBinding.Identity != budget.identity() || j.budgetBinding.Limits != budget.limits {
			return fmt.Errorf("resume budget identity/prices/limits changed")
		}
		last := j.budgetBinding.Initial
		for _, step := range pairOrder(cfg) {
			if e, ok := j.finished[pairEventKey(step)]; ok {
				if e.Before == nil || e.After == nil || !reflect.DeepEqual(*e.Before, last) {
					return fmt.Errorf("broken cumulative budget evidence")
				}
				last = *e.After
			}
		}
		if !reflect.DeepEqual(budget.Snapshot(), last) {
			return fmt.Errorf("resume cumulative budget state differs; manual reconciliation required")
		}
	} else if budget != nil {
		return fmt.Errorf("cannot add a fresh budget to an existing unbudgeted run")
	}
	var sharedRuntime *agent.RuntimeMetadata
	for _, e := range j.finished {
		for _, obs := range e.Result.Observations {
			armcfg := cfg.Model
			if e.Arm == "jev_selective" {
				armcfg = cfg.Selective
			}
			complete, err := comparisonRuntime(armcfg, obs.Turn.TelemetryRaw, &sharedRuntime)
			if err != nil || (cfg.Model.EvidenceKind == "recorded_run" && !complete) {
				return fmt.Errorf("prior runtime provenance incomplete or inconsistent")
			}
		}
	}
	for i, entry := range pairOrder(cfg) {
		if _, ok := j.finished[pairEventKey(entry)]; ok {
			continue
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if budget != nil {
			s := budget.Snapshot()
			if s.Stopped != "" || s.Calls >= budget.limits.MaxCalls || s.ReservedMicroCNY > budget.limits.MaxMicroCNY-budget.reserveCost() {
				return fmt.Errorf("pair halted before arm: model budget unavailable")
			}
			entry.Before = &s
		}
		entry.Kind = "start"
		if err = writePairEvent(f, entry); err != nil {
			return err
		}
		begun := time.Now()
		c := cfg.Suite.Cases[i/2]
		driver, setupErr := factory(ctx, entry.Arm, c)
		if driver == nil && setupErr == nil {
			setupErr = fmt.Errorf("driver factory returned nil")
		}
		var result ConversationResult
		if setupErr != nil {
			result = ConversationResult{Case: c, Status: "setup_failed", SetupError: true, Errors: []string{"driver factory failed"}}
			entry.ExecutionFailures = append(entry.ExecutionFailures, PairExecutionFailure{Stage: "factory"})
		} else {
			tracked := &pairTrackingDriver{ConversationDriver: driver}
			result, _ = RunFactCheckedConversation(ctx, c, tracked)
			entry.ExecutionFailures = append(entry.ExecutionFailures, tracked.failures...)
		}
		if closer, ok := driver.(interface{ Close() error }); ok {
			if closeErr := closer.Close(); closeErr != nil {
				result.Errors = append(result.Errors, "driver cleanup failed")
				result.Status = "failed"
				result.Success = false
				entry.ExecutionFailures = append(entry.ExecutionFailures, PairExecutionFailure{Stage: "cleanup"})
			}
		}
		// The key is needed only during execution. New offline review requirements
		// are derived using this redacted fixture, not copied from live hashes.
		result.Fixture.APIKey = ""
		result.ReviewRequirements = nil
		entry.Kind = "finish"
		entry.Result = &result
		entry.ElapsedMS = float64(time.Since(begun)) / float64(time.Millisecond)
		if budget != nil {
			s := budget.Snapshot()
			entry.After = &s
		}
		if err = writePairEvent(f, entry); err != nil {
			return err
		}
		armcfg := cfg.Model
		if entry.Arm == "jev_selective" {
			armcfg = cfg.Selective
		}
		for _, obs := range result.Observations {
			complete, runtimeErr := comparisonRuntime(armcfg, obs.Turn.TelemetryRaw, &sharedRuntime)
			if runtimeErr != nil || (cfg.Model.EvidenceKind == "recorded_run" && !complete) {
				return fmt.Errorf("pair halted: actual runtime contract unavailable or differs")
			}
		}
		if budget != nil && budget.Snapshot().Stopped != "" {
			return fmt.Errorf("pair halted: model charge uncertain")
		}
	}
	return nil
}
