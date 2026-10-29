package taskbench

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"sync"
	"testing"
	"time"
)

type tlsWitnessTimeoutEOF struct{}

func (tlsWitnessTimeoutEOF) Error() string   { return "timeout" }
func (tlsWitnessTimeoutEOF) Unwrap() error   { return io.EOF }
func (tlsWitnessTimeoutEOF) Timeout() bool   { return true }
func (tlsWitnessTimeoutEOF) Temporary() bool { return false }

type tlsWitnessClaimsEOF struct{}

func (tlsWitnessClaimsEOF) Error() string        { return "opaque" }
func (tlsWitnessClaimsEOF) Is(target error) bool { return target == io.EOF }

type tlsWitnessCyclicError struct{}

func (tlsWitnessCyclicError) Error() string   { return "cycle" }
func (e tlsWitnessCyclicError) Unwrap() error { return e }

func tlsWitnessEOF(trace *httptrace.ClientTrace) {
	trace.TLSHandshakeStart()
	trace.TLSHandshakeDone(tls.ConnectionState{}, io.EOF)
}

// These cases catch relaxed predicates: EOF alone, successful/partial I/O,
// incomplete handshakes and ambiguous error trees must never qualify.
func TestWindowTLSRetryWitnessEligibility(t *testing.T) {
	cases := []struct {
		name     string
		events   func(*httptrace.ClientTrace)
		err      error
		response *http.Response
		want     bool
	}{
		{"complete_eof", tlsWitnessEOF, io.EOF, nil, true},
		{"wrapped_eof", func(tr *httptrace.ClientTrace) {
			tr.TLSHandshakeStart()
			tr.TLSHandshakeDone(tls.ConnectionState{}, fmt.Errorf("tls: %w", io.EOF))
		}, fmt.Errorf("round trip: %w", io.EOF), nil, true},
		{"bare_eof", func(*httptrace.ClientTrace) {}, io.EOF, nil, false},
		{"unexpected_eof", tlsWitnessEOF, io.ErrUnexpectedEOF, nil, false},
		{"nil_error", tlsWitnessEOF, nil, nil, false},
		{"typed_timeout_wrapping_eof", tlsWitnessEOF, tlsWitnessTimeoutEOF{}, nil, false},
		{"opaque_eof_claim", tlsWitnessEOF, tlsWitnessClaimsEOF{}, nil, false},
		{"cyclic_error", tlsWitnessEOF, tlsWitnessCyclicError{}, nil, false},
		{"response", tlsWitnessEOF, io.EOF, &http.Response{StatusCode: 200}, false},
		{"successful_tls", func(tr *httptrace.ClientTrace) {
			tr.TLSHandshakeStart()
			tr.TLSHandshakeDone(tls.ConnectionState{}, nil)
		}, io.EOF, nil, false},
		{"duplicate_start", func(tr *httptrace.ClientTrace) { tr.TLSHandshakeStart(); tlsWitnessEOF(tr) }, io.EOF, nil, false},
		{"duplicate_done", func(tr *httptrace.ClientTrace) { tlsWitnessEOF(tr); tr.TLSHandshakeDone(tls.ConnectionState{}, io.EOF) }, io.EOF, nil, false},
		{"unfinished", func(tr *httptrace.ClientTrace) { tr.TLSHandshakeStart() }, io.EOF, nil, false},
		{"missing_start", func(tr *httptrace.ClientTrace) { tr.TLSHandshakeDone(tls.ConnectionState{}, io.EOF) }, io.EOF, nil, false},
		{"reversed", func(tr *httptrace.ClientTrace) {
			tr.TLSHandshakeDone(tls.ConnectionState{}, io.EOF)
			tr.TLSHandshakeStart()
		}, io.EOF, nil, false},
		{"tls_wrong_error", func(tr *httptrace.ClientTrace) {
			tr.TLSHandshakeStart()
			tr.TLSHandshakeDone(tls.ConnectionState{}, io.ErrUnexpectedEOF)
		}, io.EOF, nil, false},
		{"joined_unknown", tlsWitnessEOF, errors.Join(io.EOF, errors.New("other")), nil, false},
		{"joined_timeout", tlsWitnessEOF, errors.Join(io.EOF, context.DeadlineExceeded), nil, false},
		{"joined_certificate", tlsWitnessEOF, errors.Join(io.EOF, x509.UnknownAuthorityError{}), nil, false},
		{"tls_joined_unknown", func(tr *httptrace.ClientTrace) {
			tr.TLSHandshakeStart()
			tr.TLSHandshakeDone(tls.ConnectionState{}, errors.Join(io.EOF, errors.New("other")))
		}, io.EOF, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
			tc.events(httptrace.ContextClientTrace(ctx))
			if w.eligible() {
				t.Fatal("unsealed witness eligible")
			}
			w.seal(tc.response, tc.err)
			if got := w.eligible(); got != tc.want {
				t.Fatalf("eligible = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWindowTLSRetryWitnessRejectsIO(t *testing.T) {
	cases := []struct {
		name  string
		event func(*httptrace.ClientTrace)
	}{
		{"got_conn", func(tr *httptrace.ClientTrace) { tr.GotConn(httptrace.GotConnInfo{}) }},
		{"reused_conn", func(tr *httptrace.ClientTrace) { tr.GotConn(httptrace.GotConnInfo{Reused: true}) }},
		{"headers", func(tr *httptrace.ClientTrace) { tr.WroteHeaders() }},
		{"wait_continue", func(tr *httptrace.ClientTrace) { tr.Wait100Continue() }},
		{"header_field", func(tr *httptrace.ClientTrace) { tr.WroteHeaderField("x", []string{"y"}) }},
		{"request", func(tr *httptrace.ClientTrace) { tr.WroteRequest(httptrace.WroteRequestInfo{}) }},
		{"partial_request", func(tr *httptrace.ClientTrace) { tr.WroteRequest(httptrace.WroteRequestInfo{Err: io.EOF}) }},
		{"first_byte", func(tr *httptrace.ClientTrace) { tr.GotFirstResponseByte() }},
		{"continue", func(tr *httptrace.ClientTrace) { tr.Got100Continue() }},
		{"other_1xx", func(tr *httptrace.ClientTrace) { _ = tr.Got1xxResponse(103, textproto.MIMEHeader{}) }},
		{"idle_conn", func(tr *httptrace.ClientTrace) { tr.PutIdleConn(nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
			tr := httptrace.ContextClientTrace(ctx)
			tlsWitnessEOF(tr)
			tc.event(tr)
			w.seal(nil, io.EOF)
			if w.eligible() {
				t.Fatal("I/O observation accepted")
			}
		})
	}
}

func TestWindowTLSRetryWitnessContextLiveness(t *testing.T) {
	for _, name := range []string{"request_before", "parent_before", "request_after", "parent_after", "request_deadline", "parent_deadline"} {
		t.Run(name, func(t *testing.T) {
			req, cancelReq := context.WithCancel(context.Background())
			defer cancelReq()
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			if name == "request_deadline" {
				var cancel context.CancelFunc
				req, cancel = context.WithDeadline(req, time.Unix(1, 0))
				defer cancel()
			}
			if name == "parent_deadline" {
				var cancel context.CancelFunc
				parent, cancel = context.WithDeadline(parent, time.Unix(1, 0))
				defer cancel()
			}
			ctx, w := newWindowTLSRetryWitness(req, parent)
			tlsWitnessEOF(httptrace.ContextClientTrace(ctx))
			if name == "request_before" {
				cancelReq()
			}
			if name == "parent_before" {
				cancelParent()
			}
			w.seal(nil, io.EOF)
			if name == "request_after" {
				cancelReq()
			}
			if name == "parent_after" {
				cancelParent()
			}
			if w.eligible() {
				t.Fatal("cancelled or expired context accepted")
			}
		})
	}
}

func TestWindowTLSRetryWitnessSealIsFinal(t *testing.T) {
	ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
	tlsWitnessEOF(httptrace.ContextClientTrace(ctx))
	w.seal(nil, io.EOF)
	before := w.snapshot()
	if !w.eligible() {
		t.Fatal("valid witness rejected")
	}
	w.seal(nil, io.EOF)
	if w.eligible() {
		t.Fatal("repeated seal accepted")
	}
	if before.SealCalls != 1 || before.TLSStarts != 1 || before.TLSDone != 1 {
		t.Fatalf("snapshot changed or incomplete: %+v", before)
	}
	// Mutating a diagnostic copy cannot revive or poison the live witness.
	copy := w.snapshot()
	copy.SealCalls = 1
	copy.TLSStarts = 99
	if w.snapshot().TLSStarts != 1 || w.eligible() {
		t.Fatal("snapshot is live authority")
	}
}

func TestWindowTLSRetryWitnessLateEvents(t *testing.T) {
	events := []struct {
		name  string
		event func(*httptrace.ClientTrace)
	}{
		{"tls", func(tr *httptrace.ClientTrace) { tr.TLSHandshakeStart() }},
		{"get_conn", func(tr *httptrace.ClientTrace) { tr.GetConn("unused") }},
		{"dns", func(tr *httptrace.ClientTrace) { tr.DNSStart(httptrace.DNSStartInfo{}) }},
		{"connect", func(tr *httptrace.ClientTrace) { tr.ConnectDone("", "", nil) }},
	}
	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
			tr := httptrace.ContextClientTrace(ctx)
			tlsWitnessEOF(tr)
			w.seal(nil, io.EOF)
			old := w.snapshot()
			tc.event(tr)
			if w.eligible() || w.snapshot().LateEvents != 1 || old.LateEvents != 0 {
				t.Fatal("late callback did not invalidate live witness independently of snapshot")
			}
		})
	}
}

func TestWindowTLSRetryWitnessConcurrency(t *testing.T) {
	ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
	tr := httptrace.ContextClientTrace(ctx)
	tlsWitnessEOF(tr)
	w.seal(nil, io.EOF)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tr.GotConn(httptrace.GotConnInfo{}); _ = w.snapshot(); _ = w.eligible() }()
	}
	wg.Wait()
	if w.eligible() || w.snapshot().LateEvents != 32 {
		t.Fatalf("lost concurrent events: %+v", w.snapshot())
	}
}

func TestWindowTLSRetryWitnessPreservesRequestContext(t *testing.T) {
	type key struct{}
	req, cancel := context.WithTimeout(context.WithValue(context.Background(), key{}, "value"), time.Minute)
	defer cancel()
	previous := 0
	req = httptrace.WithClientTrace(req, &httptrace.ClientTrace{TLSHandshakeStart: func() { previous++ }})
	ctx, w := newWindowTLSRetryWitness(req, context.Background())
	tr := httptrace.ContextClientTrace(ctx)
	tlsWitnessEOF(tr)
	w.seal(nil, io.EOF)
	d1, ok1 := req.Deadline()
	d2, ok2 := ctx.Deadline()
	if !w.eligible() || ctx.Value(key{}) != "value" || ok1 != ok2 || !d1.Equal(d2) || previous != 1 {
		t.Fatal("request context or existing trace changed")
	}
}

func TestWindowTLSRetryWitnessContradictoryTLSState(t *testing.T) {
	ctx, w := newWindowTLSRetryWitness(context.Background(), context.Background())
	tr := httptrace.ContextClientTrace(ctx)
	tr.TLSHandshakeStart()
	tr.TLSHandshakeDone(tls.ConnectionState{HandshakeComplete: true}, io.EOF)
	w.seal(nil, io.EOF)
	if w.eligible() {
		t.Fatal("completed TLS state with EOF accepted")
	}
}

func TestWindowTLSRetryWitnessMissingContexts(t *testing.T) {
	for _, pair := range [][2]context.Context{{nil, context.Background()}, {context.Background(), nil}, {nil, nil}} {
		ctx, w := newWindowTLSRetryWitness(pair[0], pair[1])
		if w != nil || ctx != pair[0] {
			t.Fatal("missing context must reject construction without replacement")
		}
	}
}
