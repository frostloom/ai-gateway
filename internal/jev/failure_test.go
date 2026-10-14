package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailureHTTPDiagnosticsNeverExposeBody(t *testing.T) {
	for _, code := range []int{401, 429, 451, 500} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			w.Write([]byte("echoed sk-sensitive-private-credential account details"))
		}))
		_, err := NewClient(s.URL, "local", "pinned").Evaluate(context.Background(), "local", map[string]Question{"a": {Type: QNoul}})
		s.Close()
		var raw map[string]any
		b, _ := json.Marshal(FailureDetails(err))
		json.Unmarshal(b, &raw)
		if raw["kind"] != "http" || raw["http_status"] != float64(code) || raw["upstream_attempted"] != true || strings.Contains(err.Error(), "sensitive") || strings.Contains(string(b), "account") {
			t.Fatalf("unsafe/missing diagnostics: %s %v", b, err)
		}
	}
}

func TestFailureDecodeAndLocalRejection(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("sk-private-invalid-json")) }))
	defer s.Close()
	_, err := NewClient(s.URL, "local", "pinned").Evaluate(context.Background(), "local", map[string]Question{"a": {Type: QNoul}})
	d := FailureDetails(err)
	if d.Kind != "decode" || d.HTTPStatus != 200 || strings.Contains(err.Error(), "private") {
		t.Fatal("invalid decode failure", d, err)
	}
	_, err = NewClient(s.URL, "", "pinned").Evaluate(context.Background(), "local", nil)
	d = FailureDetails(err)
	if d.Kind != "configuration" || d.UpstreamAttempted == nil || *d.UpstreamAttempted {
		t.Fatal("missing key treated as upstream", d)
	}
	if FailureDetails(errors.New("secret raw error")).Kind != "unknown" {
		t.Fatal("untyped error was guessed")
	}
}

func TestFailureCanceledBeforeHTTPIsLocal(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(s.URL, "local", "pinned").Evaluate(ctx, "local", map[string]Question{"a": {Type: QNoul}})
	d := FailureDetails(err)
	if calls != 0 || d.Kind != "canceled" || d.UpstreamAttempted == nil || *d.UpstreamAttempted {
		t.Fatalf("local cancellation counted upstream: %+v calls=%d", d, calls)
	}
	_, err = NewClient("ftp://not-an-http-provider", "local", "pinned").Evaluate(context.Background(), "local", map[string]Question{"a": {Type: QNoul}})
	d = FailureDetails(err)
	if d.Kind != "configuration" || d.UpstreamAttempted == nil || *d.UpstreamAttempted {
		t.Fatalf("local URL error counted upstream: %+v", d)
	}
}

type diagnosticErrorTransport struct{}

func (diagnosticErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("secret transport URL/key")
}

func TestFailureTransportDoesNotGuessSentRequest(t *testing.T) {
	c := NewClient("https://unused.invalid", "local", "pinned")
	c.cli = &http.Client{Transport: diagnosticErrorTransport{}}
	_, err := c.Evaluate(context.Background(), "local", map[string]Question{"a": {Type: QNoul}})
	d := FailureDetails(err)
	if d.Kind != "transport" || d.UpstreamAttempted != nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unknown transport guessed or leaked: %+v %v", d, err)
	}
}
