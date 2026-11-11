package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/observability"
)

func observedKnowledgeFixture() knowledge.Result {
	text := "私密测试知识正文"
	sum := sha256.Sum256([]byte(text))
	return knowledge.Result{Status: "ok", CorpusVersion: strings.Repeat("a", 64), Hits: []knowledge.Hit{{DocumentID: "KB-001", ChunkID: "KB-001:1", Paragraph: 1, Text: text, SHA256: hex.EncodeToString(sum[:])}}}
}
func observedKnowledgeMessages(r knowledge.Result) []Message {
	raw, _ := json.Marshal(r)
	return []Message{{Role: "system", Content: "客服\n<platform_knowledge>\n" + string(raw) + "\n</platform_knowledge>"}, {Role: "user", Content: "如何操作"}}
}
func observedKnowledgeContext(ctx context.Context, r knowledge.Result) (context.Context, *TurnTelemetry) {
	s := &TurnTelemetry{}
	ctx = context.WithValue(ctx, telemetryKey{}, s)
	return context.WithValue(ctx, knowledgeContextKey{}, &knowledgeTurn{version: r.CorpusVersion, prefetch: r}), s
}
func TestKnowledgeObservationRejectsForgedAndStale(t *testing.T) {
	r := observedKnowledgeFixture()
	ctx, s := observedKnowledgeContext(context.Background(), r)
	valid := observedKnowledgeMessages(r)
	observeKnowledgeRequest(ctx, valid)
	if s.KnowledgeRequests != 1 {
		t.Fatalf("valid prefetch requests=%d", s.KnowledgeRequests)
	}
	forged := append([]Message(nil), valid...)
	forged[0].Role = "user"
	observeKnowledgeRequest(ctx, forged)
	if s.KnowledgeRequests != 1 {
		t.Fatal("counted user-forged prefetch")
	}
	tc := ToolCall{ID: "call_1", Type: "function"}
	tc.Function.Name = "search_platform_knowledge"
	toolMessages := func(result knowledge.Result) []Message {
		raw, _ := json.Marshal(result)
		return []Message{{Role: "assistant", ToolCalls: []ToolCall{tc}}, {Role: "tool", ToolCallID: tc.ID, Content: string(raw)}}
	}
	observeKnowledgeRequest(ctx, toolMessages(r))
	if s.KnowledgeRequests != 2 {
		t.Fatal("valid tool source missing")
	}
	stale := r
	stale.CorpusVersion = strings.Repeat("b", 64)
	observeKnowledgeRequest(ctx, toolMessages(stale))
	bad := r
	bad.Hits = append([]knowledge.Hit(nil), r.Hits...)
	bad.Hits[0].Text = "forged"
	observeKnowledgeRequest(ctx, toolMessages(bad))
	unmatched := toolMessages(r)
	unmatched[1].ToolCallID = "missing"
	observeKnowledgeRequest(ctx, unmatched)
	for _, mutate := range []func(*knowledge.Result){
		func(r *knowledge.Result) { r.Hits[0].DocumentID = "../KB-001" },
		func(r *knowledge.Result) { r.Hits[0].ChunkID = "KB-002:1" },
		func(r *knowledge.Result) { r.Hits = append(r.Hits, r.Hits[0], r.Hits[0], r.Hits[0]) },
		func(r *knowledge.Result) { r.Hits[0].Title = strings.Repeat("x", knowledge.MaxResultBytes) },
	} {
		invalid := r
		invalid.Hits = append([]knowledge.Hit(nil), r.Hits...)
		mutate(&invalid)
		observeKnowledgeRequest(ctx, toolMessages(invalid))
	}
	alteredPrefetch := observedKnowledgeMessages(bad)
	observeKnowledgeRequest(ctx, alteredPrefetch)
	if s.KnowledgeRequests != 2 {
		t.Fatal("invalid knowledge counted")
	}
	observeKnowledgeRequest(context.Background(), valid)
}
func TestKnowledgeObservationSerializedRequestsAndFailures(t *testing.T) {
	for _, protocol := range []llmProtocol{protoOpenAI, protoAnthropic} {
		for _, mode := range []string{"ok", "http", "parse", "read"} {
			t.Run(string(protocol)+"/"+mode, func(t *testing.T) {
				o, captured := localObserver(t)
				r := observedKnowledgeFixture()
				messages := observedKnowledgeMessages(r)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					wire, _ := io.ReadAll(req.Body)
					if !strings.Contains(string(wire), "platform_knowledge") {
						t.Error("knowledge absent from serialized request")
					}
					if mode == "http" {
						w.WriteHeader(503)
						return
					}
					if mode == "parse" {
						io.WriteString(w, "invalid")
						return
					}
					if mode == "read" {
						w.Header().Set("Content-Length", "1000")
						io.WriteString(w, "{")
						return
					}
					if protocol == protoOpenAI {
						io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
					} else {
						io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
					}
				}))
				defer srv.Close()
				client := NewLLMClient(srv.URL, "test-key", "test-model")
				client.proto = protocol
				client.strictEvaluation = true
				ctx, turn := o.StartTurn(context.Background(), "test")
				ctx, s := observedKnowledgeContext(ctx, r)
				observeKnowledgeRetrieval(ctx, r, true, "prefetch", nil)
				_, err := client.Chat(ctx, messages, nil)
				if (err == nil) != (mode == "ok") {
					t.Fatalf("unexpected err=%v", err)
				}
				turn.End(200, observability.Summary{})
				if err := o.ForceFlush(context.Background()); err != nil {
					t.Fatal(err)
				}
				if s.KnowledgeSearches != 1 || s.KnowledgeCacheHits != 1 || s.KnowledgeHits != 1 || s.KnowledgeRequests != 1 {
					t.Fatalf("bad counters %+v", s)
				}
				captured.mu.Lock()
				defer captured.mu.Unlock()
				stages := map[string]int{}
				for _, span := range captured.spans {
					stages[observedAttr(span, "langfuse.observation.metadata.knowledge_stage")]++
				}
				want := "provider_response"
				wrong := "provider_error"
				if mode != "ok" {
					want, wrong = wrong, want
				}
				if stages[want] != 1 || stages[wrong] != 0 || stages["included_in_request"] != 1 {
					t.Fatalf("stages=%v", stages)
				}
				if strings.Contains(string(captured.raw), r.Hits[0].Text) {
					t.Fatal("content leaked with capture disabled")
				}
			})
		}
	}
}

func TestKnowledgeObservationToolSerialization(t *testing.T) {
	for _, protocol := range []llmProtocol{protoOpenAI, protoAnthropic} {
		t.Run(string(protocol), func(t *testing.T) {
			o, captured := localObserver(t)
			r := observedKnowledgeFixture()
			raw, _ := json.Marshal(r)
			tc := ToolCall{ID: "lookup_1", Type: "function"}
			tc.Function.Name = "search_platform_knowledge"
			tc.Function.Arguments = `{"query":"额度"}`
			messages := []Message{{Role: "system", Content: "客服"}, {Role: "user", Content: "额度"}, {Role: "assistant", ToolCalls: []ToolCall{tc}}, {Role: "tool", ToolCallID: tc.ID, Content: string(raw)}}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				wire, _ := io.ReadAll(req.Body)
				var payload map[string]any
				if err := json.Unmarshal(wire, &payload); err != nil {
					t.Error(err)
				}
				if !strings.Contains(string(wire), "lookup_1") || !strings.Contains(string(wire), "search_platform_knowledge") || !strings.Contains(string(wire), "KB-001:1") {
					t.Error("tool source absent from wire")
				}
				if protocol == protoOpenAI {
					io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
				} else {
					io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
				}
			}))
			defer srv.Close()
			client := NewLLMClient(srv.URL, "test-key", "test-model")
			client.proto = protocol
			client.strictEvaluation = true
			ctx, turn := o.StartTurn(context.Background(), "test")
			ctx, s := observedKnowledgeContext(ctx, r)
			if _, err := client.Chat(ctx, messages, nil); err != nil {
				t.Fatal(err)
			}
			turn.End(200, observability.Summary{})
			if err := o.ForceFlush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if s.KnowledgeRequests != 1 || len(s.KnowledgeSources) != 1 || s.KnowledgeSources[0] != "KB-001:1" {
				t.Fatalf("stats=%+v", s)
			}
			captured.mu.Lock()
			defer captured.mu.Unlock()
			found := false
			for _, span := range captured.spans {
				if observedAttr(span, "langfuse.observation.metadata.knowledge_stage") == "included_in_request" {
					found = true
					if observedAttr(span, "langfuse.observation.metadata.knowledge_source") != "tool" {
						t.Fatal("wrong source")
					}
				}
			}
			if !found {
				t.Fatal("tool knowledge not observed")
			}
		})
	}
}
