package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

type llmRequestContextKey struct{}

type llmRequestContextCase struct {
	name, path, body, response string
}

func llmRequestContextCases() []llmRequestContextCase {
	return []llmRequestContextCase{
		{"openai", "/chat/completions", `{"model":"context-model","max_tokens":128,"messages":[{"role":"system","content":"system-fixed"},{"role":"user","content":"hello"}]}`, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`},
		{"anthropic", "/v1/messages", `{"model":"context-model","max_tokens":128,"system":"system-fixed","messages":[{"role":"user","content":[{"text":"hello","type":"text"}]}]}`, `{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`},
		{"anthropic_raw", "/v1/messages", "{\n \"model\":\"context-model\", \"max_tokens\":128, \"messages\":[{\"role\":\"user\",\"content\":\"raw-fixed\"}]\n}", `{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`},
	}
}

func (tc llmRequestContextCase) call(ctx context.Context, c *LLMClient) (*Message, error) {
	messages := []Message{{Role: "system", Content: "system-fixed"}, {Role: "user", Content: "hello"}}
	switch tc.name {
	case "openai":
		return c.chatOpenAI(ctx, messages, nil)
	case "anthropic":
		return c.chatAnthropic(ctx, messages, nil, 128)
	default:
		return c.chatAnthropicRaw(ctx, []byte(tc.body))
	}
}

type llmRequestContextTransport struct {
	base    http.RoundTripper
	inspect func(*http.Request)
}

func (tr llmRequestContextTransport) RoundTrip(req *http.Request) (response *http.Response, err error) {
	tr.inspect(req)
	// Like the owned native transport, the consumer starts its span from the
	// actual HTTP request context. No span or parent is fabricated in this test.
	_, native := observability.Start(req.Context(), "window.native", "span")
	defer func() { native.End(err) }()
	return tr.base.RoundTrip(req)
}

func assertLLMRequestContext(t *testing.T, req *http.Request, tc llmRequestContextCase, deadline time.Time) {
	t.Helper()
	gotDeadline, ok := req.Context().Deadline()
	if !ok || !gotDeadline.Equal(deadline) || req.Context().Value(llmRequestContextKey{}) != "original-value" {
		t.Errorf("request lost original deadline/value: deadline=%v value=%v", gotDeadline, req.Context().Value(llmRequestContextKey{}))
	}
	wantHeaders := http.Header{"Content-Type": {"application/json"}}
	if tc.name == "openai" {
		wantHeaders.Set("Authorization", "Bearer synthetic-context-key")
	} else {
		wantHeaders.Set("x-api-key", "synthetic-context-key")
		wantHeaders.Set("anthropic-version", "2023-06-01")
	}
	if req.Method != http.MethodPost || req.URL.Path != tc.path || !reflect.DeepEqual(req.Header, wantHeaders) || req.ContentLength != int64(len(tc.body)) {
		t.Errorf("request method/path/headers/length changed: %s %s %v %d", req.Method, req.URL.Path, req.Header, req.ContentLength)
	}
	if req.GetBody == nil {
		t.Error("request lost original GetBody")
		return
	}
	body, err := req.GetBody()
	if err != nil {
		t.Error(err)
		return
	}
	raw, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || string(raw) != tc.body {
		t.Errorf("request replay body changed: %q read=%v close=%v", raw, readErr, closeErr)
	}
}

func assertLLMNativeParent(t *testing.T, capture *observedSpans) {
	t.Helper()
	capture.mu.Lock()
	defer capture.mu.Unlock()
	spans := map[string]*tracepb.Span{}
	for _, span := range capture.spans {
		if spans[span.Name] != nil {
			t.Fatalf("duplicate attempt/native/turn: %s", span.Name)
		}
		spans[span.Name] = span
	}
	turn, attempt, native := spans["agent.turn"], spans["llm.attempt"], spans["window.native"]
	if len(spans) != 3 || turn == nil || attempt == nil || native == nil {
		t.Fatalf("expected one turn, attempt and native span; got %v", spans)
	}
	if !bytes.Equal(attempt.ParentSpanId, turn.SpanId) || !bytes.Equal(native.ParentSpanId, attempt.SpanId) || !bytes.Equal(native.TraceId, attempt.TraceId) || !bytes.Equal(attempt.TraceId, turn.TraceId) {
		t.Errorf("native must be child of llm.attempt, not sibling: native.parent=%x attempt.id=%x attempt.parent=%x turn.id=%x", native.ParentSpanId, attempt.SpanId, attempt.ParentSpanId, turn.SpanId)
	}
}

// Removing request.WithContext after beginLLMAttempt makes the real transport's
// window.native span a sibling of llm.attempt while HTTP still succeeds.
func TestLLMRequestCarriesAttemptContextToHTTP(t *testing.T) {
	for _, tc := range llmRequestContextCases() {
		t.Run(tc.name, func(t *testing.T) {
			o, capture := localObserver(t)
			wire := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				raw, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
				}
				wire <- string(raw)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			parent, cancel := context.WithDeadline(context.WithValue(context.Background(), llmRequestContextKey{}, "original-value"), time.Now().Add(5*time.Second))
			defer cancel()
			deadline, _ := parent.Deadline()
			ctx, turn := o.StartTurn(parent, "model")
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			defer transport.CloseIdleConnections()
			client := &LLMClient{baseURL: server.URL, apiKey: "synthetic-context-key", model: "context-model", maxTokens: 128, cli: &http.Client{Transport: llmRequestContextTransport{base: transport, inspect: func(req *http.Request) { assertLLMRequestContext(t, req, tc, deadline) }}}}
			message, err := tc.call(ctx, client)
			if err != nil || message == nil || message.Content != "ok" {
				t.Fatalf("real HTTP response failed: message=%v error=%v", message, err)
			}
			if got := <-wire; got != tc.body {
				t.Errorf("wire body changed: %q", got)
			}
			turn.End(200, observability.Summary{})
			if err := o.ForceFlush(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertLLMNativeParent(t, capture)
		})
	}
}

func TestLLMRequestAttemptContextPreservesCancellation(t *testing.T) {
	for _, tc := range llmRequestContextCases() {
		for _, mode := range []string{"cancel", "deadline"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				o, capture := localObserver(t)
				started := make(chan struct{})
				serverCanceled := make(chan struct{})
				release := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					raw, err := io.ReadAll(req.Body)
					if err != nil || string(raw) != tc.body {
						t.Errorf("wire body changed: %q error=%v", raw, err)
					}
					close(started)
					select {
					case <-req.Context().Done():
						close(serverCanceled)
					case <-release:
					}
				}))
				defer server.Close()
				defer close(release)
				limit := 5 * time.Second
				if mode == "deadline" {
					limit = 500 * time.Millisecond
				}
				parent, cancel := context.WithDeadline(context.WithValue(context.Background(), llmRequestContextKey{}, "original-value"), time.Now().Add(limit))
				defer cancel()
				deadline, _ := parent.Deadline()
				ctx, turn := o.StartTurn(parent, "model")
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.Proxy = nil
				defer transport.CloseIdleConnections()
				client := &LLMClient{baseURL: server.URL, apiKey: "synthetic-context-key", model: "context-model", maxTokens: 128, cli: &http.Client{Transport: llmRequestContextTransport{base: transport, inspect: func(req *http.Request) { assertLLMRequestContext(t, req, tc, deadline) }}}}
				done := make(chan error, 1)
				go func() { _, err := tc.call(ctx, client); done <- err }()
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatal("local HTTP request never started")
				}
				if mode == "cancel" {
					cancel()
				}
				select {
				case err := <-done:
					want := "context canceled"
					if mode == "deadline" {
						want = "context deadline exceeded"
					}
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Errorf("original %s not propagated: %v", mode, err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal(fmt.Sprintf("HTTP request ignored original %s", mode))
				}
				select {
				case <-serverCanceled:
				case <-time.After(2 * time.Second):
					t.Error("original cancellation did not reach real HTTP server")
				}
				turn.End(500, observability.Summary{})
				if err := o.ForceFlush(context.Background()); err != nil {
					t.Fatal(err)
				}
				assertLLMNativeParent(t, capture)
			})
		}
	}
}
