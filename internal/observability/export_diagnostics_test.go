package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Decode JSON instead of depending on the new type, so the first RED is the
// missing diagnostic evidence rather than a compiler failure.
func diagnosticJSON(t *testing.T, o *Observer) map[string]any {
	t.Helper()
	raw, err := json.Marshal(o.Stats())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE-RESPONSE", "sk-private", "127.0.0.1", "Authorization"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("unsafe diagnostic leaked %s", secret)
		}
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExportDiagnosticsHTTPStatusAndLegacySuccessJSON(t *testing.T) {
	for _, fail := range []bool{false, true} {
		label := "success"
		if fail {
			label = "http_failure"
		}
		t.Run(label, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if fail {
					w.WriteHeader(503)
					_, _ = w.Write([]byte("PRIVATE-RESPONSE sk-private Authorization"))
					return
				}
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(200)
			}))
			defer server.Close()
			observer, err := New(context.Background(), enabled(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			ctx, turn := observer.StartTurn(context.Background(), "model")
			_, one := Start(ctx, "first", "tool")
			one.End(nil)
			_, two := Start(ctx, "second", "tool")
			two.End(nil)
			turn.End(200, Summary{})
			flushErr := observer.ForceFlush(context.Background())
			shutdownErr := observer.Shutdown(context.Background())
			if (flushErr != nil) != fail || (shutdownErr != nil) != fail || calls.Load() != 1 {
				t.Fatalf("behavior changed: %v %v calls%d", flushErr, shutdownErr, calls.Load())
			}
			data := diagnosticJSON(t, observer)
			if !fail {
				if _, ok := data["export_diagnostics"]; ok {
					t.Fatal("legacy success serialization changed")
				}
				return
			}
			d, ok := data["export_diagnostics"].(map[string]any)
			if !ok {
				t.Fatal("missing export failure diagnostics")
			}
			records := d["failures"].([]any)
			if len(records) != 1 || d["failed_batches"] != float64(1) || d["omitted_failures"] != float64(0) {
				t.Fatal("failure count mismatch", d)
			}
			first := records[0].(map[string]any)
			for k, v := range map[string]any{"batch_sequence": float64(1), "span_count": float64(3), "error_kind": "http_status", "http_status": float64(503), "http_round_trips": float64(1), "last_request_sequence": float64(1), "stage": "response_headers"} {
				if first[k] != v {
					t.Fatalf("diagnostic %s=%v want%v", k, first[k], v)
				}
			}
			if first["elapsed_ms"].(float64) < 0 || observer.Stats().Failed != 3 || observer.Stats().Exported != 0 {
				t.Fatal("diagnostic changed counters")
			}
		})
	}
}

func TestExportDiagnosticsDisabledJSONUnchanged(t *testing.T) {
	var observer *Observer
	raw, _ := json.Marshal(observer.Stats())
	want := `{"Accepted":0,"Dropped":0,"Exported":0,"Failed":0,"Batches":0,"Turns":0,"SampledTurns":0,"QueueDepth":0}`
	if string(raw) != want {
		t.Fatal("legacy disabled JSON changed", string(raw))
	}
}
