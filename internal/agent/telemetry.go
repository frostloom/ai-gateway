package agent

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/observability"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// TurnTelemetry is request-local; never shared between concurrent conversations.
// LLMCalls counts client attempts, including transport-local budget rejections.
// Guard journals distinguish reserved provider attempts from local rejections.
type TurnTelemetry struct {
	LangfuseTraceID        string            `json:"langfuse_trace_id,omitempty"`
	Runtime                *RuntimeMetadata  `json:"runtime,omitempty"`
	TotalLatencyMS         float64           `json:"total_latency_ms"`
	ReplyRepairs           int               `json:"reply_repairs"`
	TurnStage              string            `json:"turn_stage,omitempty"`
	LLMUsageRaw            []json.RawMessage `json:"llm_usage_raw,omitempty"`
	JevEnabled             bool              `json:"jev_enabled"`
	JevCalls               int               `json:"jev_calls"`
	JevErrors              int               `json:"jev_errors"`
	JevFailures            []jev.Failure     `json:"jev_failures,omitempty"`
	JevBlocked             bool              `json:"jev_blocked"`
	JevShortCircuit        bool              `json:"jev_short_circuit"`
	LLMCalls               int               `json:"llm_calls"`
	JevLatencyMS           float64           `json:"jev_latency_ms"`
	LLMLatencyMS           float64           `json:"llm_latency_ms"`
	ToolLatencyMS          float64           `json:"tool_latency_ms"`
	JevIntent              string            `json:"jev_intent,omitempty"`
	JevIntentConfidence    float64           `json:"jev_intent_confidence"`
	JevRouteShape          string            `json:"jev_route_shape,omitempty"`
	JevShapeConfidence     float64           `json:"jev_shape_confidence"`
	JevLegitProbability    float64           `json:"jev_legit_probability"`
	JevRiskScore           float64           `json:"jev_risk_score"`
	JevRiskConfidence      float64           `json:"jev_risk_confidence"`
	JevFastPathEligible    bool              `json:"jev_fast_path_eligible"`
	JevFastPath            bool              `json:"jev_fast_path"`
	JevFastPathTool        string            `json:"jev_fast_path_tool,omitempty"`
	JevFastPathReason      string            `json:"jev_fast_path_reason,omitempty"`
	JevInputTokens         int64             `json:"jev_input_tokens"`
	JevOutputTokens        int64             `json:"jev_output_tokens"`
	JevUsageResponses      int               `json:"jev_usage_responses"`
	JevUsageMissing        int               `json:"jev_usage_missing"`
	LLMUsageResponses      int               `json:"llm_usage_responses"`
	LLMInputTokens         int64             `json:"llm_input_tokens"`
	LLMOutputTokens        int64             `json:"llm_output_tokens"`
	LLMCacheHitTokens      int64             `json:"llm_cache_hit_tokens"`
	LLMCacheMissTokens     int64             `json:"llm_cache_miss_tokens"`
	LLMCacheCreationTokens int64             `json:"llm_cache_creation_tokens"`
	LLMUsageProtocol       string            `json:"llm_usage_protocol,omitempty"`
	LLMUsageMissing        int               `json:"llm_usage_missing"`
}

type telemetryKey struct{}

// Preserve optional cache/reasoning fields exactly as supplied. Missing is not
// explicit zero. Only the usage object is retained, never provider credentials.
func recordProviderUsage(ctx context.Context, raw []byte) {
	stats := turnTelemetry(ctx)
	if stats == nil {
		return
	}
	var body struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(raw, &body) == nil && len(body.Usage) > 0 && string(body.Usage) != "null" {
		stats.LLMUsageRaw = append(stats.LLMUsageRaw, append(json.RawMessage(nil), body.Usage...))
	}
}

func turnTelemetry(ctx context.Context) *TurnTelemetry {
	v, _ := ctx.Value(telemetryKey{}).(*TurnTelemetry)
	return v
}

// recordJevResultUsage reads TypeSafe's response usage without estimating tokens.
// The caller counts the attempt as missing before Evaluate; only both valid
// required counts resolve it. Valid partial counts are retained for reporting.
func recordJevResultUsage(ctx context.Context, raw string) {
	stats := turnTelemetry(ctx)
	if stats == nil {
		return
	}
	var response struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return
	}
	count := func(name string) *int64 {
		var value *int64
		if err := json.Unmarshal(response.Usage[name], &value); err != nil || value == nil || *value < 0 {
			return nil
		}
		return value
	}
	input, output := count("input_tokens"), count("output_tokens")
	if input != nil {
		stats.JevInputTokens += *input
	}
	if output != nil {
		stats.JevOutputTokens += *output
	}
	if input != nil && output != nil {
		stats.JevUsageResponses++
		stats.JevUsageMissing--
	}
}

// beginLLMAttempt includes failed HTTP attempts and response reading in latency.
// Usage starts as missing until a response supplies both input and output counts.
type llmObservationKey struct{}
type llmObservation struct {
	op       *observability.Operation
	protocol string
}

func beginLLMAttempt(ctx context.Context, protocol llmProtocol) (context.Context, func(error)) {
	ctx, op := observability.Start(ctx, "llm.attempt", "generation")
	ctx = context.WithValue(ctx, llmObservationKey{}, llmObservation{op: op, protocol: string(protocol)})
	stats := turnTelemetry(ctx)
	if stats != nil {
		stats.LLMCalls++
		stats.LLMUsageMissing++
		if stats.Runtime != nil {
			op.Model(stats.Runtime.Model)
			op.BasePrompt(stats.Runtime.PromptSHA256)
		}
		if stats.LLMUsageProtocol == "" {
			stats.LLMUsageProtocol = string(protocol)
		} else if stats.LLMUsageProtocol != string(protocol) {
			stats.LLMUsageProtocol = "mixed"
		}
	}
	op.Usage(observability.Usage{Protocol: string(protocol)})
	started := time.Now()
	return ctx, func(err error) {
		if stats != nil {
			stats.LLMLatencyMS += float64(time.Since(started)) / float64(time.Millisecond)
		}
		op.End(err)
	}
}

// recordLLMUsage preserves supplied counts, including explicit zeroes and partial
// usage. Cache counts remain separate: Anthropic input excludes cache reads and
// creation; cache creation is not equivalent to an OpenAI provider's cache miss.
func recordLLMUsage(ctx context.Context, input, output, hit, miss, creation *int64) {
	observation, _ := ctx.Value(llmObservationKey{}).(llmObservation)
	observation.op.Usage(observability.Usage{Protocol: observation.protocol, Input: input, Output: output, CacheRead: hit, CacheMiss: miss, CacheCreation: creation})

	stats := turnTelemetry(ctx)
	if stats == nil {
		return
	}
	if input != nil {
		stats.LLMInputTokens += *input
	}
	if output != nil {
		stats.LLMOutputTokens += *output
	}
	if hit != nil {
		stats.LLMCacheHitTokens += *hit
	}
	if miss != nil {
		stats.LLMCacheMissTokens += *miss
	}
	if creation != nil {
		stats.LLMCacheCreationTokens += *creation
	}
	if input != nil && output != nil {
		stats.LLMUsageResponses++
		stats.LLMUsageMissing--
	}
}
