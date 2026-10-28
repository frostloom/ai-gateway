package jev

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type typedFailureTransport struct {
	err   error
	calls int
}

func (x *typedFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	x.calls++
	return nil, fmt.Errorf("private proxy credentials: %w", x.err)
}

// The same Evaluate failure path must preserve a finite typed cause without
// leaking wrapped text, retrying, or inferring whether the provider received it.
func TestTransportFailureTypedCauseIsRedactedAndNeverRetried(t *testing.T) {
	cases := []struct {
		name, want string
		err        error
	}{
		{"dns", "dns", &net.DNSError{Err: "private dns details", Name: "private.invalid"}},
		{"refused", "connection_refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
		{"reset", "connection_reset", syscall.ECONNRESET},
		{"pipe", "broken_pipe", syscall.EPIPE},
		{"eof", "eof", io.EOF}, {"short", "unexpected_eof", io.ErrUnexpectedEOF},
		{"cert", "tls_certificate", x509.UnknownAuthorityError{}},
		{"timeout", "timeout", context.DeadlineExceeded}, {"canceled", "canceled", context.Canceled},
		{"unknown", "unknown", errors.New("private raw URL/key")},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			up := &typedFailureTransport{err: tt.err}
			c, err := NewEvaluationClient("https://unused.invalid/v1/systemone", "private-key", "pinned", up)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Evaluate(context.Background(), "synthetic", map[string]Question{"a": {Type: QNoul}})
			d := FailureDetails(err)
			raw, _ := json.Marshal(d)
			var got map[string]any
			json.Unmarshal(raw, &got)
			if got["transport_cause"] != tt.want || up.calls != 1 || d.UpstreamAttempted != nil || strings.Contains(string(raw), "private") || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe/missing cause or retry: %s calls=%d", raw, up.calls)
			}
		})
	}
}

func TestTransportFailureLocalTLSAndEOF(t *testing.T) {
	for _, mode := range []string{"tls_certificate", "eof"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			if mode == "tls_certificate" {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			tr := &http.Transport{}
			defer tr.CloseIdleConnections()
			c, err := NewEvaluationClient(server.URL, "private-key", "pinned", tr)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Evaluate(context.Background(), "synthetic", map[string]Question{"a": {Type: QNoul}})
			raw, _ := json.Marshal(FailureDetails(err))
			var got map[string]any
			json.Unmarshal(raw, &got)
			if got["transport_cause"] != mode || FailureDetails(err).UpstreamAttempted != nil {
				t.Fatalf("local fault misclassified: %s", raw)
			}
			if mode == "eof" && calls.Load() != 1 {
				t.Fatalf("unexpected retry: %d", calls.Load())
			}
		})
	}
}

func TestTransportFailureLocalProxyRefusalAndDeadline(t *testing.T) {
	t.Run("proxy_refused", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		listener.Close()
		proxy, err := url.Parse("http://" + address)
		if err != nil {
			t.Fatal(err)
		}
		tr := &http.Transport{Proxy: http.ProxyURL(proxy)}
		defer tr.CloseIdleConnections()
		c, err := NewEvaluationClient("https://unused.invalid/v1/systemone", "private-key", "pinned", tr)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Evaluate(context.Background(), "local", map[string]Question{"a": {Type: QNoul}})
		raw, _ := json.Marshal(FailureDetails(err))
		var got map[string]any
		json.Unmarshal(raw, &got)
		if got["transport_cause"] != "connection_refused" || FailureDetails(err).UpstreamAttempted != nil {
			t.Fatalf("proxy error missing or charge guessed: %s", raw)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		release := make(chan struct{})
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
		defer s.Close()
		defer close(release)
		tr := &http.Transport{}
		defer tr.CloseIdleConnections()
		c, err := NewEvaluationClient(s.URL, "private-key", "pinned", tr)
		if err != nil {
			t.Fatal(err)
		}
		c.cli.Timeout = 50 * time.Millisecond
		_, err = c.Evaluate(context.Background(), "local", map[string]Question{"a": {Type: QNoul}})
		raw, _ := json.Marshal(FailureDetails(err))
		var got map[string]any
		json.Unmarshal(raw, &got)
		if got["transport_cause"] != "timeout" || FailureDetails(err).Kind != "timeout" || FailureDetails(err).UpstreamAttempted != nil {
			t.Fatalf("deadline mislabeled: %s", raw)
		}
	})
}
