package taskbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Rates must be explicit worst-case billing rates (including cache creation).
// Reservations are an operator-declared charge envelope, not a provider invoice
// guarantee. Actual usage outside it or missing usage halts all later attempts.
type PairBudgetLimits struct {
	MaxCalls                 int   `json:"max_calls"`
	MaxOutputTokens          int   `json:"max_output_tokens"`
	MaxRequestBytes          int   `json:"max_request_bytes"`
	InputTokenCeiling        int64 `json:"input_token_ceiling"`
	MaxMicroCNY              int64 `json:"max_micro_cny"`
	InputMicroCNYPerMillion  int64 `json:"input_micro_cny_per_million"`
	OutputMicroCNYPerMillion int64 `json:"output_micro_cny_per_million"`
}
type PairBudgetState struct {
	Calls            int    `json:"attempts"`
	ReservedMicroCNY int64  `json:"reserved_micro_cny"`
	CostMicroCNY     *int64 `json:"priced_usage_micro_cny"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	Stopped          string `json:"stopped,omitempty"`
}
type pairBudgetEvent struct {
	Kind          string            `json:"kind"`
	Contract      string            `json:"contract,omitempty"`
	Limits        *PairBudgetLimits `json:"limits,omitempty"`
	ID            int               `json:"id,omitempty"`
	RequestSHA    string            `json:"request_sha256,omitempty"`
	Input, Output int64
	Stop          string `json:"stop,omitempty"`
}
type PairBudgetTransport struct {
	mu       sync.Mutex
	file     *os.File
	lock     *os.File
	path     string
	contract string
	limits   PairBudgetLimits
	upstream http.RoundTripper
	state    PairBudgetState
	inflight bool
	closed   bool
}

func (b *PairBudgetTransport) identity() string {
	path, _ := filepath.Abs(b.path)
	return conversationHash([]byte(filepath.Clean(path)))
}

func (l PairBudgetLimits) validate() error {
	if l.MaxCalls < 1 || l.MaxCalls > 256 || l.MaxOutputTokens < 1 || l.MaxOutputTokens > 512 || l.MaxRequestBytes < 1 || l.MaxRequestBytes > 1<<20 || l.InputTokenCeiling < 1 || l.InputTokenCeiling > 10000000 || l.MaxMicroCNY < 1 || l.MaxMicroCNY > 1000000000 || l.InputMicroCNYPerMillion < 1 || l.InputMicroCNYPerMillion > 1000000000 || l.OutputMicroCNYPerMillion < 1 || l.OutputMicroCNYPerMillion > 1000000000 {
		return fmt.Errorf("explicit bounded calls/tokens/bytes/currency and nonzero worst-case rates required")
	}
	return nil
}
func (l PairBudgetLimits) cost(in, out int64) int64 {
	return (in*l.InputMicroCNYPerMillion+999999)/1000000 + (out*l.OutputMicroCNYPerMillion+999999)/1000000
}
func (b *PairBudgetTransport) reserveCost() int64 {
	return b.limits.cost(b.limits.InputTokenCeiling, int64(b.limits.MaxOutputTokens))
}
func OpenPairBudget(path, contract string, limits PairBudgetLimits, upstream http.RoundTripper) (*PairBudgetTransport, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if contract == "" || upstream == nil {
		return nil, fmt.Errorf("explicit execution contract and transport required")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("budget exclusive lock: %w", err)
	}
	b := &PairBudgetTransport{path: path, contract: contract, lock: lock, limits: limits, upstream: upstream}
	zero := int64(0)
	b.state.CostMicroCNY = &zero
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		b.Close()
		return nil, err
	}
	b.file = f
	fail := func(e error) (*PairBudgetTransport, error) { b.Close(); return nil, e }
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1<<20)
	lines := 0
	for s.Scan() {
		var ev pairBudgetEvent
		if json.Unmarshal(s.Bytes(), &ev) != nil {
			return fail(fmt.Errorf("invalid budget journal; no automatic retry"))
		}
		if lines == 0 {
			if ev.Kind != "config" || ev.Contract != contract || ev.Limits == nil || *ev.Limits != limits {
				return fail(fmt.Errorf("budget contract changed"))
			}
		} else if err = b.apply(ev); err != nil {
			return fail(err)
		}
		lines++
	}
	if err = s.Err(); err != nil {
		return fail(err)
	}
	if lines == 0 {
		if err = b.write(pairBudgetEvent{Kind: "config", Contract: contract, Limits: &limits}); err != nil {
			return fail(err)
		}
	}
	if b.inflight {
		b.state.Stopped = "interrupted attempt has unknown charge; manual reconciliation required"
		b.state.CostMicroCNY = nil
	}
	return b, nil
}
func (b *PairBudgetTransport) apply(ev pairBudgetEvent) error {
	switch ev.Kind {
	case "reserve":
		if b.inflight || b.state.Stopped != "" || ev.ID != b.state.Calls+1 || ev.RequestSHA == "" || b.state.Calls >= b.limits.MaxCalls || b.state.ReservedMicroCNY > b.limits.MaxMicroCNY-b.reserveCost() {
			return fmt.Errorf("invalid reservation sequence")
		}
		b.state.Calls++
		b.state.ReservedMicroCNY += b.reserveCost()
		b.inflight = true
	case "settle":
		if !b.inflight || ev.ID != b.state.Calls {
			return fmt.Errorf("invalid settlement sequence")
		}
		b.inflight = false
		if ev.Stop != "" {
			b.state.Stopped = ev.Stop
			b.state.CostMicroCNY = nil
		} else {
			if ev.Input < 0 || ev.Output < 0 || ev.Input > b.limits.InputTokenCeiling || ev.Output > int64(b.limits.MaxOutputTokens) {
				return fmt.Errorf("out-of-bound settlement")
			}
			b.state.InputTokens += ev.Input
			b.state.OutputTokens += ev.Output
			*b.state.CostMicroCNY += b.limits.cost(ev.Input, ev.Output)
		}
	default:
		return fmt.Errorf("unknown budget journal event")
	}
	return nil
}
func (b *PairBudgetTransport) write(ev pairBudgetEvent) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err = b.file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return b.file.Sync()
}
func (b *PairBudgetTransport) Snapshot() PairBudgetState {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state
	if s.CostMicroCNY != nil {
		v := *s.CostMicroCNY
		s.CostMicroCNY = &v
	}
	return s
}
func (b *PairBudgetTransport) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	var err error
	if b.file != nil {
		err = b.file.Close()
	}
	if b.lock != nil {
		b.lock.Close()
		os.Remove(b.path + ".lock")
	}
	return err
}
func (b *PairBudgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.state.Stopped != "" || b.inflight {
		return nil, fmt.Errorf("budget stopped: %s", b.state.Stopped)
	}
	if req.Method != "POST" || req.URL == nil || req.URL.User != nil || req.URL.RawQuery != "" || req.Body == nil {
		return nil, fmt.Errorf("bounded JSON POST required")
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, int64(b.limits.MaxRequestBytes)+1))
	req.Body.Close()
	if err != nil || len(raw) > b.limits.MaxRequestBytes {
		return nil, fmt.Errorf("request bytes exceed bound")
	}
	var payload struct {
		MaxTokens int  `json:"max_tokens"`
		Stream    bool `json:"stream"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.MaxTokens < 1 || payload.MaxTokens > b.limits.MaxOutputTokens || payload.Stream {
		return nil, fmt.Errorf("explicit nonstream output limit required")
	}
	if b.state.Calls >= b.limits.MaxCalls || b.state.ReservedMicroCNY > b.limits.MaxMicroCNY-b.reserveCost() {
		return nil, fmt.Errorf("global attempt/reserved currency budget exhausted")
	}
	ev := pairBudgetEvent{Kind: "reserve", ID: b.state.Calls + 1, RequestSHA: conversationHash(raw)}
	if err = b.write(ev); err != nil {
		b.state.Stopped = "reservation could not be persisted"
		return nil, err
	}
	if err = b.apply(ev); err != nil {
		return nil, err
	}
	copyReq := req.Clone(req.Context())
	copyReq.Body = io.NopCloser(bytes.NewReader(raw))
	copyReq.GetBody = nil
	resp, upErr := b.upstream.RoundTrip(copyReq)
	settle := pairBudgetEvent{Kind: "settle", ID: b.state.Calls}
	var body []byte
	if upErr != nil || resp == nil || resp.Body == nil {
		settle.Stop = "provider transport failed; unknown charge"
	} else {
		body, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		resp.Body.Close()
		if err != nil || len(body) > 1<<20 || resp.StatusCode != 200 {
			settle.Stop = "provider response failed; unknown charge"
		} else {
			settle.Input, settle.Output, err = pairUsage(body)
			if err != nil {
				settle.Stop = "provider usage missing or invalid"
			} else if settle.Input > b.limits.InputTokenCeiling || settle.Output > int64(b.limits.MaxOutputTokens) {
				settle.Stop = "provider usage exceeds declared charge envelope"
			}
		}
	}
	if err = b.write(settle); err != nil {
		b.state.Stopped = "settlement could not be persisted"
		b.state.CostMicroCNY = nil
		return nil, err
	}
	if err = b.apply(settle); err != nil {
		return nil, err
	}
	if settle.Stop != "" {
		return nil, fmt.Errorf("budget stopped: %s", settle.Stop)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}
func pairUsage(body []byte) (int64, int64, error) {
	var response struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &response) != nil {
		return 0, 0, fmt.Errorf("invalid usage")
	}
	count := func(key string) (int64, error) {
		var n *int64
		if json.Unmarshal(response.Usage[key], &n) != nil || n == nil || *n < 0 || *n > 10000000 {
			return 0, fmt.Errorf("missing usage %s", key)
		}
		return *n, nil
	}
	inkey, outkey := "prompt_tokens", "completion_tokens"
	if _, ok := response.Usage["input_tokens"]; ok {
		inkey, outkey = "input_tokens", "output_tokens"
	}
	in, e := count(inkey)
	if e != nil {
		return 0, 0, e
	}
	out, e := count(outkey)
	if e != nil {
		return 0, 0, e
	}
	if inkey == "input_tokens" {
		for _, key := range []string{"cache_read_input_tokens", "cache_creation_input_tokens"} {
			if _, ok := response.Usage[key]; ok {
				n, e := count(key)
				if e != nil {
					return 0, 0, e
				}
				in += n
			}
		}
	}
	return in, out, nil
}
