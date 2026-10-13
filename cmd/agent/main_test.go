package main

import (
	"context"
	"testing"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func TestExecutableRoutingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		min       float64
		wantError bool
	}{
		{"legacy", .80, false},
		{"jev_selective", .80, true},
		{"jev_selective", .90, false},
		{"model", .80, false},
		{"jev_hint", .80, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			h := agent.NewHandler(nil, nil, nil, nil, nil, nil)
			err := configureRouting(h, tc.mode, false, tc.min)
			if (err != nil) != tc.wantError {
				t.Fatalf("mode %s threshold %.2f error=%v", tc.mode, tc.min, err)
			}
			if err == nil && h.RoutingMode() != agent.RoutingMode(tc.mode) {
				t.Fatalf("configured mode=%s", h.RoutingMode())
			}
		})
	}
}

func TestExecutableObservabilityDefaultsOffAndValidatesEnabled(t *testing.T) {
	observer, err := configureObservability(context.Background(), func(string) string { return "" })
	if observer != nil || err != nil {
		t.Fatal("observability not default-off")
	}
	observer, err = configureObservability(context.Background(), func(key string) string {
		if key == "AGENT_LANGFUSE_ENABLED" {
			return "true"
		}
		return ""
	})
	if observer != nil || err == nil {
		t.Fatal("enabled configuration not validated")
	}
}
