package taskbench

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// The stopped actual response carried this standard-tier usage shape. This
// fixture contains no credential, request, tenant or supplier message ID.
const windowStandardTierResponse = `{"model":"deepseek-flash","usage":{"input_tokens":232,"cache_creation_input_tokens":0,"cache_read_input_tokens":1536,"output_tokens":37,"service_tier":"standard"}}`

func TestWindowProtocolStandardServiceTier(t *testing.T) {
	p := windowProtocolLimits("model")
	p.MaxInput = 36864
	u, err := parseWindowResponseUsage("model", []byte(windowStandardTierResponse), p)
	if err != nil {
		t.Fatal(err)
	}
	if *u.Input != 1768 || *u.Output != 37 || *u.CacheHit != 1536 || *u.CacheMiss != 232 {
		t.Fatalf("usage normalization changed: %+v", u)
	}
}

func TestWindowProtocolRejectUnfundedOrMalformedServiceTier(t *testing.T) {
	for _, tier := range []string{`null`, `1`, `true`, `{}`, `[]`, `""`, `"priority"`, `"batch"`, `"unknown"`} {
		raw := strings.Replace(windowStandardTierResponse, `"standard"`, tier, 1)
		p := windowProtocolLimits("model")
		p.MaxInput = 36864
		if _, err := parseWindowResponseUsage("model", []byte(raw), p); err == nil {
			t.Errorf("unfunded/malformed tier accepted: %s", tier)
		}
	}
	for _, provider := range []string{"model", "jev"} {
		raw := `{"model":"` + windowProtocolLimits(provider).Model + `","usage":{"input_tokens":1,"output_tokens":1,"service_tier":"standard"`
		if provider == "model" {
			raw += `,"unrecognized_usage":1`
		}
		raw += `}}`
		if _, err := parseWindowResponseUsage(provider, []byte(raw), windowProtocolLimits(provider)); err == nil {
			t.Fatalf("unknown usage contract accepted for %s", provider)
		}
	}
}

func TestWindowTransportStandardServiceTierSettlesOnce(t *testing.T) {
	c := windowTestContract()
	c.Model = windowProtocolLimits("model")
	c.Model.MaxInput = 36864
	c.JEV = windowProtocolLimits("jev")
	c.ModelCap, c.JEVCap = 100000, 100000
	dir := filepath.Join(t.TempDir(), "ledger")
	l, err := newWindowAccounting(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	cap := &windowTransportTestCapability{ledger: l, calls: map[string]int{}}
	calls := 0
	tr, err := newWindowProviderTransport(cap, "model", windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		r := windowTransportResponse("model")
		r.Body = io.NopCloser(strings.NewReader(windowStandardTierResponse))
		return r, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	r, err := tr.RoundTrip(windowTransportRequest(t, "model"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil || string(body) != windowStandardTierResponse {
		t.Fatal("provider bytes changed", err)
	}
	s := l.Snapshot()
	if calls != 1 || s.ModelAttempts != 1 || s.Pending != nil || s.ModelHeld != 0 || s.ModelKnown != 3832 || s.Halt != "" {
		t.Fatalf("settlement or attempt identity changed: calls=%d state=%+v", calls, s)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	audit, err := readWindowAccounting(dir)
	if err != nil || audit.State.ModelKnown != 3832 || audit.State.Pending != nil || audit.State.Halt != "" {
		t.Fatal("durable settlement mismatch", err, audit.State)
	}
}
