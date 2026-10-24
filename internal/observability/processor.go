package observability

import (
	"context"
	"errors"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"sync"
	"sync/atomic"
	"time"
)

type Stats struct {
	Accepted, Dropped, Exported, Failed, Batches, Turns, SampledTurns uint64
	QueueDepth                                                        int
	ExportDiagnostics                                                 *ExportDiagnostics `json:"export_diagnostics,omitempty"`
}
type counters struct{ accepted, dropped, exported, failed, batches, turns, sampledTurns atomic.Uint64 }
type flushRequest struct{ done chan error }

// boundedProcessor accounts for local drops and exporter outcomes. Failed spans
// belong to calls that returned an error, not proven delivery loss. OnEnd never
// waits for the network; only this worker exports. No OTel globals are used.
type boundedProcessor struct {
	exporter    sdktrace.SpanExporter
	queue       chan sdktrace.ReadOnlySpan
	flush       chan flushRequest
	done        chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	timeout     time.Duration
	mu          sync.RWMutex
	closed      bool
	counts      counters
	lastErr     error // worker writes before closing done
	diagnostics exportDiagnosticState
}

func newProcessor(exp sdktrace.SpanExporter, size int, timeout time.Duration) *boundedProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	p := &boundedProcessor{exporter: exp, queue: make(chan sdktrace.ReadOnlySpan, size), flush: make(chan flushRequest), done: make(chan struct{}), ctx: ctx, cancel: cancel, timeout: timeout}
	go p.run()
	return p
}
func (p *boundedProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (p *boundedProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if !s.SpanContext().IsSampled() {
		return
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		p.counts.dropped.Add(1)
		return
	}
	select {
	case p.queue <- s:
		p.counts.accepted.Add(1)
	default:
		p.counts.dropped.Add(1)
	}
}
func (p *boundedProcessor) export(batch []sdktrace.ReadOnlySpan) error {
	if len(batch) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(p.ctx, p.timeout)
	defer cancel()
	sequence := p.counts.batches.Add(1)
	capture := &exportCapture{}
	ctx = context.WithValue(ctx, exportCaptureKey{}, capture)
	begun := time.Now()
	if err := p.exporter.ExportSpans(ctx, batch); err != nil {
		p.diagnostics.add(capture.failure(sequence, len(batch), float64(time.Since(begun))/float64(time.Millisecond), err))
		p.counts.failed.Add(uint64(len(batch)))
		p.lastErr = errors.New("telemetry export failed")
		return p.lastErr
	}
	p.counts.exported.Add(uint64(len(batch)))
	return nil
}
func (p *boundedProcessor) run() {
	defer close(p.done)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
		defer cancel()
		_ = p.exporter.Shutdown(ctx)
	}()
	timer := time.NewTicker(250 * time.Millisecond)
	defer timer.Stop()
	batch := make([]sdktrace.ReadOnlySpan, 0, 64)
	send := func() error { err := p.export(batch); batch = make([]sdktrace.ReadOnlySpan, 0, 64); return err }
	for {
		select {
		case <-p.ctx.Done():
			p.counts.dropped.Add(uint64(len(batch) + len(p.queue)))
			return
		case s, ok := <-p.queue:
			if !ok {
				_ = send()
				return
			}
			batch = append(batch, s)
			if len(batch) == 64 {
				_ = send()
			}
		case <-timer.C:
			_ = send()
		case req := <-p.flush:
			// Drain only the bounded queue snapshot; concurrent producers cannot keep
			// ForceFlush running forever.
			n := len(p.queue)
			for i := 0; i < n; i++ {
				s, ok := <-p.queue
				if !ok {
					break
				}
				batch = append(batch, s)
				if len(batch) == 64 {
					_ = send()
				}
			}
			_ = send()
			// Report any failed export since startup, including full/automatic batches.
			req.done <- p.lastErr
		}
	}
}
func (p *boundedProcessor) ForceFlush(ctx context.Context) error {
	request := flushRequest{done: make(chan error, 1)}
	select {
	case p.flush <- request:
	case <-p.done:
		return p.lastErr
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.done:
		return err
	case <-p.done:
		return p.lastErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *boundedProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()
	select {
	case <-p.done:
		return p.lastErr
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	}
}
func (p *boundedProcessor) stats() Stats {
	return Stats{Accepted: p.counts.accepted.Load(), Dropped: p.counts.dropped.Load(), Exported: p.counts.exported.Load(), Failed: p.counts.failed.Load(), Batches: p.counts.batches.Load(), Turns: p.counts.turns.Load(), SampledTurns: p.counts.sampledTurns.Load(), QueueDepth: len(p.queue), ExportDiagnostics: p.diagnostics.snapshot()}
}
