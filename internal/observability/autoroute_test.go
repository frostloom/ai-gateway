package observability

import (
	"context"
	"testing"
)

func TestRecordAutoRouteWritesBoundedDecisionSpan(t *testing.T) {
	server, capture := receiver(t)
	defer server.Close()
	o, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	o.RecordAutoRoute(context.Background(),
		[]map[string]string{{"role": "user", "text": "帮我修这段代码 sk-secret-123456"}},
		map[string]string{
			"request_id": "req_test", "mode": "on", "served_model": "deepseek-v4-pro",
			"tier": "strong", "score": "2.10", "fallback": "", "degraded": "",
			"breaker_state": "closed",
		})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.spans) != 1 {
		t.Fatalf("spans=%d", len(capture.spans))
	}
	span := capture.spans[0]
	if span.Name != "auto.route" {
		t.Fatalf("name=%s", span.Name)
	}
	a := attrs(span)
	if a["langfuse.observation.metadata.served_model"] != "deepseek-v4-pro" ||
		a["langfuse.observation.metadata.tier"] != "strong" ||
		a["langfuse.observation.metadata.request_id"] != "req_test" {
		t.Fatal("missing decision fields", a)
	}
}

func TestRecordAutoRouteNilObserverIsNoOp(t *testing.T) {
	var o *Observer
	o.RecordAutoRoute(context.Background(), nil, nil) // 不得 panic
}
