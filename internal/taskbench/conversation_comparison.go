package taskbench

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/frostloom/ai-gateway/internal/agent"
)

// Configuration is supplied by the experiment recorder, never the assistant.
// Profile hashes bind the complete fixture/seed definitions, not account IDs.
type ConversationComparisonConfig struct {
	RoutingMode       string            `json:"routing_mode"`
	EvidenceKind      string            `json:"evidence_kind"`
	DefinitionSHA256  string            `json:"definition_sha256"`
	ManifestSHA256    string            `json:"manifest_sha256"`
	ProfileSHA256     map[string]string `json:"profile_sha256"`
	Model             string            `json:"model"`
	ProviderProtocol  string            `json:"provider_protocol"`
	PromptSHA256      string            `json:"prompt_sha256"`
	ToolSchemaSHA256  string            `json:"tool_schema_sha256"`
	OutputBudget      int               `json:"output_budget"`
	ExecutionRevision string            `json:"execution_revision"`
}

type ConversationComparisonRun struct {
	Config  ConversationComparisonConfig `json:"config"`
	Results []ConversationResult         `json:"results"`
}

type ConversationComparisonTokens struct {
	LLMInput  int64 `json:"llm_input"`
	LLMOutput int64 `json:"llm_output"`
	JEVInput  int64 `json:"jev_input"`
	JEVOutput int64 `json:"jev_output"`
}

type ConversationComparisonMetrics struct {
	CompletionRate    *float64                      `json:"completion_rate"`
	ResponseLatencyMS *float64                      `json:"response_latency_ms"`
	LLMCalls          *int64                        `json:"llm_calls"`
	JEVCalls          *int64                        `json:"jev_calls"`
	Tokens            *ConversationComparisonTokens `json:"tokens"`
}

type ConversationComparisonReport struct {
	EvidenceKind        string                        `json:"evidence_kind"`
	QualityComparable   bool                          `json:"quality_comparable"`
	EffectClaimsAllowed bool                          `json:"effect_claims_allowed"`
	Model               ConversationComparisonMetrics `json:"model"`
	Selective           ConversationComparisonMetrics `json:"selective"`
	Limitations         []string                      `json:"limitations"`
}

// CompareConversations requires the entire supplied suite, in definition order.
// It recomputes evaluator results from observations rather than trusting stored
// Success/Status/DurationMS/ReviewRequirements or model-reported metrics.
// No model, database, network, or environment access occurs here.
func CompareConversations(suite ConversationSuite, model, selective ConversationComparisonRun) (ConversationComparisonReport, error) {
	report := ConversationComparisonReport{EvidenceKind: model.Config.EvidenceKind, Limitations: []string{
		"Recorder configuration, oracle snapshots and response telemetry are trusted provenance inputs, not cryptographic execution attestations.",
		"Response latency is the sum of supplied server total_latency_ms, not local replay DurationMS or end-to-end user task latency.",
		"local_stub evidence tests the evaluator only and cannot establish V1/V2 quality, latency or cost benefits.",
		"Pending semantic review or unequal valid full-task coverage makes quality unavailable; incomplete telemetry is unknown, never zero.",
		"No currency cost, population generalization or causal performance improvement is computed.",
	}}
	fail := func(err error) (ConversationComparisonReport, error) { return report, err }
	if model.Config.RoutingMode != "model" || selective.Config.RoutingMode != "jev_selective" {
		return fail(fmt.Errorf("comparison requires model versus jev_selective"))
	}
	for _, cfg := range []ConversationComparisonConfig{model.Config, selective.Config} {
		if !hasString([]string{"local_stub", "recorded_run"}, cfg.EvidenceKind) || strings.TrimSpace(cfg.Model) == "" || !hasString([]string{"openai", "anthropic"}, cfg.ProviderProtocol) || strings.TrimSpace(cfg.ExecutionRevision) == "" || cfg.OutputBudget <= 0 {
			return fail(fmt.Errorf("incomplete comparison configuration"))
		}
		for _, h := range []string{cfg.DefinitionSHA256, cfg.ManifestSHA256, cfg.PromptSHA256, cfg.ToolSchemaSHA256} {
			if !comparisonDigest(h) {
				return fail(fmt.Errorf("invalid comparison SHA256"))
			}
		}
		if cfg.DefinitionSHA256 != suite.DefinitionSHA256 || cfg.ManifestSHA256 != suite.ManifestSHA256 {
			return fail(fmt.Errorf("comparison definition binding mismatch"))
		}
	}
	a, b := model.Config, selective.Config
	a.RoutingMode = ""
	b.RoutingMode = ""
	if !reflect.DeepEqual(a, b) {
		return fail(fmt.Errorf("comparison conditions differ beyond routing mode"))
	}
	if len(suite.Cases) == 0 {
		return fail(fmt.Errorf("empty comparison suite"))
	}
	profiles := map[string]bool{}
	seen := map[string]bool{}
	for _, c := range suite.Cases {
		if err := validateConversation(c); err != nil {
			return fail(err)
		}
		if seen[c.Key()] {
			return fail(fmt.Errorf("duplicate suite case"))
		}
		seen[c.Key()] = true
		profiles[c.Scenario.Profile] = true
		if !comparisonDigest(a.ProfileSHA256[c.Scenario.Profile]) {
			return fail(fmt.Errorf("missing or invalid profile hash: %s", c.Scenario.Profile))
		}
	}
	if len(a.ProfileSHA256) != len(profiles) {
		return fail(fmt.Errorf("profile hash scope differs from suite"))
	}
	replays := make([]ConversationReplayReport, 0, 2)
	runtimeComplete := true
	var sharedRuntime *agent.RuntimeMetadata
	for _, run := range []ConversationComparisonRun{model, selective} {
		if len(run.Results) != len(suite.Cases) {
			return fail(fmt.Errorf("comparison must cover the full suite"))
		}
		input := ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: run.Config.EvidenceKind}
		for i, r := range run.Results {
			for _, obs := range r.Observations {
				complete, err := comparisonRuntime(run.Config, obs.Turn.TelemetryRaw, &sharedRuntime)
				if err != nil {
					return fail(err)
				}
				if !complete {
					runtimeComplete = false
				}
			}
			// JSON equivalence tolerates numeric representation changes after decoding.
			want, _ := json.Marshal(suite.Cases[i])
			got, _ := json.Marshal(r.Case)
			if string(want) != string(got) {
				return fail(fmt.Errorf("case definition/order mismatch at %d", i))
			}
			input.Episodes = append(input.Episodes, ConversationEpisode{Key: r.Case.Key(), Fixture: r.Fixture, Initial: r.Initial, Steps: r.Observations, Final: r.Final})
		}
		replay, err := ReplayConversations(suite, input)
		if err != nil {
			return fail(err)
		}
		for i := range replay.Results {
			r := &replay.Results[i]
			for _, check := range CheckConversationFacts(*r) {
				if check.Status == "mismatch" {
					r.Errors = append(r.Errors, fmt.Sprintf("turn %d fact mismatch: %s claimed %s expected %s", check.TurnIndex+1, check.Fact, check.Claim, check.Expected))
					r.Status = "failed"
					r.Success = false
				}
			}
		}
		replay.Metrics = SummarizeConversations(replay.Results)
		replays = append(replays, replay)
	}
	x, y := replays[0].Metrics, replays[1].Metrics
	if a.EvidenceKind == "recorded_run" && x.FullTasks > 0 && x.ValidFullTasks == x.FullTasks && y.ValidFullTasks == y.FullTasks && x.CompletionRate != nil && y.CompletionRate != nil {
		report.QualityComparable = true
		report.Model.CompletionRate = x.CompletionRate
		report.Selective.CompletionRate = y.CompletionRate
	}
	// Leave effect claims disabled: comparability alone establishes no improvement.
	left, lLatency, lCalls, lTokens := comparisonTelemetry(model.Results)
	right, rLatency, rCalls, rTokens := comparisonTelemetry(selective.Results)
	if runtimeComplete && lLatency && rLatency {
		report.Model.ResponseLatencyMS = left.ResponseLatencyMS
		report.Selective.ResponseLatencyMS = right.ResponseLatencyMS
	}
	if runtimeComplete && lCalls && rCalls {
		report.Model.LLMCalls = left.LLMCalls
		report.Model.JEVCalls = left.JEVCalls
		report.Selective.LLMCalls = right.LLMCalls
		report.Selective.JEVCalls = right.JEVCalls
	}
	if runtimeComplete && lTokens && rTokens {
		report.Model.Tokens = left.Tokens
		report.Selective.Tokens = right.Tokens
	}
	return report, nil
}

// Runtime values are captured by the server. Every available value must agree
// with the declared experiment and the shared execution contract; absent runtime
// data disables paired efficiency metrics rather than certifying the config.
func comparisonRuntime(cfg ConversationComparisonConfig, telemetry json.RawMessage, shared **agent.RuntimeMetadata) (bool, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(telemetry, &raw) != nil {
		return false, nil
	}
	if cfg.RoutingMode == "model" {
		var calls *int64
		if json.Unmarshal(raw["jev_calls"], &calls) == nil && calls != nil && *calls > 0 {
			return false, fmt.Errorf("model routing telemetry contains JEV calls")
		}
		for _, key := range []string{"jev_enabled", "jev_fast_path", "jev_short_circuit"} {
			if value, ok := raw[key]; ok {
				var flag bool
				if json.Unmarshal(value, &flag) != nil || flag {
					return false, fmt.Errorf("model routing has invalid or enabled %s", key)
				}
			}
		}
	}
	value, ok := raw["runtime"]
	if !ok || string(value) == "null" {
		return false, nil
	}
	var runtime agent.RuntimeMetadata
	if json.Unmarshal(value, &runtime) != nil || runtime.Model != cfg.Model || runtime.Protocol != cfg.ProviderProtocol || runtime.RoutingMode != cfg.RoutingMode || runtime.PromptSHA256 != cfg.PromptSHA256 || runtime.ToolsSHA256 != cfg.ToolSchemaSHA256 || runtime.MaxOutputTokens != cfg.OutputBudget || !comparisonDigest(runtime.EndpointSHA256) || !comparisonDigest(runtime.ContractSHA256) || runtime.ReplyRepairLimit < 0 || len(runtime.Thresholds) == 0 {
		return false, fmt.Errorf("runtime telemetry differs from comparison configuration or is incomplete")
	}
	for _, threshold := range runtime.Thresholds {
		if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 1 {
			return false, fmt.Errorf("invalid runtime threshold")
		}
	}
	runtime.RoutingMode = ""
	if *shared == nil {
		*shared = &runtime
	} else if !reflect.DeepEqual(**shared, runtime) {
		return false, fmt.Errorf("runtime execution contract differs beyond routing")
	}
	return true, nil
}

func comparisonDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}

func comparisonTelemetry(results []ConversationResult) (ConversationComparisonMetrics, bool, bool, bool) {
	latencyOK, callsOK, tokensOK := true, true, true
	var latency float64
	var llm, jev int64
	tokens := ConversationComparisonTokens{}
	turns := 0
	for _, r := range results {
		if len(r.Observations) != len(r.Case.Scenario.Turns) {
			latencyOK = false
			callsOK = false
			tokensOK = false
		}
		for _, obs := range r.Observations {
			turns++
			var raw map[string]json.RawMessage
			if json.Unmarshal(obs.Turn.TelemetryRaw, &raw) != nil {
				latencyOK = false
				callsOK = false
				tokensOK = false
				continue
			}
			var ms *float64
			if json.Unmarshal(raw["total_latency_ms"], &ms) != nil || ms == nil || *ms < 0 || math.IsNaN(*ms) || math.IsInf(*ms, 0) {
				latencyOK = false
			} else {
				latency += *ms
				if math.IsInf(latency, 0) {
					latencyOK = false
				}
			}
			count := func(key string) (int64, bool) {
				var n *int64
				err := json.Unmarshal(raw[key], &n)
				if err != nil || n == nil || *n < 0 {
					return 0, false
				}
				return *n, true
			}
			lc, lcOK := count("llm_calls")
			jc, jcOK := count("jev_calls")
			add := func(total *int64, n int64) bool {
				if n > math.MaxInt64-*total {
					return false
				}
				*total += n
				return true
			}
			if !lcOK || !jcOK || !add(&llm, lc) || !add(&jev, jc) {
				callsOK = false
				tokensOK = false
			}
			for _, provider := range []struct {
				prefix  string
				calls   int64
				in, out *int64
			}{{"llm", lc, &tokens.LLMInput, &tokens.LLMOutput}, {"jev", jc, &tokens.JEVInput, &tokens.JEVOutput}} {
				missing, okM := count(provider.prefix + "_usage_missing")
				responses, okR := count(provider.prefix + "_usage_responses")
				in, okI := count(provider.prefix + "_input_tokens")
				out, okO := count(provider.prefix + "_output_tokens")
				if !okM || !okR || !okI || !okO || missing != 0 || responses != provider.calls || (provider.calls == 0 && (in != 0 || out != 0)) || !add(provider.in, in) || !add(provider.out, out) {
					tokensOK = false
				}
			}
		}
	}
	if turns == 0 {
		latencyOK = false
		callsOK = false
		tokensOK = false
	}
	return ConversationComparisonMetrics{ResponseLatencyMS: &latency, LLMCalls: &llm, JEVCalls: &jev, Tokens: &tokens}, latencyOK, callsOK, tokensOK
}
