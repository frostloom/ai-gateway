package taskbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestPairJEVFailureSurvivesGuardAndResume(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(451)
		w.Write([]byte("sk-private-response"))
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "jev.jsonl")
	factory := func() jev.Evaluator { return jev.NewClient(s.URL, "local", "pinned") }
	b, err := openPairJEVGuard(path, "contract", 8, "pinned", factory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Evaluate(context.Background(), "local", map[string]jev.Question{"a": {Type: jev.QNoul}})
	d := jev.FailureDetails(err)
	if d.Kind != "http" || d.HTTPStatus != 451 {
		t.Errorf("guard lost cause: %+v", d)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	json.Unmarshal([]byte(lines[len(lines)-1]), &event)
	diagnostic, ok := event["failure"].(map[string]any)
	if !ok || diagnostic["http_status"] != float64(451) || diagnostic["kind"] != "http" || diagnostic["upstream_attempted"] != true || event["latency_ms"] == nil || strings.Contains(string(raw), "private") {
		t.Errorf("missing/unsafe persisted failure: %v", event)
	}
	b, err = openPairJEVGuard(path, "contract", 8, "pinned", factory)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	_, err = b.Evaluate(context.Background(), "local", nil)
	d = jev.FailureDetails(err)
	if d.Kind != "local_stop" || d.UpstreamAttempted == nil || *d.UpstreamAttempted || calls != 1 {
		t.Fatalf("resume repeated request or lost local stop: %v calls=%d", d, calls)
	}
}

// A transport cause must survive the durable budget guard and journal without
// converting an unknown-send failure to known-unbilled or triggering retries.
func TestPairJEVTransportCausePersistsWithoutRetry(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		c, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		c.Close()
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "jev.jsonl")
	guard, err := openPairJEVGuard(path, "contract", 1, "pinned", func() jev.Evaluator { return jev.NewClient(s.URL, "synthetic", "pinned") })
	if err != nil {
		t.Fatal(err)
	}
	_, err = guard.Evaluate(context.Background(), "local", map[string]jev.Question{"a": {Type: jev.QNoul}})
	d := jev.FailureDetails(err)
	if d.Kind != "transport" || d.UpstreamAttempted != nil {
		t.Fatalf("send/charge inference changed: %+v", d)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settle map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) != nil {
			t.Fatal("journal")
		}
		if e["Kind"] == "settle" {
			settle = e
		}
	}
	failure, ok := settle["failure"].(map[string]any)
	if !ok || failure["transport_cause"] != "eof" || settle["Failed"] != true || calls != 1 {
		t.Fatalf("cause lost or retry: %s calls=%d", raw, calls)
	}
	if _, known := failure["upstream_attempted"]; known {
		t.Fatal("unknown send state changed")
	}
	reopened, err := openPairJEVGuard(path, "contract", 1, "pinned", func() jev.Evaluator { return jev.NewClient(s.URL, "synthetic", "pinned") })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Evaluate(context.Background(), "local", map[string]jev.Question{"a": {Type: jev.QNoul}}); err == nil || calls != 1 {
		t.Fatal("failed attempt reservation reset after reopen")
	}
}
