package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRuntimeMetadataIdentifiesSharedExperiment(t *testing.T) {
	h := &Handler{tb: NewToolbox(NewAdminClient("http://unused.local")), llm: NewLLMClient("https://example.invalid", "unused", "test-model")}
	first := h.runtimeMetadata()
	if first.Model != "test-model" || first.Protocol != "openai" || len(first.PromptSHA256) != 64 || len(first.ToolsSHA256) != 64 || len(first.ContractSHA256) != 64 {
		t.Fatalf("incomplete experiment metadata: %+v", first)
	}
	if err := h.SetRoutingMode("model"); err != nil {
		t.Fatal(err)
	}
	model := h.runtimeMetadata()
	if err := h.SetRoutingMode("jev_selective"); err != nil {
		t.Fatal(err)
	}
	selective := h.runtimeMetadata()
	if model.PromptSHA256 != selective.PromptSHA256 || model.ToolsSHA256 != selective.ToolsSHA256 || model.ContractSHA256 != selective.ContractSHA256 || model.Model != selective.Model || model.MaxOutputTokens != selective.MaxOutputTokens {
		t.Fatal("routing mode changed shared prompt, tools or model")
	}
	if model.RoutingMode == selective.RoutingMode || selective.Thresholds["intent_min"] < .90 {
		t.Fatal("missing explicit routing policy")
	}
	if selective.ReplyRepairLimit != 1 {
		t.Fatal("unbounded correction would invalidate costs")
	}
}

func TestRuntimeMetadataDistinguishesGatewayWithoutSecrets(t *testing.T) {
	h := &Handler{tb: NewToolbox(NewAdminClient("http://unused.local")), llm: NewLLMClient("https://first.invalid/v1", "unused", "same-model")}
	first := h.runtimeMetadata().EndpointSHA256
	h.llm = NewLLMClient("https://second.invalid/v1", "other-secret", "same-model")
	if first == h.runtimeMetadata().EndpointSHA256 {
		t.Fatal("different gateways indistinguishable")
	}
	h.llm = NewLLMClient("https://user:secret@first.invalid/v1/?api_key=secret", "other-secret", "same-model")
	if first != h.runtimeMetadata().EndpointSHA256 {
		t.Fatal("metadata hash includes credentials or insignificant trailing slash")
	}
}

func TestEarlyErrorRetainsRuntimeAndTime(t *testing.T) {
	h := &Handler{tb: NewToolbox(NewAdminClient("http://unused.local")), llm: NewLLMClient("https://example.invalid", "unused", "test-model")}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/chat", nil))
	var body struct {
		Telemetry *TurnTelemetry `json:"telemetry"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Telemetry == nil || body.Telemetry.Runtime == nil || body.Telemetry.TotalLatencyMS < 0 {
		t.Fatalf("early errors discard configuration/timing: %s", w.Body)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &fields)
	var telemetryFields map[string]json.RawMessage
	json.Unmarshal(fields["telemetry"], &telemetryFields)
	if _, present := telemetryFields["total_latency_ms"]; !present {
		t.Fatal("missing timer")
	}
}

func TestProviderUsagePreservesUnknownAndExplicitZero(t *testing.T) {
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	recordProviderUsage(ctx, []byte(`{"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":0}}`))
	recordProviderUsage(ctx, []byte(`{"usage":{"input_tokens":10,"output_tokens":2}}`))
	raw, _ := json.Marshal(stats)
	var body struct {
		Usage []map[string]any `json:"llm_usage_raw"`
	}
	json.Unmarshal(raw, &body)
	if len(body.Usage) != 2 || body.Usage[0]["cache_read_input_tokens"] != float64(0) {
		t.Fatalf("usage evidence missing explicit zero: %s", raw)
	}
	if _, present := body.Usage[1]["cache_read_input_tokens"]; present {
		t.Fatal("invented zero for unknown cache")
	}
}
