package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/frostloom/ai-gateway/internal/observability"
)

func TestObservedRoutingSummaryForwardsExistingStats(t *testing.T) {
	h := &Handler{tb: NewToolbox(nil)}
	mapper, ok := any(h).(interface {
		observabilitySummary(*TurnTelemetry, bool) observability.Summary
	})
	if !ok {
		t.Fatal("missing stats-to-observer mapping")
	}
	for _, tool := range []string{"get_balance", "", "unknown_safe_slug", "secret-tool-canary\ntext"} {
		t.Run(tool, func(t *testing.T) {
			stats := &TurnTelemetry{JevEnabled: true, JevCalls: 1, JevErrors: 1, JevBlocked: true, JevShortCircuit: true, JevRouteShape: "complex", JevShapeConfidence: .92, JevLegitProbability: .11, JevRiskScore: 1.9, JevRiskConfidence: .98, JevFastPathEligible: false, JevFastPathTool: tool}
			before := *stats
			summary := mapper.observabilitySummary(stats, true)
			if !reflect.DeepEqual(*stats, before) || !summary.Pending || summary.JevCalls != 1 || summary.JevErrors != 1 {
				t.Fatal("metadata conversion changed telemetry or existing counters/pending")
			}
			_, disabledTurn := h.observer.StartTurn(context.Background(), "jev_selective")
			disabledTurn.End(200, summary)
			if !reflect.DeepEqual(*stats, before) {
				t.Fatal("nil observer changed original telemetry")
			}
			o, capture := localObserver(t)
			_, turn := o.StartTurn(context.Background(), "jev_selective")
			turn.End(200, summary)
			if err := o.ForceFlush(context.Background()); err != nil {
				t.Fatal(err)
			}
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.spans) != 1 {
				t.Fatalf("spans=%d", len(capture.spans))
			}
			wantTool := tool
			if tool != "get_balance" && tool != "" {
				wantTool = "invalid"
			}
			for key, want := range map[string]string{"jev_enabled": "true", "jev_blocked": "true", "jev_short_circuit": "true", "jev_route_shape": "complex", "jev_shape_confidence": "0.92", "jev_legit_probability": "0.11", "jev_risk_score": "1.9", "jev_risk_confidence": "0.98", "jev_fast_path_eligible": "false", "jev_fast_path_tool": wantTool} {
				if got := observedAttr(capture.spans[0], "langfuse.observation.metadata."+key); got != want {
					t.Errorf("%s=%q want %q", key, got, want)
				}
			}
		})
	}
}
