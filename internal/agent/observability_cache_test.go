package agent

import (
	"context"
	"github.com/frostloom/ai-gateway/internal/observability"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObservedStandardOpenAICacheUsage(t *testing.T) {
	o, c := localObserver(t)
	stats := &TurnTelemetry{}
	ctx, turn := o.StartTurn(context.WithValue(context.Background(), telemetryKey{}, stats), "model")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":80}}}`))
	}))
	defer server.Close()
	_, err := NewLLMClient(server.URL, "local-only", "local-model").Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	turn.End(200, observability.Summary{})
	if err := o.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stats.LLMCacheHitTokens != 80 {
		t.Errorf("cache hit=%d", stats.LLMCacheHitTokens)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, span := range c.spans {
		if span.Name == "llm.attempt" && observedAttr(span, "langfuse.observation.usage_details") != `{"cache_read_input_tokens":80,"input":20,"output":2}` {
			t.Errorf("usage=%s", observedAttr(span, "langfuse.observation.usage_details"))
		}
	}
}
