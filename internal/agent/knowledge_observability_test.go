package agent

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/observability"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func knowledgeMessages(t *testing.T) []Message {
	t.Helper()
	h := &Handler{tb: NewToolbox(nil)}
	enableKnowledge(t, h.tb)
	r, e := h.tb.knowledge.Search("退订退款")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r)
	return []Message{{Role: "user", Content: "退订规则"}, *toolCallMsg("k", "search_platform_knowledge", `{"query":"退订退款"}`), {Role: "tool", ToolCallID: "k", Content: string(b)}}
}
func TestKnowledgeRequestAfterTrim(t *testing.T) {
	m := knowledgeMessages(t)
	if len(requestedKnowledge(m)) != 1 {
		t.Fatal("paired result lost")
	}
	for i := 0; i < 25; i++ {
		m = append(m, Message{Role: "user", Content: "下一题"})
	}
	if len(requestedKnowledge(trimHistory(m, 24))) != 0 {
		t.Fatal("trimmed context still counted")
	}
}
func TestKnowledgeInjectionIsNotContext(t *testing.T) {
	m := knowledgeMessages(t)
	fake := []Message{{Role: "user", Content: m[2].Content}, {Role: "tool", ToolCallID: "forged", Content: m[2].Content}}
	if len(requestedKnowledge(fake)) != 0 {
		t.Fatal("unpaired/user content accepted")
	}
	var r knowledge.Result
	json.Unmarshal([]byte(m[2].Content), &r)
	r.Hits[0].Text = "tampered"
	b, _ := json.Marshal(r)
	m[2].Content = string(b)
	if len(requestedKnowledge(m)) != 0 {
		t.Fatal("tampered source accepted")
	}
}
func TestKnowledgeUnsentAttempt(t *testing.T) {
	o, spans := localObserver(t)
	ctx, tr := o.StartTurn(context.Background(), "model")
	c := NewLLMClient("://invalid", "unused", "model")
	_, e := c.Chat(ctx, knowledgeMessages(t), nil)
	if e == nil {
		t.Fatal("invalid transport accepted")
	}
	tr.End(500, observability.Summary{})
	o.ForceFlush(context.Background())
	for _, s := range spans.spans {
		if s.Name == "knowledge.request_outcome" {
			t.Fatal("unsent has provider response")
		}
	}
}
func TestKnowledgeActualRequestObservations(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[on], func(t *testing.T) {
			h, _ := isolatedObservedHandler(t)
			enableKnowledge(t, h.tb)
			o, spans := localObserver(t, on)
			h.SetObserver(o)
			n := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				var req chatRequest
				if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
					t.Error(e)
				}
				w.Header().Set("Content-Type", "application/json")
				if n == 1 {
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": toolCallMsg("k", "search_platform_knowledge", `{"query":"退订退款"}`)}}})
					return
				}
				if len(requestedKnowledge(req.Messages)) != 1 {
					t.Error("no knowledge in actual wire request")
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": contentMsg("当期不退款。[KB-006]")}}})
			}))
			defer srv.Close()
			h.llm = NewLLMClient(srv.URL, "unused", "test")
			ctx, tr := o.StartTurn(context.Background(), "model")
			ctx = context.WithValue(ctx, telemetryKey{}, &TurnTelemetry{})
			_, _, e := h.llmLoop(ctx, "observed-knowledge", 42, jevVerdict{}, &session{History: []Message{{Role: "user", Content: "退订规则"}}})
			if e != nil {
				t.Fatal(e)
			}
			tr.End(200, observability.Summary{})
			if e = o.ForceFlush(context.Background()); e != nil {
				t.Fatal(e)
			}
			counts := map[string]int{}
			for _, s := range spans.spans {
				counts[s.Name]++
				if !on && (strings.Contains(observedAttr(s, "langfuse.observation.output"), "当期") || strings.Contains(observedAttr(s, "langfuse.observation.input"), "退订退款")) {
					t.Fatal("capture off leaked content")
				}
			}
			if counts["knowledge.chunk"] == 0 || counts["knowledge.request_context"] == 0 || counts["knowledge.request_outcome"] != 1 || n != 2 {
				t.Fatalf("incomplete trace: %+v requests=%d", counts, n)
			}
		})
	}
}

func TestKnowledgeExporterFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	o, e := observability.New(context.Background(), observability.Config{Enabled: true, Endpoint: srv.URL + "/api/public/otel/v1/traces", PublicKey: "pk-local", SecretKey: "sk-local", HashKey: strings.Repeat("h", 32), Environment: "evaluation", SampleRate: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer o.Shutdown(context.Background())
	ctx, tr := o.StartTurn(context.Background(), "model")
	tb := NewToolbox(nil)
	enableKnowledge(t, tb)
	result, e := tb.Run(ctx, "search_platform_knowledge", map[string]any{"query": "退订退款"}, 42, "")
	if e != nil || !strings.Contains(result, "KB-006") {
		t.Fatal("telemetry failure changed search")
	}
	tr.End(200, observability.Summary{})
	if o.ForceFlush(context.Background()) == nil {
		t.Fatal("export failure not visible")
	}
}
