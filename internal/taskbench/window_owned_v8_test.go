package taskbench

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/observability"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Uses the established isolated MySQL fixture and in-memory Redis. The history,
// keys and financial files are synthetic; only the Handler proof is genuine in
// process and remains local_stub, never actual readiness or a quality claim.
func TestWindowV8OwnedHandlerRetryDoesNotRepeatTool(t *testing.T) {
	for _, failAt := range []int64{1, 2} {
		t.Run(fmt.Sprintf("failed_native_%d", failAt), func(t *testing.T) { testWindowV8OwnedHandlerRetry(t, failAt) })
	}
}
func testWindowV8OwnedHandlerRetry(t *testing.T, failAt int64) {
	c, r := fixtureWindowV8Campaign(t, "model", false, collectionPolicySHA())
	source, _, _ := windowScopeInputs(t)
	env, err := OpenPairedFixtureEnvironment(context.Background(), PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
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
	observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "synthetic-v8-pk", SecretKey: "synthetic-v8-sk", HashKey: strings.Repeat("h", 32), Release: c.prepared.scope.Bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Shutdown(context.Background())
	calls := new(int)
	fake := closedReadWindowOwned(calls)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		copy := req.Clone(req.Context())
		copy.URL, _ = url.Parse("https://api.deepseek.com/anthropic/v1/messages")
		resp, err := fake.RoundTrip(copy)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer resp.Body.Close()
		io.Copy(w, resp.Body)
	}))
	defer server.Close()
	var dials atomic.Int64
	tr := &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if dials.Add(1) == failAt {
			client, peer := net.Pipe()
			go func() { defer peer.Close(); windowTLSProtocolReadClientHello(bufio.NewReader(peer)) }()
			return client, nil
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer tr.CloseIdleConnections()
	up := &windowActualNoRedirectTransport{tr}
	permit := r.driver.permit
	permit.environment = env
	permit.observer = observer
	permit.observerIdentity = observer.EvaluationIdentity()
	permit.active = nil
	permit.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
	slot := c.prepared.scope.Slots[0]
	d, err := newWindowOwnedDriver(permit, source, slot, slot.ArmOrder[0], env, observer, "synthetic-model", "synthetic-jev", up, up)
	if err != nil {
		t.Fatal(err)
	}
	selected, _, err := fullCustomerSelection(source)
	if err != nil {
		t.Fatal(err)
	}
	definition := selected.Cases[0]
	ctx, run := observer.StartEvaluation(context.Background(), observability.Evaluation{RunID: "synthetic-v8-closed-owned", CaseKey: definition.Key(), Arm: d.arm, EvidenceKind: "local_stub"})
	record := recordCollectionConversation(ctx, definition, d)
	run.End(observability.EvaluationResult{Status: record.Result.Status, StructuralPass: record.Result.StructuralPass})
	if record.Disposition != "continue_recorded" {
		t.Fatalf("owned retry stopped: %+v ledger=%+v", record, c.ledger.Snapshot())
	}
	proof, err := d.finishWindowOwnedArm(context.Background(), record)
	if err != nil || validateWindowOwnedArmProof(permit, proof) != nil {
		t.Fatal("genuine local owned proof refused", err)
	}
	if *calls != 2 || dials.Load() != 3 || len(record.Result.Observations) != 1 || record.Result.Observations[0].Turn.Response.Telemetry.LLMCalls != 2 {
		t.Fatal("Handler/logical turn re-entered", *calls, dials.Load(), record.Result.Observations)
	}
	state := c.ledger.Snapshot()
	if len(state.UnknownIntents) != 1 || state.UnknownIntents[0].Sequence != int(failAt) || state.ModelAttempts != 3 || len(c.extras) != 1 || state.ModelUnknownHeld != 74752 {
		t.Fatal("native liability join", state)
	}
	manifest, _, err := verifyWindowPayloadStore(permit.payload)
	if err != nil || validateWindowAcquisitionPayloadLinkage(c.ledger, c.ledgerDir, "local_stub", manifest) != nil {
		t.Fatal("payload join", err)
	}
	c.mu.Lock()
	err = c.validateWindowV8ExtrasLocked(state)
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var native, logical, tools int
	for _, span := range spans {
		switch span.Name {
		case "window.native":
			native++
		case "llm.attempt":
			logical++
		case "tool.get_balance":
			tools++
		}
		if span.Name == "window.native" {
			seen := map[string]bool{}
			for _, a := range span.Attributes {
				seen[a.Key] = true
				if strings.Contains(a.Key, "header") || strings.Contains(a.Key, "authorization") {
					t.Error("native credential metadata")
				}
			}
			for _, key := range []string{"native_schema", "native_id", "native_sequence", "native_attempt", "body_sha256", "native_outcome"} {
				if !seen["langfuse.observation.metadata."+key] {
					t.Error("missing native metadata", key)
				}
			}
		}
	}
	if out := os.Getenv("WINDOW_V8_EXPORT_FIXTURES"); out != "" {
		raw, _ := json.MarshalIndent(map[string]any{"evidence_kind": "synthetic_controlled_owned_handler_not_actual_ready", "failed_native_sequence": failAt, "logical_model_calls": 2, "native_model_intents": 3, "native_settlements": 2, "native_unknown": 1, "extra_claims": 1, "tool_get_balance_executions": tools, "native_observations": native, "logical_generation_observations": logical, "spans": spans}, "", "  ")
		if err = os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(out, fmt.Sprintf("owned-handler-failure-%d.json", failAt)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if native != 3 || logical != 2 || tools != 1 {
		t.Fatalf("native/logical/tool observations = %d/%d/%d", native, logical, tools)
	}
}

func TestWindowV8MissingNativeObserverFrameStopsBeforeEgress(t *testing.T) {
	c, r := fixtureWindowV8Campaign(t, "model", false)
	observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: "http://127.0.0.1:1/api/public/otel/v1/traces", PublicKey: "synthetic-pk", SecretKey: "synthetic-sk", HashKey: strings.Repeat("h", 32), Release: c.prepared.scope.Bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 16, ExportTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Shutdown(context.Background())
	p := r.driver.permit
	p.observer = observer
	p.observerIdentity = observer.EvaluationIdentity()
	r.driver.observer = observer
	calls := 0
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, fmt.Errorf("synthetic no-network observer guard")
	}}
	up := &windowActualNoRedirectTransport{base}
	p.acquisition = &windowAcquisitionRun{auth: windowAcquisitionAuthorization{modelUpstream: up, jevUpstream: up}}
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.phase.parent = parent
	tr, err := newWindowProviderTransport(r.driver, "model", up)
	if err != nil {
		t.Fatal(err)
	}
	req := windowTransportRequest(t, "model").WithContext(context.WithValue(parent, windowOwnedPhaseKey{}, r.phase))
	if _, err = tr.RoundTrip(req); err == nil || calls != 0 {
		t.Fatal("missing native observer frame reached transport", err, calls)
	}
}
