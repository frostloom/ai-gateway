package observability

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type capture struct {
	mu                  sync.Mutex
	spans               []*tracepb.Span
	raw                 []byte
	auth, version, path string
}

func receiver(t *testing.T) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(raw, &body); err != nil {
			t.Error(err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.raw = append(c.raw, raw...)
		c.auth = r.Header.Get("Authorization")
		c.version = r.Header.Get("x-langfuse-ingestion-version")
		c.path = r.URL.Path
		for _, resource := range body.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				c.spans = append(c.spans, scope.Spans...)
			}
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	return s, c
}
func enabled(endpoint string) Config {
	return Config{Enabled: true, Endpoint: endpoint + "/api/public/otel/v1/traces", PublicKey: "pk-test", SecretKey: "sk-private", HashKey: strings.Repeat("h", 32), Release: "test-commit", Environment: "evaluation", SampleRate: 1, QueueSize: 16, ExportTimeout: time.Second}
}
func attrs(s *tracepb.Span) map[string]string {
	m := map[string]string{}
	for _, a := range s.Attributes {
		m[a.Key] = a.Value.GetStringValue()
	}
	return m
}

func TestDisabledHasNoExporterOrValidation(t *testing.T) {
	o, err := New(context.Background(), Config{})
	if err != nil || o != nil {
		t.Fatalf("disabled observer=%v err=%v", o, err)
	}
	ctx, turn := o.StartTurn(context.Background(), "model")
	turn.BindIdentity(42, "secret-session")
	_, op := Start(ctx, "tool.get_balance", "tool")
	op.End(nil)
	turn.End(200, Summary{})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOTLPIdentityPropagationAndRedaction(t *testing.T) {
	s, c := receiver(t)
	defer s.Close()
	o, err := New(context.Background(), enabled(s.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ctx, turn := o.StartTurn(ctx, "jev_selective")
	turn.BindIdentity(42, "private-session")
	_, op := Start(ctx, "jev.intent", "evaluator")
	op.End(errors.New("private error sk-provider phone-13800000000"))
	cancel()
	turn.End(500, Summary{Stage: "process", JevCalls: 1, JevErrors: 1})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth != "Basic "+base64.StdEncoding.EncodeToString([]byte("pk-test:sk-private")) || c.version != "4" || c.path != "/api/public/otel/v1/traces" {
		t.Fatal("wrong exporter authentication or endpoint")
	}
	if len(c.spans) != 2 {
		t.Fatalf("spans=%d want 2", len(c.spans))
	}
	var root, child *tracepb.Span
	for _, span := range c.spans {
		if span.Name == "agent.turn" {
			root = span
		} else {
			child = span
		}
		a := attrs(span)
		if a["langfuse.session.id"] == "" || a["langfuse.user.id"] == "" || a["langfuse.release"] != "test-commit" || a["langfuse.observation.metadata.routing_mode"] != "jev_selective" {
			t.Fatalf("missing propagated attributes: %+v", a)
		}
	}
	if root == nil || child == nil || !bytes.Equal(root.TraceId, child.TraceId) || !bytes.Equal(root.SpanId, child.ParentSpanId) {
		t.Fatal("broken trace hierarchy")
	}
	for _, secret := range []string{"private-session", "private error", "sk-provider", "phone-13800000000", "sk-private"} {
		if bytes.Contains(c.raw, []byte(secret)) {
			t.Fatalf("leaked %s", secret)
		}
	}
}

func TestUsageMissingIsNotZero(t *testing.T) {
	s, c := receiver(t)
	defer s.Close()
	o, err := New(context.Background(), enabled(s.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, turn := o.StartTurn(context.Background(), "model")
	turn.BindIdentity(19, "sid")
	_, op := Start(ctx, "llm.attempt", "generation")
	op.Usage(Usage{})
	op.End(nil)
	zero := int64(0)
	_, op = Start(ctx, "llm.attempt", "generation")
	op.Usage(Usage{Input: &zero, Output: &zero})
	op.End(nil)
	turn.End(200, Summary{})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	foundUnknown, foundZero := false, false
	for _, span := range c.spans {
		if span.Name != "llm.attempt" {
			continue
		}
		a := attrs(span)
		if a["langfuse.observation.metadata.usage_complete"] == "false" {
			foundUnknown = true
			if _, exists := a["langfuse.observation.usage_details"]; exists {
				t.Fatal("invented zero usage")
			}
		} else {
			foundZero = true
			if a["langfuse.observation.usage_details"] != "{\"input\":0,\"output\":0}" {
				t.Fatalf("lost explicit zero: %s", a["langfuse.observation.usage_details"])
			}
		}
	}
	if !foundUnknown || !foundZero {
		t.Fatal("usage distinction not exported")
	}
}

func TestExportFailureNeverRetriesOrBlocksTurn(t *testing.T) {
	var calls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer s.Close()
	o, err := New(context.Background(), enabled(s.URL))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, turn := o.StartTurn(context.Background(), "model")
	turn.End(200, Summary{})
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("turn blocked on exporter")
	}
	_ = o.Shutdown(context.Background())
	if calls.Load() != 1 || o.Stats().Failed != 1 {
		t.Fatalf("retry/failure accounting: calls=%d stats=%+v", calls.Load(), o.Stats())
	}
}

func TestConfigRejectsUnsafeAndIncompleteSettings(t *testing.T) {
	for _, edit := range []func(*Config){func(c *Config) { c.HashKey = "short" }, func(c *Config) { c.Endpoint = "https://user:password@host/path" }, func(c *Config) { c.SampleRate = 1.1 }, func(c *Config) { c.SecretKey = "" }, func(c *Config) { c.Environment = "mixed" }} {
		c := enabled("http://localhost:13000")
		edit(&c)
		if o, err := New(context.Background(), c); err == nil {
			if o != nil {
				_ = o.Shutdown(context.Background())
			}
			t.Fatal("invalid config accepted")
		}
	}
}

func TestOptInContentIsRedactedAndBounded(t *testing.T) {
	server, capture := receiver(t)
	defer server.Close()
	cfg := enabled(server.URL)
	cfg.CaptureContent = true
	o, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, turn := o.StartTurn(context.Background(), "model")
	turn.BindIdentity(42, "sid")
	turn.Input("请查余额 联系 user@example.com 13800000000 sk-model-123456789")
	_, op := Start(ctx, "tool.refund_recharge", "tool")
	op.Input(map[string]any{"amount_money": 100, "password": "DO-NOT-SEND", "nested": map[string]any{"Authorization": "Bearer key-secret", "tenant_id": 42}, "note": strings.Repeat("x", 10000)})
	op.Output("退款结果 给 user@example.com 13800000000")
	op.End(errors.New("provider rejected sk-model-123456789 Authorization: Bearer hidden-token"))
	turn.Output("需要确认退款100元")
	turn.End(200, Summary{})
	if err := o.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, secret := range []string{"user@example.com", "13800000000", "sk-model-123456789", "DO-NOT-SEND", "key-secret", "hidden-token"} {
		if bytes.Contains(capture.raw, []byte(secret)) {
			t.Fatalf("content leaked %s", secret)
		}
	}
	var input string
	for _, span := range capture.spans {
		a := attrs(span)
		if span.Name == "agent.turn" {
			input = a["langfuse.observation.input"]
		}
		if len(a["langfuse.observation.input"]) > 4096 {
			t.Fatal("unbounded content")
		}
	}
	if !strings.Contains(input, "请查余额") || !strings.Contains(input, "[REDACTED]") {
		t.Fatalf("lost useful redacted content: %s", input)
	}
}

func TestPartialRejectionIsNotReportedAsExported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := proto.Marshal(&collector.ExportTraceServiceResponse{PartialSuccess: &collector.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: "private backend error"}})
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	o, err := New(context.Background(), enabled(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, turn := o.StartTurn(context.Background(), "model")
	turn.End(200, Summary{})
	_ = o.Shutdown(context.Background())
	if o.Stats().Failed != 1 || o.Stats().Exported != 0 {
		t.Fatalf("partial rejection hidden: %+v", o.Stats())
	}
}

func TestExporterDoesNotFollowRedirects(t *testing.T) {
	leaked := make(chan struct{}, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked <- struct{}{} }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	o, err := New(context.Background(), enabled(origin.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, turn := o.StartTurn(context.Background(), "model")
	turn.End(200, Summary{})
	_ = o.Shutdown(context.Background())
	select {
	case <-leaked:
		t.Fatal("export data followed a redirect")
	default:
	}
	if o.Stats().Failed != 1 {
		t.Fatal("redirect not counted as failure")
	}
}

func TestConcurrentIdentitiesAndZeroSampling(t *testing.T) {
	server, capture := receiver(t)
	defer server.Close()
	cfg := enabled(server.URL)
	cfg.QueueSize = 128
	observer, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			ctx, turn := observer.StartTurn(context.Background(), "model")
			turn.BindIdentity(uint64(i+1), "same-session-string")
			_, operation := Start(ctx, "tool.get_balance", "tool")
			operation.End(nil)
			turn.End(200, Summary{})
		}(i)
	}
	group.Wait()
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	identities := map[string]string{}
	sessions := map[string]bool{}
	for _, span := range capture.spans {
		a := attrs(span)
		key := string(span.TraceId)
		if identity, exists := identities[key]; exists && identity != a["langfuse.user.id"] {
			t.Fatal("cross-tenant identity propagation")
		}
		identities[key] = a["langfuse.user.id"]
		sessions[a["langfuse.session.id"]] = true
	}
	if len(capture.spans) != 20 || len(identities) != 10 || len(sessions) != 10 {
		t.Fatal("concurrent spans lost or aliased across tenants")
	}
	capture.mu.Unlock()
	cfg.SampleRate = 0
	observer, err = New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, turn := observer.StartTurn(context.Background(), "model")
	turn.End(200, Summary{})
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if observer.Stats().SampledTurns != 0 || observer.Stats().Exported != 0 || observer.Stats().Turns != 1 {
		t.Fatal("sampling zero not honored")
	}
}
