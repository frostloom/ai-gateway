package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func routerReportMetric(t *testing.T, result string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "gateway_router_report_total" {
			continue
		}
		for _, m := range f.Metric {
			for _, l := range m.Label {
				if l.GetName() == "result" && l.GetValue() == result {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestRouterReportRecordsTransportAndHTTPOutcome(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		canceled bool
		outcome  string
	}{
		{"success", 200, false, "http_200"},
		{"unavailable", 503, false, "http_error"},
		{"canceled", 200, true, "transport_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
			defer server.Close()
			before := routerReportMetric(t, tc.outcome)
			ctx := context.Background()
			if tc.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			NewRouter(server.URL, "test").Report(ctx, 1, false)
			after := routerReportMetric(t, tc.outcome)
			if after-before != 1 {
				t.Fatalf("outcome %s before=%v after=%v", tc.outcome, before, after)
			}
		})
	}
}

type reportRoundTripFunc func(*http.Request) (*http.Response, error)

func (f reportRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type reportBrokenBody struct{}

func (reportBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (reportBrokenBody) Close() error             { return nil }

func TestRouterReportRecordsMalformedRequestAndBodyFailure(t *testing.T) {
	before := routerReportMetric(t, "invalid_request")
	NewRouter("://invalid", "test").Report(context.Background(), 1, false)
	if after := routerReportMetric(t, "invalid_request"); after-before != 1 {
		t.Fatalf("invalid request before=%v after=%v", before, after)
	}
	before = routerReportMetric(t, "body_error")
	r := NewRouter("http://example.invalid", "test")
	r.cli.Transport = reportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: reportBrokenBody{}, Header: make(http.Header)}, nil
	})
	r.Report(context.Background(), 1, false)
	if after := routerReportMetric(t, "body_error"); after-before != 1 {
		t.Fatalf("body error before=%v after=%v", before, after)
	}
}
