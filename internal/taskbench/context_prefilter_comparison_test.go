package taskbench

import (
	"encoding/json"
	"testing"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func TestContextPrefilterCurrentRuntimeCanPair(t *testing.T) {
	s, a, b := comparisonInput()
	for _, run := range []*ConversationComparisonRun{&a, &b} {
		h := &agent.Handler{}
		if err := h.SetRoutingMode(run.Config.RoutingMode); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(h.EvaluationRuntimeMetadata())
		var actual map[string]any
		_ = json.Unmarshal(raw, &actual)
		for i := range run.Results[0].Observations {
			obs := &run.Results[0].Observations[i]
			var telemetry map[string]any
			_ = json.Unmarshal(obs.Turn.TelemetryRaw, &telemetry)
			runtime := telemetry["runtime"].(map[string]any)
			// Populate routing implementation fields from the actual handler,
			// rather than letting handwritten historical fixtures hide differences.
			for _, key := range []string{"jev_context_prefilter", "routing_revision"} {
				if value, ok := actual[key]; ok {
					runtime[key] = value
				}
			}
			obs.Turn.TelemetryRaw, _ = json.Marshal(telemetry)
		}
	}
	if _, err := CompareConversations(s, a, b); err != nil {
		t.Fatalf("current routing metadata prevents valid pairing: %v", err)
	}
}

func TestContextPrefilterMixedRuntimeVersionsAreRejected(t *testing.T) {
	for _, sameArm := range []bool{false, true} {
		s, a, b := comparisonInput()
		for _, run := range []*ConversationComparisonRun{&a, &b} {
			for i := range run.Results[0].Observations {
				if !sameArm && run == &a {
					continue // Historical model vs new selective.
				}
				if sameArm && i == 0 {
					continue // Historical and new observations within an arm.
				}
				obs := &run.Results[0].Observations[i]
				var telemetry map[string]any
				_ = json.Unmarshal(obs.Turn.TelemetryRaw, &telemetry)
				telemetry["runtime"].(map[string]any)["routing_revision"] = "selective-context-prefilter-v1"
				obs.Turn.TelemetryRaw, _ = json.Marshal(telemetry)
			}
		}
		if _, err := CompareConversations(s, a, b); err == nil {
			t.Fatalf("accepted old/new runtime mixture (same arm: %v)", sameArm)
		}
	}
}
