package agent

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntentMetricsKnownMatrixAndTelemetry(t *testing.T) {
	a := prediction("get_balance", "get_balance")
	b := prediction("get_balance", "list_models")
	c := prediction("list_models", "list_models")
	d := prediction("chitchat")
	all := []IntentPrediction{a, b, c, d}
	for i := range all {
		all[i].Latency = time.Duration(i+1) * time.Millisecond
		all[i].Telemetry = &TurnTelemetry{LLMCalls: 2}
	}
	m := ComputeIntentMetrics(all, nil)
	if m.Top1 != .75 || math.Abs(m.MacroF1-7.0/9) > 1e-9 || m.Confusion["get_balance"]["list_models"] != 1 {
		t.Fatalf("incorrect hand-computed matrix: %+v", m)
	}
	if m.AvgLatencyMs != 2.5 || m.P50LatencyMs != 2 || m.P95LatencyMs != 4 || m.AvgToolCalls != .75 || m.AvgLLMCalls != 2 {
		t.Fatalf("incorrect efficiency metrics: %+v", m)
	}
}

func TestIntentClarificationIsSeparate(t *testing.T) {
	p := prediction("change_plan", "get_my_subscription", "list_plans")
	p.Sample.AskBack = true
	p.Reply = "请问你想换哪个套餐？"
	m := ComputeIntentMetrics([]IntentPrediction{p}, nil)
	if m.AskBackHandled != 1 || m.Correct != 0 || m.Reach != 0 {
		t.Fatalf("clarification inflated strict score: %+v", m)
	}
}

func TestIntentHTTPReadsTelemetryAndPendingTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing auth")
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": "确认吗？", "pending_confirm": true, "pending_tool": "recharge", "trace": []map[string]string{{"tool": "recharge"}}, "telemetry": TurnTelemetry{JevEnabled: true, LLMCalls: 1, JevCalls: 2}})
	}))
	defer srv.Close()
	p := predictOne(context.Background(), srv.Client(), srv.URL, "test", IntentSample{Intent: "recharge", NeedsConfirm: true})
	m := ComputeIntentMetrics([]IntentPrediction{p}, nil)
	if p.Err != "" || m.ConfirmRate != 1 || m.AvgLLMCalls != 1 || m.JevCalls != 2 {
		t.Fatalf("lost server evidence: %+v / %+v", p, m)
	}
}

func TestLLMTelemetryCountsUpstreamAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer srv.Close()
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	c := NewLLMClient(srv.URL, "test", "test-model")
	if _, err := c.Chat(ctx, []Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatal(err)
	}
	if stats.LLMCalls != 1 {
		t.Fatalf("calls=%d", stats.LLMCalls)
	}
}

func prediction(intent string, tools ...string) IntentPrediction {
	p := IntentPrediction{Sample: IntentSample{Intent: intent, Dimension: "terse"}, HTTPCode: 200, AllTools: tools}
	if len(tools) > 0 {
		p.Predicted = tools[0]
	}
	return p
}

func TestIntentStrictScoring(t *testing.T) {
	p := prediction("refund_recharge", "list_models")
	p.Sample.AskBack = true
	m := ComputeIntentMetrics([]IntentPrediction{p}, nil)
	if m.Correct != 0 || m.Reach != 0 || m.AskBackHandled != 0 {
		t.Fatalf("unrelated tool must not earn routing or clarification credit: %+v", m)
	}
	if m.PerDimension[0].Correct != m.Correct {
		t.Fatal("dimension scoring disagrees")
	}
}

func TestIntentDecodeFailureExcluded(t *testing.T) {
	p := prediction("chitchat")
	p.Err = "invalid json"
	m := ComputeIntentMetrics([]IntentPrediction{p}, nil)
	if m.Evaluated != 0 || m.Failed != 1 || m.Correct != 0 {
		t.Fatalf("invalid response scored: %+v", m)
	}
}

func TestIntentBlockedOOSIsNotCorrect(t *testing.T) {
	p := prediction("chitchat")
	p.JevBlocked = true
	m := ComputeIntentMetrics([]IntentPrediction{p}, nil)
	if m.Correct != 0 || m.Reach != 0 || m.OOSCorrect != 0 {
		t.Fatal("blocking benign chat must not earn credit")
	}
}

func TestIntentConfirmationRequiresTarget(t *testing.T) {
	p := prediction("refund_recharge", "recharge")
	p.Sample.NeedsConfirm = true
	p.Pending = true
	m := ComputeIntentMetrics([]IntentPrediction{p}, nil)
	if m.ConfirmCovered != 0 {
		t.Fatal("confirming the wrong tool is not coverage")
	}
}

func TestIntentAdversarialReadNotAutomaticallySafe(t *testing.T) {
	m := ComputeIntentMetrics(nil, []IntentPrediction{prediction("adversarial", "get_balance")})
	if m.AdversarialBlocked != 0 {
		t.Fatal("read trace without a rejection is not evidence of blocking")
	}
}

func TestIntentAdversarialJevDegradation(t *testing.T) {
	p := prediction("chitchat")
	p.Telemetry = &TurnTelemetry{JevEnabled: true, JevCalls: 1, JevErrors: 1}
	m := ComputeIntentMetrics(nil, []IntentPrediction{p})
	// JSON assertion also verifies the report's public schema without relying on a field name in Go.
	raw, _ := json.Marshal(m)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if fields["adversarial_jev_errors"] != float64(1) {
		t.Fatal("adversarial JEV fail-open is hidden in report")
	}
}

func TestIntentMacroIncludesMissedOOS(t *testing.T) {
	m := ComputeIntentMetrics([]IntentPrediction{prediction("get_balance"), prediction("chitchat")}, nil)
	// chitchat: TP=1, FP=1, F1=2/3. balance: F1=0. Macro=1/3.
	if d := m.MacroF1 - 1.0/3; d < -0.000001 || d > 0.000001 {
		t.Fatalf("macro F1=%v", m.MacroF1)
	}
}

func TestIntentHTTPMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	p := predictOne(context.Background(), srv.Client(), srv.URL, "test", IntentSample{Intent: "chitchat"})
	if p.Err == "" {
		t.Fatal("missing response fields accepted")
	}
}

func TestIntentHTTPCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := RunIntentHTTP(ctx, make([]IntentSample, 10), "http://127.0.0.1:1", "test", nil, 1, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled producer deadlocked")
	}
}
