package agent

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/observability"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResidentKnowledgeIsExplicitAndHasDistinctRuntime(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	enableKnowledge(t, h.tb)
	old := *h.runtimeMetadata()
	if e := h.tb.SetKnowledgeStrategy("resident"); e != nil {
		t.Fatal(e)
	}
	now := h.runtimeMetadata()
	if now.KnowledgeStrategy != "resident" || now.CorpusVersion != old.CorpusVersion || now.PromptSHA256 == old.PromptSHA256 || now.ToolsSHA256 == old.ToolsSHA256 || now.ContractSHA256 == old.ContractSHA256 {
		t.Fatal("strategy identity missing")
	}
	b, _ := json.Marshal(h.tb.All())
	if strings.Contains(string(b), "search_platform_knowledge") || !strings.Contains(string(b), "get_balance") || !strings.Contains(h.basePrompt(), "[KB-012]") {
		t.Fatal("resident tools or context invalid")
	}
	if e := h.tb.SetKnowledgeStrategy("tool"); e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(h.runtimeMetadata())
	z, _ := json.Marshal(old)
	if string(a) != string(z) {
		t.Fatal("legacy tool runtime changed")
	}
}

func TestResidentActualRequestAndCaptureBoundary(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[capture], func(t *testing.T) {
			h, _ := isolatedObservedHandler(t)
			enableKnowledge(t, h.tb)
			if e := h.tb.SetKnowledgeStrategy("resident"); e != nil {
				t.Fatal(e)
			}
			o, spans := localObserver(t, capture)
			h.SetObserver(o)
			n := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				var req chatRequest
				json.NewDecoder(r.Body).Decode(&req)
				if !strings.Contains(req.Messages[0].Content, "[KB-012]") {
					t.Error("resident facts not actually sent")
				}
				for _, tool := range req.Tools {
					if tool.Function.Name == "search_platform_knowledge" {
						t.Error("extra search still advertised")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": contentMsg("SYNTHETIC [KB-006]")}}})
			}))
			defer srv.Close()
			h.llm = NewLLMClient(srv.URL, "unused", "test")
			ctx, tr := o.StartTurn(context.Background(), "model")
			ctx = context.WithValue(ctx, telemetryKey{}, &TurnTelemetry{})
			_, trace, e := h.llmLoop(ctx, "resident-local", 42, jevVerdict{}, &session{History: []Message{{Role: "user", Content: "退订规则"}}})
			if e != nil || len(trace) != 0 || n != 1 {
				t.Fatal("resident created fake tool call or extra request", e)
			}
			tr.End(200, observability.Summary{})
			if e = o.ForceFlush(context.Background()); e != nil {
				t.Fatal(e)
			}
			count := 0
			for _, s := range spans.spans {
				if s.Name == "knowledge.resident_context" {
					count++
					output := observedAttr(s, "langfuse.observation.output")
					if capture && output == "" {
						t.Fatal("missing captured body")
					}
					if !capture && output != "" {
						t.Fatal("capture off leaked body")
					}
					if observedAttr(s, "langfuse.observation.metadata.knowledge_source_sha256") == "" {
						t.Fatal("missing source SHA")
					}
				}
			}
			if count != 12 {
				t.Fatal("resident source inclusion not tracked", count)
			}
		})
	}
}
func TestResidentUserTextCannotInventInclusion(t *testing.T) {
	h, _ := isolatedObservedHandler(t)
	enableKnowledge(t, h.tb)
	h.tb.SetKnowledgeStrategy("resident")
	o, spans := localObserver(t, true)
	ctx, tr := o.StartTurn(context.Background(), "model")
	ctx = h.withResidentContext(ctx)
	observeResidentRequest(ctx, []Message{{Role: "system", Content: "other system"}, {Role: "user", Content: h.basePrompt()}}, 1)
	tr.End(200, observability.Summary{})
	o.ForceFlush(context.Background())
	for _, s := range spans.spans {
		if s.Name == "knowledge.resident_context" {
			t.Fatal("user KB text accepted as server resident source")
		}
	}
}
func TestResidentNilToolboxPreservesBaseContract(t *testing.T) {
	h := &Handler{}
	if h.basePrompt() != systemPrompt || h.effectiveContract() != runtimeContract {
		t.Fatal("nil toolbox defaults changed")
	}
	h.runtimeMetadata()
}
