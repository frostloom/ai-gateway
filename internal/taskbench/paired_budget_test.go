package taskbench

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type pairedRoundTripFunc func(*http.Request) (*http.Response, error)

func (f pairedRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func pairedLimits() PairBudgetLimits {
	return PairBudgetLimits{MaxCalls: 2, MaxOutputTokens: 128, MaxRequestBytes: 2048, InputTokenCeiling: 4096, MaxMicroCNY: 100000, InputMicroCNYPerMillion: 1000000, OutputMicroCNYPerMillion: 2000000}
}
func pairedRequest() *http.Request {
	r, _ := http.NewRequest("POST", "https://provider.invalid/chat/completions", bytes.NewBufferString(`{"model":"test-model","max_tokens":128,"messages":[]}`))
	return r
}
func TestPairBudgetBeforeAttemptAndDurableResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.jsonl")
	calls := 0
	up := pairedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":100,"completion_tokens":10}}`))}, nil
	})
	b, err := OpenPairBudget(path, "contract", pairedLimits(), up)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, err := b.RoundTrip(pairedRequest())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	b.Close()
	b, err = OpenPairBudget(path, "contract", pairedLimits(), up)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.RoundTrip(pairedRequest()); err == nil || calls != 2 {
		t.Fatal("limit did not block upstream on resume")
	}
	if b.Snapshot().Calls != 2 || b.Snapshot().CostMicroCNY == nil || *b.Snapshot().CostMicroCNY != 240 {
		t.Fatalf("wrong actual priced usage: %+v", b.Snapshot())
	}
}
func TestPairBudgetUnknownUsageStopsAndKeepsReservation(t *testing.T) {
	calls := 0
	b, err := OpenPairBudget(filepath.Join(t.TempDir(), "b"), "contract", pairedLimits(), pairedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.RoundTrip(pairedRequest()); err == nil {
		t.Fatal("missing usage accepted")
	}
	if _, err := b.RoundTrip(pairedRequest()); err == nil || calls != 1 {
		t.Fatal("unknown charge allowed another call")
	}
	if b.Snapshot().ReservedMicroCNY <= 0 || b.Snapshot().CostMicroCNY != nil {
		t.Fatal("unknown cost converted to zero")
	}
}
func TestPairBudgetRejectsOversizedOutputAndInsufficientReservation(t *testing.T) {
	for _, kind := range []string{"output", "money", "body"} {
		t.Run(kind, func(t *testing.T) {
			limits := pairedLimits()
			if kind == "money" {
				limits.MaxMicroCNY = 1
			}
			if kind == "body" {
				limits.MaxRequestBytes = 1
			}
			calls := 0
			b, err := OpenPairBudget(filepath.Join(t.TempDir(), "b"), "contract", limits, pairedRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			r := pairedRequest()
			if kind == "output" {
				r.Body = io.NopCloser(strings.NewReader(`{"max_tokens":256}`))
			}
			if _, err := b.RoundTrip(r); err == nil || calls != 0 {
				t.Fatal("unsafe request reached upstream")
			}
		})
	}
}
func TestPairBudgetExclusiveLockAndConfigBinding(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b")
	b, err := OpenPairBudget(p, "contract", pairedLimits(), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPairBudget(p, "contract", pairedLimits(), http.DefaultTransport); err == nil {
		t.Fatal("concurrent budget owner")
	}
	b.Close()
	if _, err := OpenPairBudget(p, "changed", pairedLimits(), http.DefaultTransport); err == nil {
		t.Fatal("changed run conditions resumed")
	}
}

func TestPairBudgetInterruptedAttemptIsNeverResent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b")
	calls := 0
	up := pairedRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })
	b, e := OpenPairBudget(p, "contract", pairedLimits(), up)
	if e != nil {
		t.Fatal(e)
	}
	ev := pairBudgetEvent{Kind: "reserve", ID: 1, RequestSHA: strings.Repeat("a", 64)}
	if e = b.write(ev); e != nil {
		t.Fatal(e)
	}
	if e = b.apply(ev); e != nil {
		t.Fatal(e)
	}
	b.Close()
	b, e = OpenPairBudget(p, "contract", pairedLimits(), up)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if _, e = b.RoundTrip(pairedRequest()); e == nil || calls != 0 || b.Snapshot().CostMicroCNY != nil {
		t.Fatal("interrupted charge retried or treated as zero")
	}
}

func TestPairBudgetAnthropicCacheCountsAndOutOfBoundsUsage(t *testing.T) {
	in, out, e := pairUsage([]byte(`{"usage":{"input_tokens":100,"output_tokens":12,"cache_read_input_tokens":20,"cache_creation_input_tokens":30}}`))
	if e != nil || in != 150 || out != 12 {
		t.Fatal("anthropic cache usage not priced at declared worst-case rate")
	}
	b, e := OpenPairBudget(filepath.Join(t.TempDir(), "b"), "contract", pairedLimits(), pairedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":999999,"completion_tokens":1}}`))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if _, e = b.RoundTrip(pairedRequest()); e == nil || b.Snapshot().Stopped == "" {
		t.Fatal("provider exceeded ceiling without halt")
	}
}
