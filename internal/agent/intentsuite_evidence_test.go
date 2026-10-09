package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIntentPreservesFailureEvidence(t *testing.T) {
	body := `{"error":"provider unavailable","session_id":"s1","pending_preview":"preview","pending_confirm":true,"trace":[{"tool":"recharge","args":"{\"amount_money\":5}","confirmed":false,"preview":"5元","result":"wait","phase":"preview","resolved_args":"{\"amount_money\":5}","outcome":"waiting","duration_ms":2}],"telemetry":{"llm_calls":2,"llm_usage_missing":2}}`
	cli := &http.Client{Transport: intentEvidenceTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	p := predictOne(context.Background(), cli, "http://offline.test", "key", IntentSample{ID: "failure"})
	raw, _ := json.Marshal(p)
	var v map[string]any
	json.Unmarshal(raw, &v)
	if v["RawBody"] != body || v["SessionID"] != "s1" || v["PendingPreview"] != "preview" || v["HTTPError"] != "provider unavailable" {
		t.Fatalf("failure evidence lost: %s", raw)
	}
	if v["TraceRaw"] == nil {
		t.Fatal("full raw trace missing")
	}
	if trace, ok := v["Trace"].([]any); !ok || len(trace) != 1 || trace[0].(map[string]any)["preview"] != "5元" {
		t.Fatalf("trace lost: %s", raw)
	}
}
func TestIntentFailedCallsIncludedInUsage(t *testing.T) {
	m := ComputeIntentMetrics([]IntentPrediction{{HTTPCode: 502, Telemetry: &TurnTelemetry{LLMCalls: 2, LLMUsageMissing: 2, JevCalls: 1, JevErrors: 1}}}, nil)
	if m.TelemetrySamples != 1 || m.AvgLLMCalls != 2 || m.JevCalls != 1 {
		t.Fatalf("failed attempt usage excluded: %+v", m)
	}
}

type intentEvidenceTransport func(*http.Request) (*http.Response, error)

func (f intentEvidenceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClarificationFailureStaysInCoverageDenominator(t *testing.T) {
	m := ComputeIntentMetrics([]IntentPrediction{{Sample: IntentSample{Intent: "recharge", AskBack: true}, HTTPCode: 502}, {Sample: IntentSample{Intent: "recharge", AskBack: true}, HTTPCode: 200, Reply: "充值多少元？"}}, nil)
	if m.AskBackTotal != 2 || m.AskBackHandled != 1 {
		t.Fatalf("clarification failed request excluded: %+v", m)
	}
}
func TestIntentClarificationMustAskRelevantMissingSlot(t *testing.T) {
	for _, tt := range []struct {
		reply string
		want  bool
	}{{"今天下雨吗？", false}, {"请确认充值100元？", false}, {"请提供充值金额。", true}, {"充值多少元？", true}} {
		p := IntentPrediction{Sample: IntentSample{Intent: "recharge", AskBack: true}, HTTPCode: 200, Reply: tt.reply}
		if got := clarificationHandled(p); got != tt.want {
			t.Errorf("%q handled=%v want=%v", tt.reply, got, tt.want)
		}
	}
}
