package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// These literal-loopback receivers characterize ambiguous acknowledgement.
// Receiving the whole body is not proof of durable storage; no real trace is
// replayed and no historical transport cause is inferred from these scenarios.
func TestExportAcknowledgementDoesNotProveDeliveryLoss(t *testing.T) {
	for _, mode := range []string{"normal_ack", "response_timeout", "http_503", "connection_close", "malformed_protobuf"} {
		t.Run(mode, func(t *testing.T) {
			var requests, received atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("receiver body: %v", err)
					return
				}
				var body collector.ExportTraceServiceRequest
				if err := proto.Unmarshal(raw, &body); err != nil {
					t.Errorf("receiver protobuf: %v", err)
					return
				}
				for _, resource := range body.ResourceSpans {
					for _, scope := range resource.ScopeSpans {
						received.Add(int64(len(scope.Spans)))
					}
				}
				switch mode {
				case "response_timeout":
					// Wait on cancellation, not a guessed sleep. The entire request body has
					// already been received when the client's export deadline expires.
					<-r.Context().Done()
				case "http_503":
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("PRIVATE-RESPONSE-BODY"))
				case "connection_close":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Errorf("hijack: %v", err)
						return
					}
					_ = conn.Close()
				case "malformed_protobuf":
					w.Header().Set("Content-Type", "application/x-protobuf")
					_, _ = w.Write([]byte{0xff})
				default:
					w.Header().Set("Content-Type", "application/x-protobuf")
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()
			cfg := enabled(server.URL)
			cfg.ExportTimeout = time.Second
			observer, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, turn := observer.StartTurn(context.Background(), "model")
			_, one := Start(ctx, "synthetic.first", "tool")
			one.End(nil)
			_, two := Start(ctx, "synthetic.second", "tool")
			two.End(nil)
			turn.End(http.StatusOK, Summary{})
			flushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			flushErr := observer.ForceFlush(flushCtx)
			shutdownErr := observer.Shutdown(flushCtx)
			stats := observer.Stats()
			failed := mode != "normal_ack"
			if (flushErr != nil) != failed || (shutdownErr != nil) != failed {
				t.Fatalf("failure not retained: flush=%v shutdown=%v", flushErr, shutdownErr)
			}
			if flushErr != nil && flushErr.Error() != "telemetry export failed" {
				t.Fatal("raw export error escaped")
			}
			if requests.Load() != 1 || received.Load() != 3 || stats.Accepted != 3 || stats.Dropped != 0 || stats.QueueDepth != 0 || stats.Batches != 1 {
				t.Fatalf("request/count mismatch: requests=%d received=%d stats=%+v", requests.Load(), received.Load(), stats)
			}
			if failed && (stats.Failed != 3 || stats.Exported != 0) || !failed && (stats.Failed != 0 || stats.Exported != 3) {
				t.Fatalf("acknowledgement counters=%+v", stats)
			}
			record := map[string]any{"scenario": mode, "receiver_complete_body_spans": received.Load(), "requests": requests.Load(), "client_stats": stats, "flush_failed": flushErr != nil, "shutdown_failed": shutdownErr != nil, "historical_cause_proven": false, "model_calls": 0, "jev_calls": 0}
			encoded, _ := json.Marshal(record)
			t.Log(string(encoded))
		})
	}
}
