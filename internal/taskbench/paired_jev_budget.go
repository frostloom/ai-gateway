package taskbench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/frostloom/ai-gateway/internal/jev"
)

type pairJEVEvent struct {
	Kind, Contract, Version string
	Limit, ID               int
	Failed                  bool
}

type PairJEVBudgetState struct {
	IdentitySHA256 string `json:"identity_sha256"`
	Limit          int    `json:"limit"`
	Attempts       int    `json:"attempts"`
	Stopped        bool   `json:"stopped"`
}

func (b *boundedPairJEV) Snapshot() PairJEVBudgetState {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, _ := filepath.Abs(b.path)
	raw, _ := json.Marshal([]any{filepath.Clean(p), b.contract, b.version, b.max})
	return PairJEVBudgetState{conversationHash(raw), b.max, b.calls, b.stopped}
}

type boundedPairJEV struct {
	mu                      sync.Mutex
	calls, max              int
	version, contract, path string
	file, lock              *os.File
	factory                 func() jev.Evaluator
	evaluator               jev.Evaluator
	stopped, closed         bool
}

func openPairJEVGuard(path, contract string, max int, version string, factory func() jev.Evaluator) (*boundedPairJEV, error) {
	if max < 1 || max > 256 || contract == "" || factory == nil {
		return nil, fmt.Errorf("explicit bounded JEV configuration required")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	b := &boundedPairJEV{max: max, version: version, contract: contract, path: path, lock: lock, factory: factory}
	fail := func(err error) (*boundedPairJEV, error) { return nil, errors.Join(err, b.Close()) }
	b.file, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fail(err)
	}
	s := bufio.NewScanner(b.file)
	s.Buffer(make([]byte, 4096), 1<<20)
	line := 0
	pending := false
	for s.Scan() {
		var e pairJEVEvent
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return fail(fmt.Errorf("invalid JEV journal"))
		}
		if line == 0 {
			if e.Kind != "config" || e.Contract != contract || e.Version != version || e.Limit != max {
				return fail(fmt.Errorf("JEV journal contract differs"))
			}
		} else if e.Kind == "reserve" && !pending && !b.stopped && e.ID == b.calls+1 && b.calls < max {
			b.calls++
			pending = true
		} else if e.Kind == "settle" && pending && e.ID == b.calls {
			pending = false
			b.stopped = e.Failed
		} else {
			return fail(fmt.Errorf("JEV journal sequence differs"))
		}
		line++
	}
	if err = s.Err(); err != nil {
		return fail(err)
	}
	if line == 0 {
		if err = b.write(pairJEVEvent{Kind: "config", Contract: contract, Version: version, Limit: max}); err != nil {
			return fail(err)
		}
	}
	if pending {
		b.stopped = true
	}
	return b, nil
}
func (b *boundedPairJEV) write(e pairJEVEvent) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = b.file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return b.file.Sync()
}
func (b *boundedPairJEV) Evaluate(ctx context.Context, state any, q map[string]jev.Question) (*jev.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.stopped || b.calls >= b.max {
		return nil, fmt.Errorf("JEV stopped or attempt limit exhausted")
	}
	if b.evaluator == nil {
		b.evaluator = b.factory()
		if b.evaluator == nil {
			return nil, fmt.Errorf("missing JEV evaluator")
		}
	}
	if err := b.write(pairJEVEvent{Kind: "reserve", ID: b.calls + 1}); err != nil {
		b.stopped = true
		return nil, fmt.Errorf("JEV reservation unavailable")
	}
	b.calls++
	r, err := b.evaluator.Evaluate(ctx, state, q)
	if err == nil && (r == nil || (b.version != "" && r.Model != b.version)) {
		err = fmt.Errorf("JEV version/result differs")
	}
	if e := b.write(pairJEVEvent{Kind: "settle", ID: b.calls, Failed: err != nil}); e != nil {
		b.stopped = true
		return nil, fmt.Errorf("JEV settlement unavailable")
	}
	if err != nil {
		b.stopped = true
		return nil, fmt.Errorf("JEV execution failed; unknown charge")
	}
	return r, nil
}
func (b *boundedPairJEV) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	var errs []error
	if b.file != nil {
		errs = append(errs, b.file.Close())
	}
	if b.lock != nil {
		errs = append(errs, b.lock.Close(), os.Remove(b.path+".lock"))
	}
	return errors.Join(errs...)
}
