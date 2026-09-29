package agent

import "context"

// TurnTelemetry is request-local; never shared between concurrent conversations.
// LLMCalls counts actual upstream attempts (including retries), not tool traces.
type TurnTelemetry struct {
	JevEnabled      bool `json:"jev_enabled"`
	JevCalls        int  `json:"jev_calls"`
	JevErrors       int  `json:"jev_errors"`
	JevBlocked      bool `json:"jev_blocked"`
	JevShortCircuit bool `json:"jev_short_circuit"`
	LLMCalls        int  `json:"llm_calls"`
}

type telemetryKey struct{}

func turnTelemetry(ctx context.Context) *TurnTelemetry {
	v, _ := ctx.Value(telemetryKey{}).(*TurnTelemetry)
	return v
}
