package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestJEVFailureTelemetryPreservesSafeCause(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
	defer s.Close()
	h := &Handler{}
	configureRouting(t, h, "jev_selective")
	h.SetJev(jev.NewClient(s.URL, "local", "pinned"))
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	v := h.evaluateTurn(ctx, "查询余额", nil)
	var raw map[string]any
	b, _ := json.Marshal(stats)
	json.Unmarshal(b, &raw)
	failures, ok := raw["jev_failures"].([]any)
	if v.Available || stats.JevErrors != 1 || !ok || len(failures) != 1 {
		t.Fatalf("missing failure telemetry: %s", b)
	}
	f := failures[0].(map[string]any)
	if f["kind"] != "http" || f["http_status"] != float64(429) || f["upstream_attempted"] != true || f["latency_ms"] == nil {
		t.Fatalf("lost safe cause: %v", f)
	}
}
