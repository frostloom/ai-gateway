package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func spoolConfig(t *testing.T) DurableSpoolConfig {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return DurableSpoolConfig{Root: filepath.Join(p, "spool"), RunID: strings.Repeat("a", 64), MaxBytes: 4 << 30}
}
func spoolSpans(id byte) []*tracepb.ResourceSpans {
	return []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{id}, 8), ParentSpanId: bytes.Repeat([]byte{2}, 8), Name: "original", StartTimeUnixNano: 123, EndTimeUnixNano: 456, Attributes: []*commonpb.KeyValue{{Key: "content", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "original content"}}}}}}}}}}
}
func startSpool(t *testing.T) (*durableSpoolClient, DurableSpoolConfig) {
	t.Helper()
	c := spoolConfig(t)
	s := newDurableSpoolClient(c)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s, c
}
func TestDurableOriginalBytesAndClosedProof(t *testing.T) {
	s, c := startSpool(t)
	ctx := context.Background()
	spans := spoolSpans(3)
	want, err := proto.MarshalOptions{Deterministic: true}.Marshal(&collector.ExportTraceServiceRequest{ResourceSpans: spans})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UploadTraces(ctx, spans); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyClosedDurableSpool(ctx, c); err == nil {
		t.Fatal("open spool verified")
	}
	if err = s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	proof, err := VerifyClosedDurableSpool(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := proof.ReadBatch(ctx, 1)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("original bytes: %x %v", got, err)
	}
	ids := proof.SpanIdentities()
	if len(ids) != 1 || ids[0].TraceID != strings.Repeat("01", 16) || ids[0].SpanID != strings.Repeat("03", 8) || ids[0].ParentSpanID != strings.Repeat("02", 8) || ids[0].StartTimeUnixNano != 123 || ids[0].EndTimeUnixNano != 456 {
		t.Fatalf("identities %+v", ids)
	}
	ids[0].SpanID = "forged"
	if proof.SpanIdentities()[0].SpanID == "forged" {
		t.Fatal("mutable inventory")
	}
	var forged VerifiedDurableSpool
	raw, _ := json.Marshal(proof.Summary())
	_ = json.Unmarshal(raw, &forged)
	if _, err := forged.ReadBatch(ctx, 1); err == nil {
		t.Fatal("JSON minted proof")
	}
	if err = os.WriteFile(filepath.Join(c.Root, "00000000000000000001.pb"), []byte("tamper"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = proof.ReadBatch(ctx, 1); err == nil {
		t.Fatal("cached body trusted")
	}
}
func TestDurableDuplicateAndConflict(t *testing.T) {
	s, _ := startSpool(t)
	ctx := context.Background()
	a := spoolSpans(3)
	if err := s.UploadTraces(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.UploadTraces(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, err := s.status(); err != nil || got.Batches != 1 || got.Spans != 1 {
		t.Fatalf("duplicate %+v %v", got, err)
	}
	a[0].ScopeSpans[0].Spans[0].Name = "conflict"
	if err := s.UploadTraces(ctx, a); err == nil {
		t.Fatal("conflicting ID accepted")
	}
	if err := s.UploadTraces(ctx, spoolSpans(4)); err == nil {
		t.Fatal("poison was not sticky")
	}
}
func TestDurableBoundsAndMalformed(t *testing.T) {
	for _, kind := range []string{"large", "zero_trace", "zero_span", "short_parent", "backward_time", "duplicate_id", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := startSpool(t)
			ctx := context.Background()
			spans := spoolSpans(3)
			span := spans[0].ScopeSpans[0].Spans[0]
			switch kind {
			case "large":
				span.Name = strings.Repeat("x", 1<<20)
			case "zero_trace":
				span.TraceId = make([]byte, 16)
			case "zero_span":
				span.SpanId = make([]byte, 8)
			case "short_parent":
				span.ParentSpanId = []byte{1}
			case "backward_time":
				span.EndTimeUnixNano = 1
			case "duplicate_id":
				spans[0].ScopeSpans[0].Spans = append(spans[0].ScopeSpans[0].Spans, proto.Clone(span).(*tracepb.Span))
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := s.UploadTraces(ctx, spans); err == nil {
				t.Fatal("invalid upload succeeded")
			}
			if err := s.Stop(context.Background()); err == nil {
				t.Fatal("poison cleanly closed")
			}
		})
	}
}
func TestDurableConcurrentUploadStop(t *testing.T) {
	s, c := startSpool(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 3; i < 43; i++ {
		wg.Add(1)
		go func(id byte) { defer wg.Done(); _ = s.UploadTraces(ctx, spoolSpans(id)) }(byte(i))
	}
	stop := make(chan error, 1)
	go func() { stop <- s.Stop(ctx) }()
	wg.Wait()
	if err := <-stop; err != nil {
		t.Fatal(err)
	}
	proof, err := VerifyClosedDurableSpool(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Summary().Poisoned {
		t.Fatal("orderly concurrent close poisoned")
	}
}
func TestDurableObserverSelectionIsolationAndAccounting(t *testing.T) {
	ctx := context.Background()
	c := enabled("https://unused.invalid")
	dc := spoolConfig(t)
	c.DurableSpool = &dc
	production := c
	production.Environment = "production"
	if _, err := New(ctx, production); err == nil {
		t.Fatal("production durable allowed")
	}
	o, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	original := dc
	dc.RunID = strings.Repeat("b", 64)
	dc.Root = "changed"
	dc.MaxBytes = 1
	_, turn := o.StartTurn(ctx, "model")
	turn.End(200, Summary{})
	if err = o.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	st := o.Stats()
	if st.Accepted != 1 || st.Persisted != 1 || st.Exported != 0 || st.Failed != 0 {
		t.Fatalf("accounting %+v", st)
	}
	if err = o.CheckDurableReceipt(ctx); err != nil {
		t.Fatal(err)
	}
	identity := o.EvaluationIdentity()
	if identity.ObservationReceiptBoundary != "durable_local" || identity.SpoolRunID != original.RunID || identity.SpoolMaxBytes != 4<<30 {
		t.Fatalf("identity %+v", identity)
	}
	if err = o.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyClosedDurableSpool(ctx, original); err != nil {
		t.Fatal(err)
	}
}
func TestDurableDirectDefaultGolden(t *testing.T) {
	configJSON, _ := json.Marshal(Config{})
	if string(configJSON) != `{"CaptureContent":false,"Enabled":false,"Endpoint":"","PublicKey":"","SecretKey":"","HashKey":"","Release":"","Environment":"","SampleRate":0,"QueueSize":0,"ExportTimeout":0}` {
		t.Fatalf("direct config changed %s", configJSON)
	}
	raw, _ := json.Marshal(Stats{})
	if string(raw) != `{"Accepted":0,"Dropped":0,"Exported":0,"Failed":0,"Batches":0,"Turns":0,"SampledTurns":0,"QueueDepth":0}` {
		t.Fatalf("direct stats changed %s", raw)
	}
	raw, _ = json.Marshal(EvaluationIdentity{})
	if string(raw) != `{"enabled":false,"capture_content":false,"sample_rate":0,"environment":"","release":"","export_timeout_ms":0,"queue_size":0,"endpoint_sha256":"","project_sha256":""}` {
		t.Fatalf("direct identity changed %s", raw)
	}
	c := enabled("https://unused.invalid")
	c.ExportTimeout = time.Second
	o, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Shutdown(context.Background())
	if _, err = o.DurableStatus(); err == nil {
		t.Fatal("direct observer minted receipt")
	}
}

type durableDenyNetwork struct{ calls atomic.Uint64 }

func (d *durableDenyNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, errors.New("network forbidden by durable test")
}
func TestDurableSDKOriginalContentAndCredentials(t *testing.T) {
	deny := &durableDenyNetwork{}
	previous := http.DefaultTransport
	http.DefaultTransport = deny
	t.Cleanup(func() {
		http.DefaultTransport = previous
		if deny.calls.Load() != 0 {
			t.Error("durable export attempted network")
		}
	})
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[capture], func(t *testing.T) {
			c := enabled("https://unused.invalid")
			c.CaptureContent = capture
			dc := spoolConfig(t)
			c.DurableSpool = &dc
			ctx := context.Background()
			o, e := New(ctx, c)
			if e != nil {
				t.Fatal(e)
			}
			ctx, turn := o.StartTurn(ctx, "model")
			rootID := turn.root.SpanContext()
			turn.Input("original task text")
			ctx, op := Start(ctx, "original child", "generation")
			_ = ctx
			childID := op.span.SpanContext()
			op.Input(map[string]any{"message": "literal content", "password": "private-value"})
			op.Output("original answer")
			op.End(nil)
			turn.End(200, Summary{})
			if e = o.Shutdown(context.Background()); e != nil {
				t.Fatal(e)
			}
			v, e := VerifyClosedDurableSpool(context.Background(), dc)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for i := uint64(1); i <= v.Summary().Batches; i++ {
				raw, e := v.ReadBatch(context.Background(), i)
				if e != nil {
					t.Fatal(e)
				}
				for _, secret := range []string{c.SecretKey, c.PublicKey, c.HashKey, "private-value"} {
					if bytes.Contains(raw, []byte(secret)) {
						t.Fatal("credential or unredacted secret persisted")
					}
				}
				var req collector.ExportTraceServiceRequest
				if e = proto.Unmarshal(raw, &req); e != nil {
					t.Fatal(e)
				}
				for _, r := range req.ResourceSpans {
					for _, scope := range r.ScopeSpans {
						for _, sp := range scope.Spans {
							if sp.Name == "original child" {
								found = true
								wantChild := childID.SpanID()
								wantTrace := rootID.TraceID()
								wantParent := rootID.SpanID()
								if !bytes.Equal(sp.SpanId, wantChild[:]) || !bytes.Equal(sp.TraceId, wantTrace[:]) || !bytes.Equal(sp.ParentSpanId, wantParent[:]) || sp.StartTimeUnixNano == 0 || sp.EndTimeUnixNano < sp.StartTimeUnixNano {
									t.Fatal("SDK original span changed")
								}
								a := attrs(sp)
								if capture {
									if a["langfuse.observation.output"] != `"original answer"` || !strings.Contains(a["langfuse.observation.input"], "literal content") || !strings.Contains(a["langfuse.observation.input"], "[REDACTED]") {
										t.Fatalf("content capture changed %+v", a)
									}
								} else {
									if _, ok := a["langfuse.observation.input"]; ok {
										t.Fatal("content disabled input persisted")
									}
									if _, ok := a["langfuse.observation.output"]; ok {
										t.Fatal("content disabled output persisted")
									}
								}
							}
						}
					}
				}
			}
			if !found {
				t.Fatal("original SDK child missing")
			}
		})
	}
}
func TestDurableRepeatedShutdownRetainsFailure(t *testing.T) {
	c := enabled("https://unused.invalid")
	dc := spoolConfig(t)
	c.DurableSpool = &dc
	o, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	o.durable.io.sync = func(*os.File) error { return context.DeadlineExceeded }
	if e = o.Shutdown(context.Background()); e == nil {
		t.Fatal("first close fault swallowed")
	}
	if e = o.Shutdown(context.Background()); e == nil {
		t.Fatal("repeat close fault swallowed")
	}
	if e = o.ForceFlush(context.Background()); e == nil {
		t.Fatal("post-fault flush reported success")
	}
}
func TestDurablePreCanceledShutdownStopsWorker(t *testing.T) {
	c := enabled("https://unused.invalid")
	dc := spoolConfig(t)
	c.DurableSpool = &dc
	o, e := New(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = o.Shutdown(ctx); e == nil {
		t.Fatal("canceled shutdown succeeded")
	}
	select {
	case <-o.processor.done:
	case <-time.After(time.Second):
		o.processor.cancel()
		<-o.processor.done
		t.Fatal("canceled Shutdown orphaned worker")
	}
	st, e := o.DurableStatus()
	if e == nil || !st.Poisoned || st.Closed {
		t.Fatalf("canceled shutdown receipt %+v %v", st, e)
	}
}

func TestDurableSeparateProcessReaderAndCrash(t *testing.T) {
	for _, mode := range []string{"closed", "crash"} {
		t.Run(mode, func(t *testing.T) {
			c := spoolConfig(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestDurableSubprocessWriter$")
			cmd.Env = append(os.Environ(), "DURABLE_TEST_ROOT="+c.Root, "DURABLE_TEST_MODE="+mode)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("controlled child %v %s", e, out)
			}
			proof, e := VerifyClosedDurableSpool(context.Background(), c)
			if mode == "crash" {
				if e == nil {
					t.Fatal("orphan spool verified")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			sum := proof.Summary()
			if !sum.DataOnly || !sum.DurabilityReturnUnproven || !sum.OwnerCompletionUnproven || sum.Spans != 1 {
				t.Fatalf("disk proof overstated %+v", sum)
			}
			if _, e = proof.ReadBatch(context.Background(), 1); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestDurableSubprocessWriter(t *testing.T) {
	root := os.Getenv("DURABLE_TEST_ROOT")
	if root == "" {
		return
	}
	s := newDurableSpoolClient(DurableSpoolConfig{Root: root, RunID: strings.Repeat("a", 64), MaxBytes: 4 << 30})
	if e := s.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := s.UploadTraces(context.Background(), spoolSpans(3)); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("DURABLE_TEST_MODE") == "closed" {
		if e := s.Stop(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	// The orphan case exits with no Stop; OS closes descriptors but supplies no
	// clean close marker. The parent is a genuinely independent disk reader.
}
func TestDurableExactBatchLimit(t *testing.T) {
	s, c := startSpool(t)
	spans := spoolSpans(3)
	span := spans[0].ScopeSpans[0].Spans[0]
	span.Name = strings.Repeat("x", (1<<20)-100)
	for i := 0; i < 4; i++ {
		n := proto.Size(&collector.ExportTraceServiceRequest{ResourceSpans: spans})
		if n == 1<<20 {
			break
		}
		span.Name = strings.Repeat("x", len(span.Name)+(1<<20)-n)
	}
	if n := proto.Size(&collector.ExportTraceServiceRequest{ResourceSpans: spans}); n != 1<<20 {
		t.Fatalf("fixture size %d", n)
	}
	if e := s.UploadTraces(context.Background(), spans); e != nil {
		t.Fatal(e)
	}
	if e := s.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, e := VerifyClosedDurableSpool(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := v.ReadBatch(context.Background(), 1)
	if e != nil || len(raw) != 1<<20 {
		t.Fatal("exact 1 MiB not retained", len(raw), e)
	}
}
