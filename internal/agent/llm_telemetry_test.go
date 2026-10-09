package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIAppliesRecordedOutputBudget(t *testing.T) {
	observedBudget := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		observedBudget = body.MaxTokens
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	client := NewLLMClient(srv.URL, "test", "test-model")
	client.maxTokens = 777
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatal(err)
	}
	if observedBudget != 777 {
		t.Fatalf("recorded budget was not sent: %d", observedBudget)
	}
}

func assertLLMTelemetry(t *testing.T, stats *TurnTelemetry, want map[string]float64) {
	t.Helper()
	raw, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}
	latency, ok := got["llm_latency_ms"].(float64)
	if !ok || latency < 10 {
		t.Errorf("llm_latency_ms = %v, want >= 10", got["llm_latency_ms"])
	}
}

// These HTTP fixtures catch discarded usage, invented cache counts, and failure attempts
// disappearing from telemetry while exercising the actual response decoder.
func TestLLMTelemetryOpenAI(t *testing.T) {
	cases := []struct {
		name, body                                   string
		status                                       int
		wantErr                                      bool
		responses, missing, input, output, hit, miss float64
	}{
		{"usage", `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":12,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`, 200, false, 1, 0, 100, 12, 80, 20},
		{"no_cache_fields", `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":12}}`, 200, false, 1, 0, 100, 12, 0, 0},
		{"zero_usage", `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, 200, false, 1, 0, 0, 0, 0, 0},
		{"missing", `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`, 200, false, 0, 1, 0, 0, 0, 0},
		{"partial", `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":42}}`, 200, false, 0, 1, 42, 0, 0, 0},
		{"parse_failure", `{`, 200, true, 0, 1, 0, 0, 0, 0},
		{"http_failure", `{"error":{"message":"unavailable"}}`, 503, true, 0, 1, 0, 0, 0, 0},
		{"empty_choices_with_usage", `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2}}`, 200, true, 1, 0, 5, 2, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(12 * time.Millisecond)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			client := NewLLMClient(srv.URL, "test-key", "test-model")
			client.proto = protoOpenAI
			stats := &TurnTelemetry{}
			ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
			_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hello"}}, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %v", err, tc.wantErr)
			}
			assertLLMTelemetry(t, stats, map[string]float64{"llm_calls": 1, "llm_usage_responses": tc.responses, "llm_usage_missing": tc.missing, "llm_input_tokens": tc.input, "llm_output_tokens": tc.output, "llm_cache_hit_tokens": tc.hit, "llm_cache_miss_tokens": tc.miss})
			if stats.LLMUsageProtocol != "openai" {
				t.Errorf("usage protocol = %q, want openai", stats.LLMUsageProtocol)
			}
		})
	}
}

func TestLLMTelemetryAnthropicRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		time.Sleep(12 * time.Millisecond)
		if attempts == 1 {
			io.WriteString(w, `{"stop_reason":"max_tokens","content":[{"type":"thinking"}],"usage":{"input_tokens":10,"output_tokens":8,"cache_read_input_tokens":30,"cache_creation_input_tokens":20}}`)
			return
		}
		io.WriteString(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":11,"output_tokens":3,"cache_read_input_tokens":31,"cache_creation_input_tokens":21}}`)
	}))
	defer srv.Close()
	client := NewLLMClient(srv.URL, "test-key", "test-model")
	client.proto = protoAnthropic
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	if _, err := client.Chat(ctx, []Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatal(err)
	}
	assertLLMTelemetry(t, stats, map[string]float64{"llm_calls": 2, "llm_usage_responses": 2, "llm_usage_missing": 0, "llm_input_tokens": 21, "llm_output_tokens": 11, "llm_cache_hit_tokens": 61, "llm_cache_miss_tokens": 0, "llm_cache_creation_tokens": 41})
	if stats.LLMLatencyMS < 24 {
		t.Errorf("retry latency = %v, want >= 24 ms across both attempts", stats.LLMLatencyMS)
	}
	if stats.LLMUsageProtocol != "anthropic" {
		t.Errorf("usage protocol = %q, want anthropic", stats.LLMUsageProtocol)
	}
}

func TestLLMTelemetryAnthropicRawAndMissing(t *testing.T) {
	for _, raw := range []bool{false, true} {
		for _, usage := range []string{"", `,"usage":{"input_tokens":10,"output_tokens":4}`} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(12 * time.Millisecond)
				io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]`+usage+`}`)
			}))
			client := NewLLMClient(srv.URL, "test-key", "test-model")
			client.proto = protoAnthropic
			stats := &TurnTelemetry{}
			ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
			var err error
			if raw {
				_, err = client.chatAnthropicRaw(ctx, []byte(`{"model":"test","messages":[]}`))
			} else {
				_, err = client.Chat(ctx, nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]float64{"llm_calls": 1, "llm_usage_responses": 0, "llm_usage_missing": 1, "llm_input_tokens": 0, "llm_output_tokens": 0}
			if usage != "" {
				want["llm_usage_responses"] = 1
				want["llm_usage_missing"] = 0
				want["llm_input_tokens"] = 10
				want["llm_output_tokens"] = 4
			}
			assertLLMTelemetry(t, stats, want)
			srv.Close()
		}
	}
}

func TestLLMTelemetryTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(12 * time.Millisecond)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()
	for _, protocol := range []llmProtocol{protoOpenAI, protoAnthropic} {
		client := NewLLMClient(srv.URL, "test-key", "test-model")
		client.proto = protocol
		stats := &TurnTelemetry{}
		ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
		if _, err := client.Chat(ctx, nil, nil); err == nil {
			t.Fatal("want transport error")
		}
		assertLLMTelemetry(t, stats, map[string]float64{"llm_calls": 1, "llm_usage_responses": 0, "llm_usage_missing": 1})
	}
}
