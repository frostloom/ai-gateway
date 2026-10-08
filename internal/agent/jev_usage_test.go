package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestJevUsageTelemetrySchema(t *testing.T) {
	raw, err := json.Marshal(&TurnTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jev_input_tokens", "jev_output_tokens", "jev_usage_responses", "jev_usage_missing"} {
		if got[name] != float64(0) {
			t.Errorf("%s = %v, want explicit zero", name, got[name])
		}
	}
}

func TestRecordJevResultUsage(t *testing.T) {
	cases := []struct {
		name, raw          string
		input, output      int64
		responses, missing int
	}{
		{"complete", `{"usage":{"input_tokens":120,"output_tokens":12}}`, 120, 12, 1, 0},
		{"zero", `{"usage":{"input_tokens":0,"output_tokens":0}}`, 0, 0, 1, 0},
		{"missing_usage", `{}`, 0, 0, 0, 1},
		{"null_usage", `{"usage":null}`, 0, 0, 0, 1},
		{"partial_input", `{"usage":{"input_tokens":120}}`, 120, 0, 0, 1},
		{"partial_output", `{"usage":{"output_tokens":12}}`, 0, 12, 0, 1},
		{"negative_input", `{"usage":{"input_tokens":-1,"output_tokens":12}}`, 0, 12, 0, 1},
		{"negative_output", `{"usage":{"input_tokens":120,"output_tokens":-1}}`, 120, 0, 0, 1},
		{"fractional_input", `{"usage":{"input_tokens":1.5,"output_tokens":12}}`, 0, 12, 0, 1},
		{"string_input", `{"usage":{"input_tokens":"120","output_tokens":12}}`, 0, 12, 0, 1},
		{"null_output", `{"usage":{"input_tokens":120,"output_tokens":null}}`, 120, 0, 0, 1},
		{"overflow", `{"usage":{"input_tokens":9223372036854775808,"output_tokens":12}}`, 0, 12, 0, 1},
		{"invalid_json", `{"usage":{"input_tokens":120,`, 0, 0, 0, 1},
		{"wrong_usage_type", `{"usage":[120,12]}`, 0, 0, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats := &TurnTelemetry{JevUsageMissing: 1}
			ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
			recordJevResultUsage(ctx, tc.raw)
			if stats.JevInputTokens != tc.input || stats.JevOutputTokens != tc.output || stats.JevUsageResponses != tc.responses || stats.JevUsageMissing != tc.missing {
				t.Errorf("usage = (%d, %d, %d, %d), want (%d, %d, %d, %d)", stats.JevInputTokens, stats.JevOutputTokens, stats.JevUsageResponses, stats.JevUsageMissing, tc.input, tc.output, tc.responses, tc.missing)
			}
		})
	}
}

func TestRecordJevResultUsageRequestIsolation(t *testing.T) {
	first, second := &TurnTelemetry{JevUsageMissing: 2}, &TurnTelemetry{JevUsageMissing: 1}
	firstCtx := context.WithValue(context.Background(), telemetryKey{}, first)
	secondCtx := context.WithValue(context.Background(), telemetryKey{}, second)
	recordJevResultUsage(firstCtx, `{"usage":{"input_tokens":10,"output_tokens":2}}`)
	recordJevResultUsage(secondCtx, `{"usage":{"input_tokens":20,"output_tokens":3}}`)
	recordJevResultUsage(firstCtx, `{"usage":{"input_tokens":11}}`)
	recordJevResultUsage(context.Background(), `{"usage":{"input_tokens":100,"output_tokens":100}}`)
	if first.JevInputTokens != 21 || first.JevOutputTokens != 2 || first.JevUsageResponses != 1 || first.JevUsageMissing != 1 {
		t.Errorf("first telemetry = %+v", first)
	}
	if second.JevInputTokens != 20 || second.JevOutputTokens != 3 || second.JevUsageResponses != 1 || second.JevUsageMissing != 0 {
		t.Errorf("second telemetry = %+v", second)
	}
}
