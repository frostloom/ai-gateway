package knowledgepilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type taskKey struct{}

func WithTask(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, taskKey{}, id)
}

type Transport struct {
	mu       sync.Mutex
	upstream http.RoundTripper
	claim    *Claim
	limits   Limits
	journal  *Journal
	stopped  bool
	count    int
	perTask  map[string]int
}

func NewTransport(up http.RoundTripper, c *Claim, l Limits, j *Journal) *Transport {
	return &Transport{upstream: up, claim: c, limits: l, journal: j, perTask: map[string]int{}}
}
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.claim == nil || !t.claim.local || t.journal == nil || t.upstream == nil || t.limits != t.claim.scope.Limits {
		return nil, fmt.Errorf("attempt capability unavailable")
	}
	task, _ := req.Context().Value(taskKey{}).(string)
	if task == "" || req.Method != "POST" || req.URL.String() != strings.TrimRight(t.claim.endpoint, "/")+"/chat/completions" || !literalLoopback(t.claim.endpoint) {
		return nil, fmt.Errorf("unbound task/endpoint")
	}
	if t.count >= t.limits.Total || t.perTask[task] >= t.limits.PerTask {
		return nil, fmt.Errorf("attempt bound reached")
	}
	if req.Body == nil {
		return nil, fmt.Errorf("missing payload")
	}
	raw, e := io.ReadAll(io.LimitReader(req.Body, int64(t.limits.RequestBytes)+1))
	req.Body.Close()
	if e != nil || len(raw) > t.limits.RequestBytes {
		return nil, fmt.Errorf("payload exceeds bound")
	}
	var cfg struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
	}
	if json.Unmarshal(raw, &cfg) != nil || cfg.Model != t.claim.scope.Model || cfg.MaxTokens != t.limits.Output {
		return nil, fmt.Errorf("payload contract differs")
	}
	t.count++
	t.perTask[task]++
	ev := Event{Kind: "reserved", TaskID: task, Sequence: t.count, RequestSHA: SHA(raw), Request: raw}
	if e = t.journal.Append(ev); e != nil {
		t.stopped = true
		return nil, e
	}
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(raw))
	clone.ContentLength = int64(len(raw))
	start := time.Now()
	resp, e := t.upstream.RoundTrip(clone)
	ev.Kind = "failed"
	ev.DurationMS = float64(time.Since(start)) / float64(time.Millisecond)
	ev.Request = nil
	fail := func(kind string, err error) (*http.Response, error) {
		t.stopped = true
		ev.Failure = kind
		if w := t.journal.Append(ev); w != nil {
			return nil, w
		}
		return nil, err
	}
	if e != nil {
		return fail("transport", fmt.Errorf("local upstream transport failed"))
	}
	if resp == nil || resp.Body == nil {
		return fail("response", fmt.Errorf("missing response"))
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	resp.Body.Close()
	ev.Status = resp.StatusCode
	if e != nil || len(body) > 1<<20 {
		return fail("response", fmt.Errorf("response exceeds bound"))
	}
	if json.Valid(body) {
		ev.Response = body
	}
	if resp.StatusCode != 200 {
		return fail("http_status", fmt.Errorf("local provider HTTP %d", resp.StatusCode))
	}
	a, e := NormalizeOpenAI(body)
	if e != nil {
		return fail("usage", e)
	}
	ev.Kind = "completed"
	ev.Attempt = &a
	if e = t.journal.Append(ev); e != nil {
		t.stopped = true
		return nil, e
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// This normalization is limited to explicit OpenAI-style cache details. Missing
// usage is unknown, never zero. It cannot verify tariffs or authorize a call.
func NormalizeOpenAI(b []byte) (knowledgeeval.Attempt, error) {
	var x struct {
		Usage *struct {
			Input   *int64 `json:"prompt_tokens"`
			Output  *int64 `json:"completion_tokens"`
			Details *struct {
				Cached *int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	var a knowledgeeval.Attempt
	if json.Unmarshal(b, &x) != nil || x.Usage == nil || x.Usage.Input == nil || x.Usage.Output == nil || x.Usage.Details == nil || x.Usage.Details.Cached == nil {
		return a, fmt.Errorf("usage incomplete")
	}
	u := x.Usage
	for _, n := range []int64{*u.Input, *u.Output, *u.Details.Cached} {
		if n < 0 || n > 1e9 {
			return a, fmt.Errorf("invalid usage")
		}
	}
	if *u.Details.Cached > *u.Input {
		return a, fmt.Errorf("cache exceeds input")
	}
	uncached := *u.Input - *u.Details.Cached
	zero := int64(0)
	a = knowledgeeval.Attempt{Outcome: "success", Currency: "CNY", Input: &uncached, Output: u.Output, CacheRead: u.Details.Cached, CacheCreation: &zero}
	return a, nil
}
