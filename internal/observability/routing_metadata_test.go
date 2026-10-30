package observability

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func routingSummary(t *testing.T) Summary {
	t.Helper()
	var s Summary
	if err := json.Unmarshal([]byte(`{"JevCalls":1,"JevErrors":0,"JevEnabled":true,"JevBlocked":false,"JevShortCircuit":false,"RouteShape":"single_read","ShapeConfidence":0.97,"LegitProbability":0.95,"RiskScore":1.69,"RiskConfidence":0.91,"FastPathEligible":false,"FastPathTool":"get_balance"}`), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func routingSummaryField(s *Summary, name string, value any) {
	// Missing new Summary fields are ignored only in the pre-implementation RED:
	// the real OTLP assertions below then fail for the absent metadata.
	f := reflect.ValueOf(s).Elem().FieldByName(name)
	if f.IsValid() {
		f.Set(reflect.ValueOf(value))
	}
}

func TestTurnExportsExistingJevRoutingMetadata(t *testing.T) {
	for _, content := range []bool{false, true} {
		t.Run(map[bool]string{false: "capture_off", true: "capture_on"}[content], func(t *testing.T) {
			server, captured := receiver(t)
			defer server.Close()
			cfg := enabled(server.URL)
			cfg.CaptureContent = content
			o, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer o.Shutdown(context.Background())
			_, turn := o.StartTurn(context.Background(), "jev_selective")
			turn.Input("raw-customer-input-canary")
			turn.Output("raw-customer-output-canary")
			turn.End(200, routingSummary(t))
			if err := o.ForceFlush(context.Background()); err != nil {
				t.Fatal(err)
			}
			captured.mu.Lock()
			defer captured.mu.Unlock()
			if len(captured.spans) != 1 {
				t.Fatalf("spans=%d", len(captured.spans))
			}
			got := attrs(captured.spans[0])
			for key, value := range map[string]string{"jev_enabled": "true", "jev_blocked": "false", "jev_short_circuit": "false", "jev_route_shape": "single_read", "jev_shape_confidence": "0.97", "jev_legit_probability": "0.95", "jev_risk_score": "1.69", "jev_risk_confidence": "0.91", "jev_fast_path_eligible": "false", "jev_fast_path_tool": "get_balance", "jev_calls": "1", "jev_errors": "0"} {
				if got["langfuse.observation.metadata."+key] != value {
					t.Errorf("%s=%q, want %q", key, got["langfuse.observation.metadata."+key], value)
				}
			}
			if !content && (strings.Contains(string(captured.raw), "raw-customer-") || got["langfuse.observation.input"] != "" || got["langfuse.observation.output"] != "") {
				t.Error("capture off exported raw input/output")
			}
			if content && (got["langfuse.observation.input"] != `"raw-customer-input-canary"` || got["langfuse.observation.output"] != `"raw-customer-output-canary"`) {
				t.Error("capture on no longer follows the existing content policy")
			}
		})
	}
}

func TestTurnJevMetadataInvalidValuesAndNoCall(t *testing.T) {
	cases := []struct {
		name, field, key, want string
		value                  any
	}{
		{"shape_text", "RouteShape", "jev_route_shape", "invalid", "secret-shape-canary"},
		{"shape_empty", "RouteShape", "jev_route_shape", "", ""},
		{"shape_complex", "RouteShape", "jev_route_shape", "complex", "complex"},
		{"tool_text", "FastPathTool", "jev_fast_path_tool", "invalid", "secret-tool-canary\ncustomer text"},
		{"tool_long", "FastPathTool", "jev_fast_path_tool", "invalid", strings.Repeat("a", 65)},
		{"tool_empty", "FastPathTool", "jev_fast_path_tool", "", ""},
		{"shape_nan", "ShapeConfidence", "jev_shape_confidence", "invalid", math.NaN()},
		{"shape_negative", "ShapeConfidence", "jev_shape_confidence", "invalid", -0.01},
		{"legit_over", "LegitProbability", "jev_legit_probability", "invalid", 1.01},
		{"legit_zero", "LegitProbability", "jev_legit_probability", "0", 0.0},
		{"confidence_inf", "RiskConfidence", "jev_risk_confidence", "invalid", math.Inf(1)},
		{"confidence_one", "RiskConfidence", "jev_risk_confidence", "1", 1.0},
		{"risk_negative", "RiskScore", "jev_risk_score", "invalid", -0.01},
		{"risk_over", "RiskScore", "jev_risk_score", "invalid", 2.01},
		{"risk_two", "RiskScore", "jev_risk_score", "2", 2.0},
	}
	server, captured := receiver(t)
	defer server.Close()
	o, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Shutdown(context.Background())
	for _, tc := range cases {
		s := routingSummary(t)
		routingSummaryField(&s, tc.field, tc.value)
		_, turn := o.StartTurn(context.Background(), "jev_selective")
		turn.End(200, s)
	}
	s := routingSummary(t)
	s.JevCalls = 0
	_, turn := o.StartTurn(context.Background(), "model")
	turn.End(200, s)
	if err := o.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	captured.mu.Lock()
	defer captured.mu.Unlock()
	if len(captured.spans) != len(cases)+1 {
		t.Fatalf("spans=%d", len(captured.spans))
	}
	for i, tc := range cases {
		if got := attrs(captured.spans[i])["langfuse.observation.metadata."+tc.key]; got != tc.want {
			t.Errorf("%s=%q, want %q", tc.name, got, tc.want)
		}
	}
	for key := range attrs(captured.spans[len(cases)]) {
		for _, suffix := range []string{"jev_enabled", "jev_blocked", "jev_short_circuit", "jev_route_shape", "jev_shape_confidence", "jev_legit_probability", "jev_risk_score", "jev_risk_confidence", "jev_fast_path_eligible", "jev_fast_path_tool"} {
			if key == "langfuse.observation.metadata."+suffix {
				t.Errorf("no JEV call exported %s", suffix)
			}
		}
	}
	if strings.Contains(string(captured.raw), "secret-shape-canary") || strings.Contains(string(captured.raw), "secret-tool-canary") {
		t.Error("invalid metadata leaked free text")
	}
	var disabled *Observer
	_, nilTurn := disabled.StartTurn(context.Background(), "model")
	nilTurn.End(200, s)
}
