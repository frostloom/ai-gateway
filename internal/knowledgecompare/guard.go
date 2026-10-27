package knowledgecompare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type turnKey struct{}
type turnID struct {
	Task string
	Turn int
}

func WithTurn(ctx context.Context, task string, turn int) context.Context {
	return context.WithValue(ctx, turnKey{}, turnID{task, turn})
}

type Event struct {
	Kind                               string          `json:"kind"`
	Task                               string          `json:"task"`
	Turn                               int             `json:"turn"`
	Sequence                           int             `json:"sequence"`
	Hold                               int64           `json:"max_hold_micro_cny,omitempty"`
	RequestSHA                         string          `json:"request_sha256,omitempty"`
	Request                            json.RawMessage `json:"request,omitempty"`
	Response                           json.RawMessage `json:"response,omitempty"`
	Input, Output, CacheHit, CacheMiss int64
	UpperCost                          int64   `json:"upper_cost_micro_cny,omitempty"`
	ElapsedMS                          float64 `json:"elapsed_ms,omitempty"`
	Failure                            string  `json:"failure,omitempty"`
}
type GuardState struct {
	Calls                  int
	KnownUpper, Hold       int64
	Stopped, UnknownCharge bool
}
type Guard struct {
	mu        sync.Mutex
	c         Contract
	cap       *Capability
	file      *os.File
	upstream  http.RoundTripper
	closed    bool
	events    []Event
	state     GuardState
	turnCalls map[turnID]int
}

func NewGuard(path string, cap *Capability, upstream http.RoundTripper) (*Guard, error) {
	if cap == nil || upstream == nil {
		return nil, fmt.Errorf("explicit capability and transport required")
	}
	if e := cap.consume(); e != nil {
		return nil, e
	}
	if e := writeNew(path, nil); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return nil, e
	}
	return &Guard{c: cap.c, cap: cap, file: f, upstream: upstream, turnCalls: map[turnID]int{}}, nil
}
func (g *Guard) append(e Event) error {
	if g.closed {
		return fmt.Errorf("journal closed")
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	n, err := g.file.Write(append(b, '\n'))
	if err != nil {
		return err
	}
	if n != len(b)+1 {
		return fmt.Errorf("short event write")
	}
	if err = g.file.Sync(); err != nil {
		return err
	}
	g.events = append(g.events, e)
	return nil
}
func (g *Guard) Events() []Event {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, _ := json.Marshal(g.events)
	var e []Event
	json.Unmarshal(b, &e)
	return e
}
func (g *Guard) State() GuardState { g.mu.Lock(); defer g.mu.Unlock(); return g.state }
func (g *Guard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	return g.file.Close()
}
func price(input, output int64, c Contract) int64 {
	return (input*c.InputRate+999999)/1000000 + (output*c.OutputRate+999999)/1000000
}
func (g *Guard) RoundTrip(req *http.Request) (*http.Response, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id, _ := req.Context().Value(turnKey{}).(turnID)
	if g.closed || g.state.Stopped || id.Task == "" || id.Turn < 1 || id.Turn > 3 || req.Method != "POST" || req.URL.String() != ProviderURL {
		return nil, fmt.Errorf("request capability or turn rejected")
	}
	if g.state.Calls >= g.c.MaxCalls || g.turnCalls[id] >= 5 {
		g.state.Stopped = true
		return nil, fmt.Errorf("attempt limit reached")
	}
	hold := price(g.c.InputCeiling, int64(g.c.MaxOutput), g.c)
	if g.state.KnownUpper+hold > g.c.MaxMicroCNY {
		g.state.Stopped = true
		return nil, fmt.Errorf("independent budget cannot admit next maximum hold")
	}
	if req.Body == nil {
		return nil, fmt.Errorf("payload missing")
	}
	raw, e := io.ReadAll(io.LimitReader(req.Body, int64(g.c.MaxBytes)+1))
	req.Body.Close()
	if e != nil || len(raw) > g.c.MaxBytes {
		g.state.Stopped = true
		return nil, fmt.Errorf("request exceeds declared byte bound")
	}
	var body struct {
		Model    string `json:"model"`
		Max      int    `json:"max_tokens"`
		Thinking struct {
			Type string `json:"type"`
		} `json:"thinking"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Model != "deepseek-flash" || body.Max != 512 || body.Thinking.Type != "disabled" {
		g.state.Stopped = true
		return nil, fmt.Errorf("serialized provider configuration rejected")
	}
	g.state.Calls++
	g.turnCalls[id]++
	g.state.Hold = hold
	ev := Event{Kind: "intent", Task: id.Task, Turn: id.Turn, Sequence: g.state.Calls, Hold: hold, RequestSHA: knowledgepilot.SHA(raw), Request: raw}
	if e = g.append(ev); e != nil {
		g.state.Stopped = true
		return nil, e
	}
	start := time.Now()
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(raw))
	clone.ContentLength = int64(len(raw))
	resp, e := g.upstream.RoundTrip(clone)
	fail := func(kind string) (*http.Response, error) {
		g.state.Stopped = true
		g.state.UnknownCharge = true
		ev.Kind = "halt"
		ev.Request = nil
		ev.Failure = kind
		ev.ElapsedMS = float64(time.Since(start)) / float64(time.Millisecond)
		if err := g.append(ev); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("provider attempt halted: %s", kind)
	}
	if e != nil {
		return fail("transport")
	}
	if resp == nil || resp.Body == nil {
		return fail("empty_response")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	resp.Body.Close()
	if e != nil || len(data) > 1<<20 {
		return fail("response_body")
	}
	if json.Valid(data) {
		ev.Response = data
	}
	if resp.StatusCode != 200 {
		return fail("http_status")
	}
	input, output, hit, miss, e := Usage(data)
	if e != nil || input > g.c.InputCeiling || output > int64(g.c.MaxOutput) {
		return fail("usage_unknown_or_excess")
	}
	ev.Kind = "usage"
	ev.Request = nil
	ev.Hold = 0
	ev.Input, ev.Output, ev.CacheHit, ev.CacheMiss = input, output, hit, miss
	ev.UpperCost = price(input, output, g.c)
	ev.ElapsedMS = float64(time.Since(start)) / float64(time.Millisecond)
	if e = g.append(ev); e != nil {
		g.state.Stopped = true
		g.state.UnknownCharge = true
		return nil, e
	}
	g.state.KnownUpper += ev.UpperCost
	g.state.Hold = 0
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}
func Usage(b []byte) (input, output, hit, miss int64, err error) {
	var r struct {
		Usage *struct {
			Input   *int64 `json:"prompt_tokens"`
			Output  *int64 `json:"completion_tokens"`
			Hit     *int64 `json:"prompt_cache_hit_tokens"`
			Miss    *int64 `json:"prompt_cache_miss_tokens"`
			Details *struct {
				Cached *int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
		Choices []json.RawMessage `json:"choices"`
	}
	bad := func() (int64, int64, int64, int64, error) {
		return 0, 0, 0, 0, fmt.Errorf("missing or invalid native usage")
	}
	if json.Unmarshal(b, &r) != nil || r.Usage == nil || r.Usage.Input == nil || r.Usage.Output == nil || len(r.Choices) != 1 {
		return bad()
	}
	var choice struct {
		Message struct {
			Role    string  `json:"role"`
			Content *string `json:"content"`
			Tools   []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	}
	if json.Unmarshal(r.Choices[0], &choice) != nil || choice.Message.Role != "assistant" || (choice.Message.Content == nil && len(choice.Message.Tools) == 0) {
		return bad()
	}
	for _, tc := range choice.Message.Tools {
		if tc.ID == "" || tc.Type != "function" || tc.Function.Name == "" || !json.Valid([]byte(tc.Function.Arguments)) {
			return bad()
		}
	}
	u := r.Usage
	input, output = *u.Input, *u.Output
	if u.Hit != nil && u.Miss != nil {
		hit, miss = *u.Hit, *u.Miss
	} else if u.Details != nil && u.Details.Cached != nil {
		hit = *u.Details.Cached
		miss = input - hit
	} else {
		return bad()
	}
	if input < 0 || output < 0 || hit < 0 || miss < 0 || input > 1048576 || output > 512 || hit+miss != input {
		return bad()
	}
	return input, output, hit, miss, nil
}
