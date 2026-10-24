package observability

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"

	"google.golang.org/protobuf/proto"
)

const maxExportFailures = 8

// ExportFailureDiagnostic contains allowlisted scalars only. A RoundTrip is an
// invocation of the HTTP transport, not proof of a wire attempt or delivery.
type ExportFailureDiagnostic struct {
	BatchSequence       uint64  `json:"batch_sequence"`
	SpanCount           int     `json:"span_count"`
	ElapsedMS           float64 `json:"elapsed_ms"`
	ErrorKind           string  `json:"error_kind"`
	HTTPRoundTrips      uint64  `json:"http_round_trips"`
	LastRequestSequence uint64  `json:"last_request_sequence,omitempty"`
	HTTPStatus          *int    `json:"http_status,omitempty"`
	Stage               string  `json:"stage"`
	TransportCause      string  `json:"transport_cause,omitempty"`
}

type ExportDiagnostics struct {
	FailedBatches   uint64                    `json:"failed_batches"`
	OmittedFailures uint64                    `json:"omitted_failures"`
	Failures        []ExportFailureDiagnostic `json:"failures"`
}

type exportDiagnosticState struct {
	mu      sync.Mutex
	failed  uint64
	records []ExportFailureDiagnostic
}

func cloneExportFailure(record ExportFailureDiagnostic) ExportFailureDiagnostic {
	if record.HTTPStatus != nil {
		status := *record.HTTPStatus
		record.HTTPStatus = &status
	}
	return record
}
func (s *exportDiagnosticState) add(record ExportFailureDiagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed++
	if len(s.records) < maxExportFailures {
		s.records = append(s.records, cloneExportFailure(record))
	}
}
func (s *exportDiagnosticState) snapshot() *ExportDiagnostics {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == 0 {
		return nil
	}
	result := &ExportDiagnostics{FailedBatches: s.failed, OmittedFailures: s.failed - uint64(len(s.records)), Failures: make([]ExportFailureDiagnostic, len(s.records))}
	for i, record := range s.records {
		result.Failures[i] = cloneExportFailure(record)
	}
	return result
}

type exportCaptureKey struct{}
type exportCapture struct {
	mu     sync.Mutex
	record ExportFailureDiagnostic
}

func (c *exportCapture) begin(sequence uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record.HTTPRoundTrips++
	c.record.LastRequestSequence = sequence
	c.record.HTTPStatus = nil
	c.record.Stage = "round_trip"
	c.record.TransportCause = ""
}
func (c *exportCapture) observe(sequence uint64, status int, stage string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A late callback from an earlier request cannot overwrite the latest one.
	if c.record.LastRequestSequence != sequence {
		return
	}
	// Preserve the first observed failure for this request; a later Close
	// failure must not replace an earlier response read failure.
	if c.record.TransportCause != "" {
		return
	}
	if status >= 100 && status <= 999 {
		v := status
		c.record.HTTPStatus = &v
	}
	c.record.Stage = stage
	if err != nil {
		c.record.TransportCause = exportTransportCause(err)
	}
}
func (c *exportCapture) failure(sequence uint64, spans int, elapsed float64, err error) ExportFailureDiagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()
	record := cloneExportFailure(c.record)
	record.BatchSequence = sequence
	record.SpanCount = spans
	record.ElapsedMS = elapsed
	if record.Stage == "" {
		record.Stage = "export"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		record.ErrorKind = "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		record.ErrorKind = "canceled"
	case record.Stage == "response_body":
		record.ErrorKind = "response_read"
	case record.Stage == "response_close":
		record.ErrorKind = "response_close"
	case record.HTTPStatus != nil && (*record.HTTPStatus < 200 || *record.HTTPStatus > 299):
		record.ErrorKind = "http_status"
	case record.HTTPRoundTrips > 0 && record.HTTPStatus != nil && *record.HTTPStatus >= 200 && *record.HTTPStatus <= 299 && errors.Is(err, proto.Error):
		record.ErrorKind = "response_decode"
		record.Stage = "response_decode"
	case record.Stage == "round_trip" && record.TransportCause != "":
		record.ErrorKind = "transport"
	default:
		record.ErrorKind = "unknown"
	}
	return record
}

// Never use Error(), addresses or response text to classify a transport cause.
func exportTransportCause(err error) string {
	var certificate x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var record tls.RecordHeaderError
	var recordPointer *tls.RecordHeaderError
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, net.ErrClosed):
		return "connection_closed"
	case errors.As(err, &certificate), errors.As(err, &invalid), errors.As(err, &hostname):
		return "tls_certificate"
	case errors.As(err, &record), errors.As(err, &recordPointer):
		return "tls_record"
	case errors.As(err, &network):
		if network.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	default:
		return "unknown"
	}
}

type exportDiagnosticTransport struct {
	base http.RoundTripper
	next atomic.Uint64
}

func (t *exportDiagnosticTransport) underlying() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}
func (t *exportDiagnosticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	sequence := t.next.Add(1)
	capture, _ := req.Context().Value(exportCaptureKey{}).(*exportCapture)
	if capture != nil {
		capture.begin(sequence)
	}
	response, err := t.underlying().RoundTrip(req)
	if capture != nil {
		if err != nil {
			capture.observe(sequence, 0, "round_trip", err)
		} else if response != nil {
			capture.observe(sequence, response.StatusCode, "response_headers", nil)
			if response.Body != nil {
				response.Body = &exportDiagnosticBody{ReadCloser: response.Body, capture: capture, sequence: sequence}
			}
		}
	}
	return response, err
}
func (t *exportDiagnosticTransport) CloseIdleConnections() {
	if closer, ok := t.underlying().(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type exportDiagnosticBody struct {
	io.ReadCloser
	capture  *exportCapture
	sequence uint64
}

func (b *exportDiagnosticBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		b.capture.observe(b.sequence, 0, "response_body", err)
	}
	return n, err
}
func (b *exportDiagnosticBody) Close() error {
	err := b.ReadCloser.Close()
	if err != nil {
		b.capture.observe(b.sequence, 0, "response_close", err)
	}
	return err
}
