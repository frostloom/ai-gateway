package observability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type deliveryTransport func(*http.Request) (*http.Response, error)

func (f deliveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func deliveryFixture(t *testing.T) (*VerifiedDurableSpool, SpoolDeliveryConfig, []byte) {
	t.Helper()
	s, c := startSpool(t)
	ctx := context.Background()
	if e := s.UploadTraces(ctx, spoolSpans(3)); e != nil {
		t.Fatal(e)
	}
	if e := s.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := VerifyClosedDurableSpool(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := v.ReadBatch(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	return v, SpoolDeliveryConfig{Endpoint: "http://127.0.0.1:1/api/public/otel/v1/traces", PublicKey: "pk-test", SecretKey: "sk-test", JournalRoot: filepath.Join(filepath.Dir(c.Root), "delivery"), BatchSHA256: spoolDigest(raw)}, raw
}
func TestSpoolDeliverySealedZeroReadRetriesSameBytes(t *testing.T) {
	v, c, raw := deliveryFixture(t)
	var bodies [][]byte
	var retained []io.ReadCloser
	var waits []time.Duration
	n := 0
	factory := func() http.RoundTripper {
		return deliveryTransport(func(r *http.Request) (*http.Response, error) {
			n++
			if r.GetBody != nil || !r.Close {
				t.Fatal("replayable request")
			}
			retained = append(retained, r.Body)
			if n < 3 {
				return nil, io.EOF
			}
			b, _ := io.ReadAll(r.Body)
			bodies = append(bodies, b)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil)), Header: http.Header{}}, nil
		})
	}
	result, e := deliverSpoolBatch(context.Background(), v, 1, c, factory, func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	if e != nil || result.Outcome != "acknowledged" || n != 3 || !bytes.Equal(bodies[0], raw) {
		t.Fatalf("%+v %v n=%d", result, e, n)
	}
	if len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 200*time.Millisecond {
		t.Fatal(waits)
	}
	for _, body := range retained[:2] {
		b, e := io.ReadAll(body)
		if len(b) != 0 || e == nil {
			t.Fatal("late reader got original bytes")
		}
	}
	if _, e := DeliverSpoolBatch(context.Background(), v, 1, c); e == nil {
		t.Fatal("second invocation allowed")
	}
	data, _ := os.ReadFile(filepath.Join(c.JournalRoot, "batch-00000000000000000001", "intent.json"))
	if bytes.Contains(data, []byte("sk-test")) {
		t.Fatal("credential leaked")
	}
}
func TestSpoolDeliveryNeverRetriesExposedOrStatus(t *testing.T) {
	for _, kind := range []string{"partial", "full", "401", "500", "status-before-read", "redirect", "decode", "partial-rejection", "oversize", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			v, c, _ := deliveryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			f := func() http.RoundTripper {
				return deliveryTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					switch kind {
					case "partial":
						r.Body.Read(make([]byte, 1))
						return nil, io.EOF
					case "full":
						io.Copy(io.Discard, r.Body)
						return nil, io.EOF
					case "cancel":
						cancel()
						return nil, io.EOF
					}
					status := 200
					body := ""
					switch kind {
					case "401":
						status = 401
					case "500":
						status = 500
					case "status-before-read":
						status = 503
					case "redirect":
						status = 307
					case "decode":
						body = "not protobuf"
					case "partial-rejection":
						body = string([]byte{0x0a, 0x02, 0x08, 0x01})
					case "oversize":
						body = strings.Repeat("a", (1<<20)+1)
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
				})
			}
			result, e := deliverSpoolBatch(ctx, v, 1, c, f, func(context.Context, time.Duration) error { t.Fatal("unsafe retry"); return nil })
			if e == nil || calls != 1 || result.Outcome != "unknown" {
				t.Fatalf("%+v %v calls%d", result, e, calls)
			}
			if _, e := DeliverSpoolBatch(context.Background(), v, 1, c); e == nil {
				t.Fatal("unknown replayed")
			}
		})
	}
}
func TestSpoolDeliveryCapAndSealRace(t *testing.T) {
	v, c, _ := deliveryFixture(t)
	calls := 0
	var delays []time.Duration
	r, e := deliverSpoolBatch(context.Background(), v, 1, c, func() http.RoundTripper {
		return deliveryTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("private transport detail")
		})
	}, func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil })
	if e == nil || calls != 5 || r.Outcome != "unsent" || len(delays) != 4 || delays[3] != 800*time.Millisecond {
		t.Fatalf("%+v %v %d %v", r, e, calls, delays)
	}
	for i := 0; i < 100; i++ {
		b := newDeliveryBody([]byte("original"))
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = b.Read(make([]byte, 1)) }()
		zero := b.seal()
		wg.Wait()
		if zero {
			data, e := io.ReadAll(b)
			if len(data) != 0 || e == nil {
				t.Fatal("seal race")
			}
		}
	}
}
func TestSpoolDeliveryRealHTTPOriginalBytesNoRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "redirect"}[redirect], func(t *testing.T) {
			v, c, raw := deliveryFixture(t)
			calls := 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
			defer target.Close()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				b, _ := io.ReadAll(r.Body)
				if !bytes.Equal(b, raw) || r.ProtoMajor != 1 || r.Header.Get("Content-Type") != "application/x-protobuf" {
					t.Error("original transport changed")
				}
				if redirect {
					w.Header().Set("Location", target.URL)
					w.WriteHeader(307)
				}
			}))
			defer srv.Close()
			c.Endpoint = srv.URL + "/api/public/otel/v1/traces"
			result, e := DeliverSpoolBatch(context.Background(), v, 1, c)
			if calls != 1 || (e == nil) == redirect || (!redirect && result.Outcome != "acknowledged") {
				t.Fatalf("%+v %v %d", result, e, calls)
			}
		})
	}
}
func TestSpoolDeliveryRejectsProofAndChangedBodyBeforeIO(t *testing.T) {
	v, c, _ := deliveryFixture(t)
	for _, bad := range []string{"nil", "forged", "sha", "mutated"} {
		t.Run(bad, func(t *testing.T) {
			p := v
			cfg := c
			switch bad {
			case "nil":
				p = nil
			case "forged":
				p = &VerifiedDurableSpool{}
			case "sha":
				cfg.BatchSHA256 = strings.Repeat("0", 64)
			case "mutated":
				os.WriteFile(filepath.Join(v.cfg.Root, spoolBodyName(1)), []byte("bad"), 0600)
			}
			_, e := deliverSpoolBatch(context.Background(), p, 1, cfg, func() http.RoundTripper { t.Fatal("network before proof"); return nil }, nil)
			if e == nil {
				t.Fatal("bad proof accepted")
			}
		})
	}
}

func TestSpoolDeliveryPersistenceFailureRetainsPending(t *testing.T) {
	v, c, _ := deliveryFixture(t)
	calls := 0
	r, e := deliverSpoolBatch(context.Background(), v, 1, c, func() http.RoundTripper {
		return deliveryTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			io.Copy(io.Discard, req.Body)
			p := filepath.Join(c.JournalRoot, "batch-00000000000000000001", "result-1.json")
			if e := os.WriteFile(p, []byte("retained failure"), 0600); e != nil {
				t.Fatal(e)
			}
			return nil, io.EOF
		})
	}, func(context.Context, time.Duration) error { t.Fatal("retried failed persistence"); return nil })
	if e == nil || calls != 1 || r.Attempts != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	folder := filepath.Join(c.JournalRoot, "batch-00000000000000000001")
	if _, e := os.Stat(filepath.Join(folder, "attempt-1.json")); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(folder, "result.json")); !os.IsNotExist(e) {
		t.Fatal("failed persistence finalized")
	}
	b, _ := os.ReadFile(filepath.Join(folder, "result-1.json"))
	if string(b) != "retained failure" {
		t.Fatal("failure overwritten")
	}
	if _, e := DeliverSpoolBatch(context.Background(), v, 1, c); e == nil {
		t.Fatal("pending retried")
	}
}
func TestSpoolDeliveryDeadlineAndConcurrentAttempt(t *testing.T) {
	v, c, _ := deliveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	finished := make(chan struct{})
	var result SpoolDeliveryResult
	var err error
	go func() {
		defer close(finished)
		result, err = deliverSpoolBatch(ctx, v, 1, c, func() http.RoundTripper {
			return deliveryTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
		}, nil)
	}()
	<-entered
	if _, e := DeliverSpoolBatch(context.Background(), v, 1, c); e == nil {
		t.Fatal("concurrent shipment allowed")
	}
	cancel()
	<-finished
	if err == nil || result.Attempts != 1 || result.Outcome != "unknown" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestSpoolDeliveryRangePreservesMiddleUnknownAndUntouchedSuffix(t *testing.T) {
	for _, kind := range []string{"middle-unknown", "expire", "prior-journal", "over-bound"} {
		t.Run(kind, func(t *testing.T) {
			s, spool := startSpool(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			for i := byte(3); i <= 5; i++ {
				if e := s.UploadTraces(ctx, spoolSpans(i)); e != nil {
					t.Fatal(e)
				}
			}
			if e := s.Stop(ctx); e != nil {
				t.Fatal(e)
			}
			v, e := VerifyClosedDurableSpool(ctx, spool)
			if e != nil {
				t.Fatal(e)
			}
			var batches []SpoolDeliveryBatch
			for i := uint64(1); i <= 3; i++ {
				raw, e := v.ReadBatch(ctx, i)
				if e != nil {
					t.Fatal(e)
				}
				batches = append(batches, SpoolDeliveryBatch{Batch: i, BodySHA256: spoolDigest(raw)})
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if spoolDigest(raw) != batches[calls-1].BodySHA256 {
					t.Error("original batch order/body changed")
				}
				if calls == 2 {
					if kind == "expire" {
						cancel()
					}
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			cfg := SpoolDeliveryConfig{Endpoint: server.URL + "/api/public/otel/v1/traces", PublicKey: "public", SecretKey: "private", JournalRoot: filepath.Join(filepath.Dir(spool.Root), "delivery")}
			if kind == "prior-journal" {
				os.Mkdir(cfg.JournalRoot, 0700)
				os.Mkdir(filepath.Join(cfg.JournalRoot, "batch-00000000000000000002"), 0700)
			}
			if kind == "over-bound" {
				for len(batches) < 129 {
					batches = append(batches, SpoolDeliveryBatch{Batch: uint64(len(batches) + 1), BodySHA256: strings.Repeat("a", 64)})
				}
			}
			got, e := DeliverSpoolRange(ctx, v, batches, cfg)
			switch kind {
			case "middle-unknown":
				if e != nil || calls != 3 || len(got.Results) != 3 || len(got.Unattempted) != 0 || got.Results[1].Outcome != "unknown" {
					t.Fatalf("%+v %v calls%d", got, e, calls)
				}
			case "expire":
				if e == nil || calls != 2 || len(got.Results) != 2 || len(got.Unattempted) != 1 || got.Unattempted[0].Batch != 3 {
					t.Fatalf("%+v %v calls%d", got, e, calls)
				}
				if _, e := os.Stat(filepath.Join(cfg.JournalRoot, "batch-00000000000000000003")); !os.IsNotExist(e) {
					t.Fatal("untouched suffix got intent")
				}
			default:
				if e == nil || calls != 0 {
					t.Fatalf("invalid range sent %d: %v", calls, e)
				}
			}
		})
	}
}
