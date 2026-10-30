package taskbench

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Native observations are metadata only regardless of content capture. Raw
// request/response packets remain exact private evidence, and existing logical
// and tool operations retain their own established content/redaction behavior.
func TestWindowV8NativeObserverMetadataOnly(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		for _, capture := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/capture_%t", provider, capture), func(t *testing.T) {
				c, r := fixtureWindowV8Campaign(t, provider, false)
				secrets := map[string]string{"password": "fixturePasswordAlpha42", "cookie": "fixtureCookieBravo43", "refresh_token": "fixtureRefreshCharlie44", "session_id": "fixtureSessionDelta45"}
				encoded, _ := json.Marshal(secrets)
				// An encoded JSON string in tool arguments/state contains escaped sensitive
				// keys. Regex replacement on the whole outer JSON string cannot redact it.
				requestObject := map[string]any{}
				baseRequest := windowProtocolModelBody
				if provider == "jev" {
					baseRequest = windowProtocolJEVBody
				}
				if err := json.Unmarshal([]byte(baseRequest), &requestObject); err != nil {
					t.Fatal(err)
				}
				responseObject := map[string]any{"model": r.request.Model, "usage": map[string]int{"input_tokens": 2, "output_tokens": 1}}
				if provider == "model" {
					requestObject["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": string(encoded)}}}}
					requestObject["tools"] = []any{map[string]any{"name": "fixture_read", "description": "synthetic fixture", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"arguments_json": map[string]string{"type": "string"}}, "required": []string{"arguments_json"}}}}
					responseObject["content"] = []any{map[string]any{"type": "tool_use", "id": "fixture-tool", "name": "fixture_read", "input": map[string]any{"arguments_json": string(encoded)}}}
					responseObject["stop_reason"] = "tool_use"
				} else {
					requestObject["state"] = map[string]any{"arguments_json": string(encoded)}
					requestObject["questions"] = map[string]any{"q": map[string]any{"type": "score", "instructions": "synthetic fixture", "criteria": []string{string(encoded)}}}
					responseObject["answers"] = map[string]any{"q": map[string]any{"type": "score", "score": 0, "confidence": .99, "legend": map[string]string{"0": string(encoded)}}}
				}
				requestRaw, _ := json.Marshal(requestObject)
				responseRaw, _ := json.Marshal(responseObject)
				var mu sync.Mutex
				var spans []*tracepb.Span
				receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					raw, _ := io.ReadAll(req.Body)
					var export collectortrace.ExportTraceServiceRequest
					if err := proto.Unmarshal(raw, &export); err != nil {
						t.Error(err)
					}
					mu.Lock()
					for _, rs := range export.ResourceSpans {
						for _, ss := range rs.ScopeSpans {
							spans = append(spans, ss.Spans...)
						}
					}
					mu.Unlock()
					w.Header().Set("Content-Type", "application/x-protobuf")
					w.WriteHeader(200)
				}))
				defer receiver.Close()
				observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: capture, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "synthetic-pk", SecretKey: "synthetic-sk", HashKey: strings.Repeat("h", 32), Release: c.prepared.scope.Bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 32, ExportTimeout: 10 * time.Second})
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Shutdown(context.Background())
				var received []byte
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					raw, err := io.ReadAll(req.Body)
					if err != nil {
						t.Error(err)
					}
					mu.Lock()
					received = append([]byte(nil), raw...)
					mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					w.Write(responseRaw)
				}))
				defer server.Close()
				base := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				}}
				defer base.CloseIdleConnections()
				up := &windowActualNoRedirectTransport{base}
				p := r.driver.permit
				p.observer = observer
				p.observerIdentity = observer.EvaluationIdentity()
				r.driver.observer = observer
				p.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				turnCtx, turn := observer.StartTurn(ctx, r.driver.arm)
				logicalCtx, logical := observability.Start(turnCtx, "logical.fixture", "generation")
				logical.Input(map[string]any{"password": secrets["password"], "cookie": secrets["cookie"], "refresh_token": secrets["refresh_token"], "session_id": secrets["session_id"], "message": "legacy-logical-visible"})
				_, tool := observability.Start(logicalCtx, "tool.fixture_read", "tool")
				tool.Input(map[string]any{"cookie": secrets["cookie"], "message": "legacy-tool-visible"})
				tool.Output("legacy-tool-output")
				tool.End(nil)
				r.phase.parent = logicalCtx
				endpoint, _, _ := windowProviderEndpoint(provider)
				req, err := http.NewRequestWithContext(context.WithValue(logicalCtx, windowOwnedPhaseKey{}, r.phase), "POST", endpoint, bytes.NewReader(requestRaw))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				transport, err := newWindowProviderTransport(r.driver, provider, up)
				if err != nil {
					t.Fatal(err)
				}
				response, err := transport.RoundTrip(req)
				if err != nil {
					t.Fatal(err)
				}
				returned, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || !bytes.Equal(returned, responseRaw) {
					t.Fatal("provider response bytes changed", err)
				}
				logical.Output("legacy-logical-output")
				logical.End(nil)
				turn.End(200, observability.Summary{})
				if err = observer.ForceFlush(ctx); err != nil {
					t.Fatal(err)
				}
				state := c.ledger.Snapshot()
				if state.ModelAttempts+state.JEVAttempts != 1 || state.Pending != nil || len(state.UnknownIntents) != 0 || len(c.extras) != 0 || r.phase.model+r.phase.jev != 1 {
					t.Fatal("logical/native counters changed", state)
				}
				stats := observer.Stats()
				if stats.Turns != 1 || stats.SampledTurns != 1 || stats.Accepted != 4 || stats.Exported != 4 || stats.Failed != 0 || stats.Dropped != 0 {
					t.Fatal("observer counters changed", stats)
				}
				manifest, _, err := verifyWindowPayloadStore(p.payload)
				if err != nil || len(manifest.Packets) != 1 {
					t.Fatal("private packet unavailable", err)
				}
				packet := manifest.Packets[0]
				persistedRequest, err := os.ReadFile(filepath.Join(p.payload.dir, packet.Request.File))
				if err != nil || !bytes.Equal(persistedRequest, requestRaw) {
					t.Fatal("private request bytes changed", err)
				}
				persistedResponse, err := os.ReadFile(filepath.Join(p.payload.dir, packet.Response.Body.File))
				if err != nil || !bytes.Equal(persistedResponse, responseRaw) {
					t.Fatal("private response bytes changed", err)
				}
				mu.Lock()
				defer mu.Unlock()
				if !bytes.Equal(received, requestRaw) {
					t.Fatal("sent body bytes changed")
				}
				var native *tracepb.Span
				legacy := map[string]*tracepb.Span{}
				for _, span := range spans {
					if span.Name == "window.native" {
						if native != nil {
							t.Fatal("duplicate native observation")
						}
						native = span
					} else {
						legacy[span.Name] = span
					}
				}
				if native == nil {
					t.Fatal("native observation missing")
				}
				nativeRaw, _ := json.Marshal(native)
				for key, value := range secrets {
					if bytes.Contains(nativeRaw, []byte(value)) {
						t.Errorf("native observation leaked synthetic sentinel from %s", key)
					}
				}
				values := map[string]string{}
				integers := map[string]int64{}
				for _, a := range native.Attributes {
					values[a.Key] = a.Value.GetStringValue()
					integers[a.Key] = a.Value.GetIntValue()
					if a.Key == "langfuse.observation.input" || a.Key == "langfuse.observation.output" {
						t.Error("native observation copied raw body content")
					}
				}
				prefix := "langfuse.observation.metadata."
				if values[prefix+"body_sha256"] != conversationHash(requestRaw) || integers[prefix+"request_bytes"] != int64(len(requestRaw)) || values[prefix+"response_capture_sha256"] != conversationHash(responseRaw) || integers[prefix+"response_bytes"] != int64(len(responseRaw)) || integers[prefix+"response_status"] != 200 || integers[prefix+"native_sequence"] != 1 || integers[prefix+"native_attempt"] != 1 || values[prefix+"native_id"] == "" || values[prefix+"claim_sha256"] != c.claimSHA {
					t.Error("native body metadata/linkage absent or altered")
				}
				for name, marker := range map[string]string{"logical.fixture": "legacy-logical-visible", "tool.fixture_read": "legacy-tool-visible"} {
					span := legacy[name]
					if span == nil {
						t.Fatal("legacy operation missing", name)
					}
					hasInput, hasOutput := false, false
					for _, a := range span.Attributes {
						if a.Key == "langfuse.observation.input" {
							hasInput = true
							if capture && !strings.Contains(a.Value.GetStringValue(), marker) {
								t.Error("legacy input changed", name)
							}
						}
						if a.Key == "langfuse.observation.output" {
							hasOutput = true
						}
					}
					if hasInput != capture || hasOutput != capture {
						t.Error("legacy capture switch changed", name, capture, hasInput, hasOutput)
					}
				}
			})
		}
	}
}
