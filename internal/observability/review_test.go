package observability

import (
	"context"
	"errors"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"strings"
	"testing"
	"time"
)

func TestCredentialKeyVariantsAreMasked(t *testing.T) {
	o := &Observer{cfg: Config{HashKey: strings.Repeat("h", 32)}}
	for _, key := range []string{"apiKey", "apikey", "xApiKey", "API KEY", "api.key", "clientSecret", "accessToken", "set-cookie"} {
		got := o.safeValue(map[string]any{key: "opaque-credential", "amount": 42}, 0).(map[string]any)
		if got[key] != "[REDACTED]" {
			t.Errorf("credential key %q leaked: %v", key, got[key])
		}
		if got["amount"] != 42 {
			t.Fatal("ordinary argument changed")
		}
	}
}

type failingExporter struct{}

func (failingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errors.New("private backend failure")
}
func (failingExporter) Shutdown(context.Context) error { return nil }
func TestForceFlushReportsFullBatchFailures(t *testing.T) {
	// Queue and flush request are ready before starting the worker, ensuring an
	// exact full batch regardless of whether it selects queue or flush first.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &boundedProcessor{exporter: failingExporter{}, queue: make(chan sdktrace.ReadOnlySpan, 64), flush: make(chan flushRequest, 1), done: make(chan struct{}), ctx: ctx, cancel: cancel, timeout: time.Second}
	for i := 0; i < 64; i++ {
		p.queue <- nil
	}
	result := make(chan error, 1)
	p.flush <- flushRequest{done: result}
	go p.run()
	if err := <-result; err == nil {
		t.Error("flush reported success despite failed full batch")
	}
	if p.stats().Failed != 64 {
		t.Errorf("failed spans=%d", p.stats().Failed)
	}
	_ = p.Shutdown(context.Background())
}
