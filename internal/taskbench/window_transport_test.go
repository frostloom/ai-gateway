package taskbench

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type windowTransportTestCapability struct {
	mu     sync.Mutex
	ledger *windowAccounting
	calls  map[string]int
}

func (c *windowTransportTestCapability) windowAccountingLedger() *windowAccounting { return c.ledger }
func (c *windowTransportTestCapability) windowNextRequest(_ context.Context, provider string) (windowRequestIdentity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[provider]++
	r := windowTestRequest(provider)
	r.Model = c.ledger.contract.Model.Model
	if provider == "jev" {
		r.Model = c.ledger.contract.JEV.Model
	}
	r.Attempt = c.calls[provider]
	return r, nil
}

func TestWindowTransportJournalFailurePreventsSuccessOrAnotherCall(t *testing.T) {
	for _, stage := range []string{"intent", "settlement"} {
		t.Run(stage, func(t *testing.T) {
			c := windowTestContract()
			c.Model = windowProtocolLimits("model")
			c.JEV = windowProtocolLimits("jev")
			c.ModelCap = 100000
			c.JEVCap = 100000
			var file *failingWindowFile
			l, e := newWindowAccountingWithFile(filepath.Join(t.TempDir(), "window"), c, func(f *os.File) windowJournalFile { file = &failingWindowFile{windowJournalFile: f}; return file })
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			cap := &windowTransportTestCapability{ledger: l, calls: map[string]int{}}
			calls := 0
			up := windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if stage == "settlement" {
					file.syncFailure = true
				}
				return windowTransportResponse("model"), nil
			})
			tr, e := newWindowProviderTransport(cap, "model", up)
			if e != nil {
				t.Fatal(e)
			}
			if stage == "intent" {
				file.syncFailure = true
			}
			if _, e = tr.RoundTrip(windowTransportRequest(t, "model")); e == nil {
				t.Fatal("durability failure returned response")
			}
			want := 0
			if stage == "settlement" {
				want = 1
			}
			if calls != want {
				t.Fatal(calls, want)
			}
			if _, e = tr.RoundTrip(windowTransportRequest(t, "model")); e == nil || calls != want {
				t.Fatal("subsequent call escaped", e, calls)
			}
			s := l.Snapshot()
			if s.Halt != "durability_failure" || s.ModelKnown != 0 {
				t.Fatal(s)
			}
			if stage == "settlement" && (s.Pending == nil || s.ModelHeld == 0) {
				t.Fatal("unknown charge lost", s)
			}
		})
	}
}

func TestWindowTransportConcurrentProvidersHaltWithoutSecondSend(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	calls := 0
	up := windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		close(started)
		<-release
		return windowTransportResponse("model"), nil
	})
	model, e := newWindowProviderTransport(cap, "model", up)
	if e != nil {
		t.Fatal(e)
	}
	judge, e := newWindowProviderTransport(cap, "jev", up)
	if e != nil {
		t.Fatal(e)
	}
	req := windowTransportRequest(t, "model")
	go func() { _, e := model.RoundTrip(req); done <- e }()
	<-started
	_, judgeErr := judge.RoundTrip(windowTransportRequest(t, "jev"))
	close(release)
	modelErr := <-done
	s := cap.ledger.Snapshot()
	if judgeErr == nil || modelErr == nil || calls != 1 || s.Halt != "concurrent_request" || s.ModelHeld == 0 || s.JEVAttempts != 0 {
		t.Fatal(judgeErr, modelErr, calls, s)
	}
}

type windowTransportRoundTripFunc func(*http.Request) (*http.Response, error)

type windowTransportPayloadCapability struct {
	*windowTransportTestCapability
	store    *windowPayloadStore
	required bool
}

func (c *windowTransportPayloadCapability) windowPayloadEvidence() (*windowPayloadStore, bool) {
	return c.store, c.required
}

func TestWindowTransportRequiredPayloadEvidenceRejectsMissingStore(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	wrapped := &windowTransportPayloadCapability{windowTransportTestCapability: cap, required: true}
	if _, e := newWindowProviderTransport(wrapped, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing evidence reached upstream")
		return nil, nil
	})); e == nil {
		t.Fatal("required payload evidence bypassed")
	}
}
func TestWindowTransportCapturesBadProtocolBodyBeforeHalt(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	dir := filepath.Join(t.TempDir(), "payloads")
	store, e := newWindowPayloadStore(dir, filepath.Join(t.TempDir(), "campaign"), cap.ledger.contract, "recorded_run")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	wrapped := &windowTransportPayloadCapability{windowTransportTestCapability: cap, store: store, required: true}
	raw := []byte(`{"model":"different-response-model","usage":{"input_tokens":2,"output_tokens":1}}`)
	tr, e := newWindowProviderTransport(wrapped, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		m, _, e := verifyWindowPayloadStore(store)
		if e != nil || len(m.Packets) != 1 || m.Packets[0].Response != nil {
			t.Fatal("request evidence missing before I/O", m, e)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tr.RoundTrip(windowTransportRequest(t, "model")); e == nil {
		t.Fatal("bad response accepted")
	}
	manifest, _, e := verifyWindowPayloadStore(store)
	if e != nil || len(manifest.Packets) != 1 || manifest.Packets[0].Response == nil {
		t.Fatal("original bad response lost", manifest, e)
	}
	body := manifest.Packets[0].Response.Body
	if body == nil || !body.Complete || body.FullSHA256 != conversationHash(raw) || body.PrefixSHA256 != "" {
		t.Fatal(body)
	}
	captured, e := os.ReadFile(filepath.Join(dir, body.File))
	if e != nil || !bytes.Equal(captured, raw) {
		t.Fatal("bad-response bytes changed", e)
	}
	if s := cap.ledger.Snapshot(); s.Pending == nil || s.ModelHeld == 0 || s.ModelKnown != 0 || s.Halt == "" {
		t.Fatal(s)
	}
}

func TestWindowTransportRequestPayloadSyncFailureStopsBeforeSend(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	store, e := newWindowPayloadStore(filepath.Join(t.TempDir(), "payloads"), filepath.Join(t.TempDir(), "campaign"), cap.ledger.contract, "recorded_run")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	store.fileWrap = func(f *os.File) windowPayloadFile { return &failingWindowFile{windowJournalFile: f, syncFailure: true} }
	wrapped := &windowTransportPayloadCapability{windowTransportTestCapability: cap, store: store, required: true}
	calls := 0
	tr, e := newWindowProviderTransport(wrapped, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return windowTransportResponse("model"), nil }))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tr.RoundTrip(windowTransportRequest(t, "model")); e == nil || calls != 0 {
		t.Fatal("unsynced payload sent", e, calls)
	}
	if s := cap.ledger.Snapshot(); s.Pending == nil || s.ModelHeld == 0 || s.Halt == "" {
		t.Fatal("intent/hold lost", s)
	}
}

func (f windowTransportRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func windowTransportFixture(t *testing.T) (*windowTransportTestCapability, string) {
	t.Helper()
	c := windowTestContract()
	c.Model = windowProtocolLimits("model")
	c.JEV = windowProtocolLimits("jev")
	c.JEV.OutputRate = c.JEV.InputRate
	c.ModelCap = 100000
	c.JEVCap = 100000
	dir := filepath.Join(t.TempDir(), "window")
	l, e := newWindowAccounting(dir, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	return &windowTransportTestCapability{ledger: l, calls: map[string]int{}}, dir
}
func windowTransportRequest(t *testing.T, provider string) *http.Request {
	t.Helper()
	body := windowProtocolModelBody
	if provider == "jev" {
		body = windowProtocolJEVBody
	}
	endpoint, _, e := windowProviderEndpoint(provider)
	if e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}
func windowTransportResponse(provider string) *http.Response {
	model := "deepseek-flash"
	if provider == "jev" {
		model = "jev-1.13.0"
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"model":%q,"usage":{"input_tokens":2,"output_tokens":1}}`, model)))}
}

func TestWindowTransportIntentBeforeOneCallAndUsageBeforeReturn(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			cap, dir := windowTransportFixture(t)
			calls := 0
			up := windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				a, e := readWindowAccounting(dir)
				if e != nil || a.State.Pending == nil {
					t.Fatal("upstream before persisted intent", a, e)
				}
				raw, _ := io.ReadAll(r.Body)
				if a.State.Pending.Request.BodySHA256 != conversationHash(raw) || r.GetBody != nil || r.ContentLength != int64(len(raw)) {
					t.Fatal("unfrozen/replayable request")
				}
				return windowTransportResponse(provider), nil
			})
			tr, e := newWindowProviderTransport(cap, provider, up)
			if e != nil {
				t.Fatal(e)
			}
			response, e := tr.RoundTrip(windowTransportRequest(t, provider))
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if !bytes.Contains(raw, []byte(`"input_tokens":2`)) {
				t.Fatal("response rewritten")
			}
			s := cap.ledger.Snapshot()
			if calls != 1 || s.Pending != nil || s.ModelHeld != 0 || s.JEVHeld != 0 || s.ModelKnown+s.JEVKnown <= 0 {
				t.Fatal(calls, s)
			}
		})
	}
}
func TestWindowTransportMissingCapabilityNeverCalls(t *testing.T) {
	calls := 0
	up := windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })
	if _, e := newWindowProviderTransport(nil, "model", up); e == nil {
		t.Fatal("nil capability accepted")
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

type windowTransportTrackedBody struct {
	io.Reader
	closed bool
}

func (b *windowTransportTrackedBody) Close() error { b.closed = true; return nil }
func TestWindowTransportRejectedRequestClosesBody(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	tr, e := newWindowProviderTransport(cap, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected external call"); return nil, nil }))
	if e != nil {
		t.Fatal(e)
	}
	req := windowTransportRequest(t, "model")
	body := &windowTransportTrackedBody{Reader: strings.NewReader(windowProtocolModelBody)}
	req.Body = body
	req.URL = nil
	if _, e = tr.RoundTrip(req); e == nil || !body.closed {
		t.Fatal("rejected request leaked body", e, body.closed)
	}
}
func TestWindowTransportTypedNilUpstreamRejected(t *testing.T) {
	cap, _ := windowTransportFixture(t)
	var up windowTransportRoundTripFunc
	if _, e := newWindowProviderTransport(cap, "model", up); e == nil {
		t.Fatal("typed nil upstream accepted")
	}
}
func TestWindowTransportInvalidRequestsHaltBeforeIntent(t *testing.T) {
	for _, name := range []string{"endpoint", "method", "body", "output", "idempotent", "host", "encoding"} {
		t.Run(name, func(t *testing.T) {
			cap, _ := windowTransportFixture(t)
			calls := 0
			tr, e := newWindowProviderTransport(cap, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return windowTransportResponse("model"), nil }))
			if e != nil {
				t.Fatal(e)
			}
			req := windowTransportRequest(t, "model")
			switch name {
			case "endpoint":
				req.URL.Host = "example.invalid"
			case "method":
				req.Method = "GET"
			case "body":
				req.Body = io.NopCloser(strings.NewReader("{}"))
			case "output":
				req.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(windowProtocolModelBody, "128", "129")))
			case "idempotent":
				req.Header.Set("Idempotency-Key", "retry")
			case "host":
				req.Host = "example.invalid"
			case "encoding":
				req.Header.Set("Content-Encoding", "gzip")
			}
			if _, e = tr.RoundTrip(req); e == nil {
				t.Fatal("invalid accepted")
			}
			s := cap.ledger.Snapshot()
			if calls != 0 || s.ModelAttempts != 0 || s.Halt == "" {
				t.Fatal(calls, s)
			}
		})
	}
}
func TestWindowTransportJEVFailureStopsModelFallback(t *testing.T) {
	for _, failure := range []string{"network", "http", "usage", "model", "excess", "cache-excess"} {
		t.Run(failure, func(t *testing.T) {
			cap, _ := windowTransportFixture(t)
			provider := "jev"
			if failure == "cache-excess" {
				provider = "model"
			}
			calls := 0
			up := windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				r := windowTransportResponse(provider)
				switch failure {
				case "network":
					return nil, fmt.Errorf("secret upstream detail")
				case "http":
					r.StatusCode = 503
				case "usage":
					r.Body = io.NopCloser(strings.NewReader(`{"model":"jev-1.13.0"}`))
				case "model":
					r.Body = io.NopCloser(strings.NewReader(`{"model":"alias","usage":{"input_tokens":2,"output_tokens":1}}`))
				case "excess":
					r.Body = io.NopCloser(strings.NewReader(`{"model":"jev-1.13.0","usage":{"input_tokens":1001,"output_tokens":1}}`))
				case "cache-excess":
					r.Body = io.NopCloser(strings.NewReader(`{"model":"deepseek-flash","usage":{"input_tokens":900,"output_tokens":1,"cache_creation_input_tokens":101}}`))
				}
				return r, nil
			})
			tr, e := newWindowProviderTransport(cap, provider, up)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = tr.RoundTrip(windowTransportRequest(t, provider)); e == nil {
				t.Fatal("failure hidden")
			}
			if strings.Contains(e.Error(), "secret") {
				t.Fatal("raw error leaked")
			}
			s := cap.ledger.Snapshot()
			if s.Pending == nil || s.ModelHeld+s.JEVHeld <= 0 || s.ModelKnown+s.JEVKnown != 0 || s.Halt == "" {
				t.Fatal(s)
			}
			if (failure == "excess" || failure == "cache-excess") && !s.EnvelopeViolation {
				t.Fatal("envelope incident unreported", s)
			}
			fallback, e := newWindowProviderTransport(cap, "model", up)
			if e == nil {
				fallback.RoundTrip(windowTransportRequest(t, "model"))
			}
			if calls != 1 {
				t.Fatal("fallback reached upstream", calls)
			}
		})
	}
}
