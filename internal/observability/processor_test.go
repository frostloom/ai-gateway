package observability

import (
	"context"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"sync"
	"testing"
	"time"
)

type gatedExporter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *gatedExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (e *gatedExporter) Shutdown(context.Context) error { return nil }
func TestQueueFullDropsWithoutBlockingRequests(t *testing.T) {
	e := &gatedExporter{started: make(chan struct{}), release: make(chan struct{})}
	p := newProcessor(e, 2, time.Second)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p))
	tracer := provider.Tracer("test")
	_, span := tracer.Start(context.Background(), "first")
	span.End()
	flushed := make(chan error, 1)
	go func() { flushed <- provider.ForceFlush(context.Background()) }()
	select {
	case <-e.started:
	case <-time.After(2 * time.Second):
		t.Fatal("export not started")
	}
	started := time.Now()
	for i := 0; i < 100; i++ {
		_, span := tracer.Start(context.Background(), "queued")
		span.End()
	}
	if time.Since(started) > 250*time.Millisecond {
		t.Fatal("full queue blocked producer")
	}
	if p.stats().Dropped == 0 || p.stats().QueueDepth > 2 {
		t.Fatalf("unbounded/lost drop accounting: %+v", p.stats())
	}
	close(e.release)
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats := p.stats()
	if stats.Exported+stats.Dropped != 101 || stats.Failed != 0 {
		t.Fatalf("lost spans: %+v", stats)
	}
}
func TestShutdownDeadlineCancelsBlockedExporter(t *testing.T) {
	e := &gatedExporter{started: make(chan struct{}), release: make(chan struct{})}
	p := newProcessor(e, 2, time.Second)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p))
	_, span := provider.Tracer("test").Start(context.Background(), "first")
	span.End()
	flushed := make(chan error, 1)
	go func() { flushed <- provider.ForceFlush(context.Background()) }()
	select {
	case <-e.started:
	case <-time.After(2 * time.Second):
		t.Fatal("export not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := provider.Shutdown(ctx); err == nil {
		t.Fatal("shutdown ignored deadline")
	}
	select {
	case <-p.done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("worker did not exit after cancellation")
	}
	<-flushed
	if p.stats().Failed != 1 {
		t.Fatalf("blocked export not accounted: %+v", p.stats())
	}
}
