package taskbench

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type windowDiagnosticBody struct {
	io.Reader
	readErr, closeErr error
}

func (b *windowDiagnosticBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.Reader.Read(p)
}
func (b *windowDiagnosticBody) Close() error { return b.closeErr }

func TestWindowTransportDiagnosticsSuccessfulAndFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"success", "read", "close", "eof", "tls", "dns", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			cap, _ := windowTransportFixture(t)
			dir := filepath.Join(t.TempDir(), "payloads")
			store, err := newWindowPayloadStore(dir, filepath.Join(t.TempDir(), "campaign"), cap.ledger.contract, "recorded_run")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			wrapped := &windowTransportPayloadCapability{windowTransportTestCapability: cap, store: store, required: true}
			calls := 0
			tr, err := newWindowProviderTransport(wrapped, "model", windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				switch mode {
				case "eof":
					return nil, io.EOF
				case "tls":
					httptrace.ContextClientTrace(r.Context()).TLSHandshakeStart()
					return nil, x509.UnknownAuthorityError{}
				case "dns":
					return nil, &net.DNSError{Name: "private.invalid", Err: "secret-error"}
				case "unknown":
					return nil, fmt.Errorf("Bearer private-credential")
				}
				response := windowTransportResponse("model")
				if mode == "read" {
					response.Body = &windowDiagnosticBody{readErr: io.ErrUnexpectedEOF}
				}
				if mode == "close" {
					response.Body = &windowDiagnosticBody{Reader: response.Body, closeErr: io.EOF}
				}
				return response, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tr.RoundTrip(windowTransportRequest(t, "model"))
			if calls != 1 {
				t.Fatal(calls)
			}
			m, _, auditErr := verifyWindowPayloadStore(store)
			if auditErr != nil {
				t.Fatal(auditErr)
			}
			response := m.Packets[0].Response
			if mode == "success" {
				if err != nil || response.TransportFailure != nil || cap.ledger.Snapshot().Pending != nil {
					t.Fatal("successful path changed", err, response)
				}
				raw, _ := os.ReadFile(filepath.Join(dir, "response-000001.json"))
				if strings.Contains(string(raw), "transport_failure") {
					t.Fatal("successful JSON changed")
				}
				return
			}
			if err == nil || response.TransportFailure == nil || cap.ledger.Snapshot().Pending == nil || cap.ledger.Snapshot().ModelHeld == 0 {
				t.Fatal("failure/hold missing", err, response)
			}
			want := map[string]string{"read": "unexpected_eof", "close": "eof", "eof": "eof", "tls": "tls_certificate", "dns": "dns", "unknown": "unknown"}[mode]
			if response.TransportFailure.Cause != want {
				t.Fatal("wrong typed cause", response.TransportFailure)
			}
			if mode == "read" && response.TransportFailure.Phase != "response_read" || mode == "close" && response.TransportFailure.Phase != "response_close" || mode == "tls" && response.TransportFailure.Phase != "tls_handshake" {
				t.Fatal("failure phase missing", response.TransportFailure)
			}
			response.TransportFailure.Cause = "mutated detached copy"
			again, _, e := verifyWindowPayloadStore(store)
			if e != nil || again.Packets[0].Response.TransportFailure.Cause != want {
				t.Fatal("detached metadata aliases store", e)
			}
		})
	}
}

func TestWindowTransportProbePreservesParentCallbacks(t *testing.T) {
	parentCalls := 0
	parent := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GetConn: func(string) { parentCalls++ }})
	ctx, p := newWindowTransportProbe(parent)
	httptrace.ContextClientTrace(ctx).GetConn("private.invalid")
	if parentCalls != 1 || p.snapshot(nil, "").Phase != "get_connection" {
		t.Fatal("existing trace callback replaced", parentCalls)
	}
}

func TestWindowTransportBoundAndCloseFailureRetainsOriginalPrefix(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	dir := filepath.Join(t.TempDir(), "payloads")
	store, err := newWindowPayloadStore(dir, filepath.Join(t.TempDir(), "campaign"), cap.ledger.contract, "recorded_run")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wrapped := &windowTransportPayloadCapability{windowTransportTestCapability: cap, store: store, required: true}
	body := strings.Repeat("x", windowMaxResponseBytes+1)
	calls := 0
	tr, err := newWindowProviderTransport(wrapped, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: &windowDiagnosticBody{Reader: strings.NewReader(body), closeErr: io.EOF}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tr.RoundTrip(windowTransportRequest(t, "model")); err == nil || calls != 1 {
		t.Fatal("combined failure escaped", err, calls)
	}
	manifest, _, err := verifyWindowPayloadStore(store)
	if err != nil {
		t.Fatal("diagnostic suppressed original over-bound evidence", err)
	}
	r := manifest.Packets[0].Response
	if r == nil || r.Code != "response_bound" || r.Complete || r.Body == nil || r.Body.Bytes != len(body) || r.Body.PrefixSHA256 != conversationHash([]byte(body)) || r.TransportFailure != nil {
		t.Fatal("original bounded prefix/code changed", r)
	}
	if s := cap.ledger.Snapshot(); s.Halt != "protocol_failure" || s.ModelHeld == 0 || s.ModelKnown != 0 {
		t.Fatal("unknown liability changed", s)
	}
}

func TestWindowTransportFailureRetainsBoundedCauseAndLastPhase(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	dir := filepath.Join(t.TempDir(), "payloads")
	store, err := newWindowPayloadStore(dir, filepath.Join(t.TempDir(), "campaign"), cap.ledger.contract, "recorded_run")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wrapped := &windowTransportPayloadCapability{windowTransportTestCapability: cap, store: store, required: true}
	calls := 0
	tr, err := newWindowProviderTransport(wrapped, "jev", windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if trace := httptrace.ContextClientTrace(r.Context()); trace != nil {
			trace.GetConn("secret-host.invalid:443")
			trace.GotConn(httptrace.GotConnInfo{Reused: true})
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		}
		return nil, fmt.Errorf("Bearer sk-sensitive-credential https://secret.invalid: %w", context.DeadlineExceeded)
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.RoundTrip(windowTransportRequest(t, "jev"))
	if err == nil || calls != 1 {
		t.Fatal("failure missing or retried", err, calls)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "response-000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var descriptor map[string]any
	if json.Unmarshal(raw, &descriptor) != nil {
		t.Fatal("invalid descriptor")
	}
	response := descriptor["response"].(map[string]any)
	diag, ok := response["transport_failure"].(map[string]any)
	if !ok || diag["cause"] != "timeout" || diag["phase"] != "await_response" || diag["trace_observed"] != true || diag["request_written"] != true || diag["connection_reused"] != true {
		t.Fatal("transport cause/phase lost", response)
	}
	for _, secret := range []string{"sensitive", "secret.invalid", "secret-host", "Bearer"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("diagnostic leaked raw transport data")
		}
	}
	s := cap.ledger.Snapshot()
	if s.Halt != "upstream_failure" || s.Pending == nil || s.JEVHeld == 0 || s.JEVKnown != 0 {
		t.Fatal("liability changed", s)
	}
	WindowJEVRejectFallback(t, cap)
	if _, err = tr.RoundTrip(windowTransportRequest(t, "jev")); err == nil || calls != 1 {
		t.Fatal("second upstream escaped", err, calls)
	}
}

func TestWindowTransportProbeDoesNotRegressOrShareSnapshots(t *testing.T) {
	ctx, p := newWindowTransportProbe(context.Background())
	trace := httptrace.ContextClientTrace(ctx)
	trace.WroteRequest(httptrace.WroteRequestInfo{})
	trace.GotFirstResponseByte()
	before := p.snapshot(context.DeadlineExceeded, "")
	// A losing parallel dial may finish after the winning connection wrote.
	trace.ConnectDone("tcp", "private.invalid:443", fmt.Errorf("late private error"))
	after := p.snapshot(context.DeadlineExceeded, "")
	if after.Phase != "read_response" || !after.FirstByte || !after.RequestWritten {
		t.Fatal("late callback regressed request", after)
	}
	before.Cause = "mutated"
	if p.snapshot(context.Canceled, "").Cause != "canceled" {
		t.Fatal("snapshot aliases probe")
	}
	ctx2, other := newWindowTransportProbe(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trace.GetConn("never retained")
			trace.GotFirstResponseByte()
			_ = p.snapshot(context.Canceled, "")
		}()
	}
	wg.Wait()
	if got := other.snapshot(nil, ""); got.TraceObserved || got.Phase != "round_trip" {
		t.Fatal("unrelated probe polluted", got)
	}
	if httptrace.ContextClientTrace(ctx2) == trace {
		t.Fatal("trace reused")
	}
}

func TestWindowPayloadRejectsFalseSuccessDiagnosticAndMalformedFields(t *testing.T) {
	for _, mode := range []string{"success", "invalid cause", "invalid phase", "negative time", "flags without trace"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := fixtureWindowPayloadStore(t, "local_stub")
			body := []byte("synthetic request")
			id := payloadRequest("model", body)
			if err := s.writeWindowRequest(1, id, body); err != nil {
				t.Fatal(err)
			}
			d := &windowTransportFailure{Cause: "timeout", Phase: "round_trip"}
			code := "round_trip"
			complete := false
			status := 0
			var response []byte
			switch mode {
			case "success":
				code = "round_trip"
				complete = true
				status = 200
				response = []byte("complete body")
			case "invalid cause":
				d.Cause = "secret-host.invalid"
			case "invalid phase":
				d.Phase = "Bearer sk-sensitive"
			case "negative time":
				d.ElapsedMS = -1
			case "flags without trace":
				d.RequestWritten = true
			}
			if err := s.writeWindowResponseWithFailure(1, id, status, response, complete, code, d); err == nil {
				t.Fatal("malformed/false success diagnostic accepted")
			}
			if !s.poisoned {
				t.Fatal("invalid diagnostic did not stop store")
			}
		})
	}
}
