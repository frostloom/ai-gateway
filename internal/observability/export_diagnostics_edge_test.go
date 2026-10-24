package observability

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/protobuf/proto"
)

type diagnosticTestExporter struct{ err error }

func (e *diagnosticTestExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return e.err
}
func (e *diagnosticTestExporter) Shutdown(context.Context) error { return nil }

func TestExportDiagnosticsBoundAndConcurrentSnapshots(t *testing.T) {
	p := newProcessor(&diagnosticTestExporter{err: errors.New("PRIVATE-RESPONSE sk-private")}, 1, time.Second)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = p.stats()
			}
		}
	}()
	for i := 0; i < 10; i++ {
		if p.export([]sdktrace.ReadOnlySpan{nil}) == nil {
			t.Fatal("export error lost")
		}
	}
	close(done)
	wg.Wait()
	if err := p.Shutdown(context.Background()); err == nil {
		t.Fatal("failure state cleared")
	}
	stats := p.stats()
	d := stats.ExportDiagnostics
	if d == nil || len(d.Failures) != 8 || d.FailedBatches != 10 || d.OmittedFailures != 2 || stats.Failed != 10 || stats.Batches != 10 {
		t.Fatalf("unbounded/missing diagnostic: %+v", d)
	}
	for i, record := range d.Failures {
		if record.BatchSequence != uint64(i+1) || record.ErrorKind != "unknown" || record.Stage != "export" || record.HTTPStatus != nil || record.HTTPRoundTrips != 0 {
			t.Fatalf("invented HTTP evidence: %+v", record)
		}
	}
	d.Failures[0].ErrorKind = "caller-mutated"
	if p.stats().ExportDiagnostics.Failures[0].ErrorKind != "unknown" {
		t.Fatal("snapshot aliases mutable internal state")
	}
}

func TestExportDiagnosticsStatusPointersCopied(t *testing.T) {
	var state exportDiagnosticState
	status := 503
	state.add(ExportFailureDiagnostic{HTTPStatus: &status})
	status = 200
	first := state.snapshot()
	*first.Failures[0].HTTPStatus = 401
	if *state.snapshot().Failures[0].HTTPStatus != 503 {
		t.Fatal("status pointer alias escaped")
	}
}

func TestExportDiagnosticsTypedCausesOnly(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{io.EOF, "eof"}, {io.ErrUnexpectedEOF, "unexpected_eof"}, {syscall.ECONNRESET, "connection_reset"},
		{syscall.ECONNREFUSED, "connection_refused"}, {net.ErrClosed, "connection_closed"},
		{&url.Error{Op: "POST", URL: "https://sk-private:password@private.invalid", Err: io.EOF}, "eof"},
		{x509.UnknownAuthorityError{}, "tls_certificate"}, {x509.HostnameError{}, "tls_certificate"},
		{x509.CertificateInvalidError{}, "tls_certificate"}, {tls.RecordHeaderError{}, "tls_record"},
		{&tls.RecordHeaderError{}, "tls_record"},
		{&net.DNSError{IsTimeout: true}, "network_timeout"}, {&net.DNSError{}, "network_error"},
		{errors.New("deadline exceeded EOF PRIVATE-RESPONSE sk-private"), "unknown"},
	}
	for _, tc := range tests {
		if got := exportTransportCause(tc.err); got != tc.want {
			t.Errorf("typed %T cause %q want%q", tc.err, got, tc.want)
		}
	}
}

func TestExportDiagnosticsSequencesAndFailureRemain(t *testing.T) {
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if failing.Load() {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	observer, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, turn := observer.StartTurn(context.Background(), "model")
		turn.End(200, Summary{})
		err = observer.ForceFlush(context.Background())
		if (err != nil) != (i == 1) {
			t.Fatal("flush changed", err)
		}
		failing.Store(true)
	}
	d := observer.Stats().ExportDiagnostics
	if d == nil || len(d.Failures) != 1 || d.Failures[0].BatchSequence != 2 || d.Failures[0].LastRequestSequence != 2 {
		t.Fatalf("sequence not bound to actual invocations: %+v", d)
	}
	if observer.Shutdown(context.Background()) == nil {
		t.Fatal("failure cleared at shutdown")
	}
}

func TestExportDiagnosticsTruncatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Length", "50")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()
	observer, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, turn := observer.StartTurn(context.Background(), "model")
	turn.End(200, Summary{})
	if observer.ForceFlush(context.Background()) == nil {
		t.Fatal("truncated response ignored")
	}
	_ = observer.Shutdown(context.Background())
	d := observer.Stats().ExportDiagnostics
	if d == nil || len(d.Failures) != 1 {
		t.Fatal("missing response diagnostic")
	}
	record := d.Failures[0]
	if record.ErrorKind != "response_read" || record.TransportCause != "unexpected_eof" || record.HTTPStatus == nil || *record.HTTPStatus != 200 {
		t.Fatalf("response read confused with HTTP success: %+v", record)
	}
}

type diagnosticTestBody struct{ reads, closes int }

func (b *diagnosticTestBody) Read(p []byte) (int, error) {
	b.reads++
	copy(p, []byte("xy"))
	return 2, io.ErrUnexpectedEOF
}
func (b *diagnosticTestBody) Close() error { b.closes++; return syscall.ECONNRESET }

type diagnosticTestTransport struct {
	body *diagnosticTestBody
	seen *http.Request
	idle int
}

func (t *diagnosticTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.seen = r
	return &http.Response{StatusCode: 200, Body: t.body}, nil
}
func (t *diagnosticTestTransport) CloseIdleConnections() { t.idle++ }
func TestExportDiagnosticsDelegatesBodyAndIdleClose(t *testing.T) {
	body := &diagnosticTestBody{}
	base := &diagnosticTestTransport{body: body}
	transport := &exportDiagnosticTransport{base: base}
	capture := &exportCapture{}
	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), exportCaptureKey{}, capture), "POST", "http://private.invalid", nil)
	req.Header.Set("Authorization", "sk-private")
	resp, err := transport.RoundTrip(req)
	if err != nil || base.seen != req || req.Header.Get("Authorization") != "sk-private" || body.reads != 0 || body.closes != 0 {
		t.Fatal("transport changed or consumed request/body")
	}
	buf := make([]byte, 2)
	n, err := resp.Body.Read(buf)
	if n != 2 || err != io.ErrUnexpectedEOF || string(buf) != "xy" {
		t.Fatal("body read changed")
	}
	if resp.Body.Close() != syscall.ECONNRESET || body.reads != 1 || body.closes != 1 {
		t.Fatal("body called twice or error changed")
	}
	transport.CloseIdleConnections()
	if base.idle != 1 {
		t.Fatal("idle connections leaked")
	}
	record := capture.failure(1, 1, 1, syscall.ECONNRESET)
	if record.Stage != "response_body" || record.TransportCause != "unexpected_eof" {
		t.Fatal("later close error overwrote first response failure")
	}
}

func TestExportDiagnosticsDoesNotInventResponseForPreHTTPProtoError(t *testing.T) {
	p := newProcessor(&diagnosticTestExporter{err: proto.Error}, 1, time.Second)
	if p.export([]sdktrace.ReadOnlySpan{nil}) == nil {
		t.Fatal("export failure ignored")
	}
	_ = p.Shutdown(context.Background())
	record := p.stats().ExportDiagnostics.Failures[0]
	if record.ErrorKind != "unknown" || record.Stage != "export" || record.HTTPRoundTrips != 0 || record.HTTPStatus != nil {
		t.Fatalf("invented response decode evidence before HTTP: %+v", record)
	}
}

func TestExportDiagnosticsCloseOnlyFailure(t *testing.T) {
	capture := &exportCapture{}
	capture.begin(1)
	capture.observe(1, 200, "response_headers", nil)
	body := &diagnosticTestBody{}
	wrapped := &exportDiagnosticBody{ReadCloser: body, capture: capture, sequence: 1}
	if wrapped.Close() != syscall.ECONNRESET || body.reads != 0 || body.closes != 1 {
		t.Fatal("close-only delegation changed")
	}
	record := capture.failure(1, 1, 1, syscall.ECONNRESET)
	if record.ErrorKind != "response_close" || record.Stage != "response_close" || record.TransportCause != "connection_reset" {
		t.Fatal("close-only evidence absent")
	}
}
