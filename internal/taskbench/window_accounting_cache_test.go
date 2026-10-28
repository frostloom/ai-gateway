package taskbench

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func windowCacheContract() windowAccountingContract {
	c := windowTestContract()
	c.Schema = "full-window-accounting.v2"
	c.ModelCap = 200000
	c.Model.MaxInput = 36864
	c.Model.MaxOutput = 128
	c.Model.InputRate = 2000000
	c.Model.OutputRate = 8000000
	c.Model.CacheHitInputRate = 40000
	return c
}
func windowCacheSample() *windowUsage {
	return &windowUsage{Input: windowInt(1757), Output: windowInt(33), CacheHit: windowInt(1536), CacheMiss: windowInt(221)}
}
func TestWindowAccountingCacheSampleAndVersions(t *testing.T) {
	c := windowCacheContract()
	if got, err := c.Model.settlementCost(windowCacheSample()); err != nil || got != 768 {
		t.Fatalf("split cost=%d err=%v; want768", got, err)
	}
	if err := c.validate(); err != nil {
		t.Fatal("schema2 rejected", err)
	}
	if got := c.Model.cost(c.Model.MaxInput, c.Model.MaxOutput); got != 74752 {
		t.Fatal("hold changed", got)
	}
	old := c
	old.Schema = "full-window-accounting.v1"
	old.Model.CacheHitInputRate = 0
	if err := old.validate(); err != nil {
		t.Fatal(err)
	}
	got, err := old.Model.settlementCost(windowCacheSample())
	if err != nil || got != 3778 {
		t.Fatal(got, err)
	}
	raw, _ := json.Marshal(old)
	if bytes.Contains(raw, []byte("cache_hit_input")) {
		t.Fatal("legacy canonical changed", string(raw))
	}
	old.Model.CacheHitInputRate = 40000
	if old.validate() == nil {
		t.Fatal("legacy accepted new rate")
	}
}
func TestWindowAccountingCacheLiveReplay(t *testing.T) {
	c := windowCacheContract()
	dir := filepath.Join(t.TempDir(), "window")
	l, err := newWindowAccounting(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := l.Begin(windowTestRequest("model"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Snapshot().ModelHeld != 74752 {
		t.Fatal(l.Snapshot())
	}
	if err = l.Settle(ticket, strings.Repeat("f", 64), windowCacheSample()); err != nil {
		t.Fatal(err)
	}
	if l.Snapshot().ModelKnown != 768 {
		t.Fatal(l.Snapshot())
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	audit, err := readWindowAccounting(dir)
	if err != nil {
		t.Fatal(err)
	}
	if audit.State.ModelKnown != 768 || audit.State.ModelHeld != 0 || !audit.State.Closed {
		t.Fatal(audit)
	}
}

func TestWindowAccountingCacheSchemaBounds(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		model, jev   int64
		valid        bool
	}{
		{"old-zero", "full-window-accounting.v1", 0, 0, true},
		{"new-positive", "full-window-accounting.v2", 40000, 0, true},
		{"new-equal", "full-window-accounting.v2", 2000000, 0, true},
		{"old-positive", "full-window-accounting.v1", 1, 0, false},
		{"old-negative", "full-window-accounting.v1", -1, 0, false},
		{"new-zero", "full-window-accounting.v2", 0, 0, false},
		{"new-negative", "full-window-accounting.v2", -1, 0, false},
		{"new-excess", "full-window-accounting.v2", 2000001, 0, false},
		{"new-jev", "full-window-accounting.v2", 40000, 1, false},
		{"old-jev", "full-window-accounting.v1", 0, -1, false},
		{"unknown", "full-window-accounting.v3", 40000, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := windowCacheContract()
			c.Schema = tc.schema
			c.Model.CacheHitInputRate = tc.model
			c.JEV.CacheHitInputRate = tc.jev
			if (c.validate() == nil) != tc.valid {
				t.Fatal("version/rate guard", c)
			}
			if !tc.valid {
				dir := filepath.Join(t.TempDir(), "refused")
				if l, err := newWindowAccounting(dir, c); err == nil {
					l.Close()
					t.Fatal("bad contract created owner")
				}
			}
		})
	}
}
func TestWindowAccountingCacheUsageAndRounding(t *testing.T) {
	p := windowCacheContract().Model
	for _, tc := range []struct {
		name  string
		u     *windowUsage
		want  int64
		valid bool
	}{
		{"mixed", windowCacheSample(), 768, true},
		{"all-hit", &windowUsage{Input: windowInt(1757), Output: windowInt(33), CacheHit: windowInt(1757), CacheMiss: windowInt(0)}, 335, true},
		{"all-miss", &windowUsage{Input: windowInt(1757), Output: windowInt(33), CacheHit: windowInt(0), CacheMiss: windowInt(1757)}, 3778, true},
		{"absent-pair", &windowUsage{Input: windowInt(1757), Output: windowInt(33)}, 3778, true},
		{"zero", &windowUsage{Input: windowInt(0), Output: windowInt(0), CacheHit: windowInt(0), CacheMiss: windowInt(0)}, 0, true},
		{"nil", nil, 0, false},
		{"half-hit", &windowUsage{Input: windowInt(1), Output: windowInt(0), CacheHit: windowInt(1)}, 0, false},
		{"half-miss", &windowUsage{Input: windowInt(1), Output: windowInt(0), CacheMiss: windowInt(1)}, 0, false},
		{"negative", &windowUsage{Input: windowInt(1), Output: windowInt(0), CacheHit: windowInt(-1), CacheMiss: windowInt(2)}, 0, false},
		{"mismatch", &windowUsage{Input: windowInt(1), Output: windowInt(0), CacheHit: windowInt(1), CacheMiss: windowInt(1)}, 0, false},
		{"overbound", &windowUsage{Input: windowInt(p.MaxInput + 1), Output: windowInt(0)}, 0, false},
		{"output-overbound", &windowUsage{Input: windowInt(0), Output: windowInt(p.MaxOutput + 1)}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.settlementCost(tc.u)
			if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
				t.Fatal(got, err, "want", tc.want)
			}
		})
	}
	// Fractional input components round once together, independently of output.
	p.InputRate = 600000
	p.CacheHitInputRate = 400000
	p.OutputRate = 1
	got, err := p.settlementCost(&windowUsage{Input: windowInt(2), Output: windowInt(1), CacheHit: windowInt(1), CacheMiss: windowInt(1)})
	if err != nil || got != 2 {
		t.Fatal("weighted input must ceil once", got, err)
	}
	// Largest valid rates and inputs remain far inside int64 multiplication.
	p.MaxInput = 10000000
	p.MaxOutput = 1000000
	p.InputRate = 1000000000
	p.CacheHitInputRate = 999999999
	p.OutputRate = 1000000000
	got, err = p.settlementCost(&windowUsage{Input: windowInt(p.MaxInput), Output: windowInt(p.MaxOutput), CacheHit: windowInt(p.MaxInput), CacheMiss: windowInt(0)})
	if err != nil || got != 10999999990 {
		t.Fatal("maximum bounds", got, err)
	}
}
func TestWindowAccountingCacheNeverExceedsAdmissionCost(t *testing.T) {
	p := windowCacheContract().Model
	for _, rate := range []int64{1, 40000, 1999999, 2000000} {
		p.CacheHitInputRate = rate
		for _, input := range []int64{0, 1, 2, 999, 1757, 36863, 36864} {
			for _, hit := range []int64{0, input / 2, input} {
				for _, output := range []int64{0, 1, 33, 128} {
					u := &windowUsage{Input: windowInt(input), Output: windowInt(output), CacheHit: windowInt(hit), CacheMiss: windowInt(input - hit)}
					got, err := p.settlementCost(u)
					if err != nil || got < 0 || got > p.cost(input, output) {
						t.Fatal(rate, u, got, err)
					}
				}
			}
		}
	}
}
func TestWindowAccountingCacheFailureAndTickets(t *testing.T) {
	for _, kind := range []string{"partial", "stale", "duplicate", "pending-close", "cold-budget"} {
		t.Run(kind, func(t *testing.T) {
			c := windowCacheContract()
			c.ModelCap = 74752
			if kind == "stale" {
				c.ModelCap = 200000
			}
			l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			ticket, err := l.Begin(windowTestRequest("model"))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "pending-close" {
				if l.Close() == nil || l.Snapshot().ModelHeld != 74752 {
					t.Fatal("pending hold lost")
				}
				return
			}
			if kind == "partial" {
				u := windowCacheSample()
				u.CacheMiss = nil
				if l.Settle(ticket, strings.Repeat("f", 64), u) == nil || l.Snapshot().ModelHeld != 74752 || l.Snapshot().ModelKnown != 0 {
					t.Fatal("invalid usage discounted")
				}
				return
			}
			u := windowCacheSample()
			if kind == "cold-budget" {
				u = &windowUsage{Input: windowInt(c.Model.MaxInput), Output: windowInt(c.Model.MaxOutput)}
			}
			if err = l.Settle(ticket, strings.Repeat("f", 64), u); err != nil {
				t.Fatal(err)
			}
			if kind == "stale" {
				next := windowTestRequest("model")
				next.Attempt = 2
				if _, err = l.Begin(next); err != nil {
					t.Fatal(err)
				}
				if l.Settle(ticket, strings.Repeat("f", 64), u) == nil || l.Snapshot().ModelHeld != 74752 || l.Snapshot().ModelKnown != 768 {
					t.Fatal("stale ticket changed current hold", l.Snapshot())
				}
				return
			}
			r := windowTestRequest("model")
			if kind == "cold-budget" {
				r.Attempt = 2
			}
			if _, err = l.Begin(r); err == nil {
				t.Fatal("duplicate/cold budget admitted")
			}
			s := l.Snapshot()
			if s.ModelHeld != 0 || s.ModelAttempts != 1 || s.Halt == "" {
				t.Fatal(s)
			}
		})
	}
}

func TestWindowAccountingCachePureGuardAndJEV(t *testing.T) {
	p := windowCacheContract().Model
	for _, mutate := range []func(*windowProviderLimits, *windowUsage){
		func(p *windowProviderLimits, u *windowUsage) { p.InputRate = 1000000001 },
		func(p *windowProviderLimits, u *windowUsage) { p.CacheHitInputRate = -1 },
		func(p *windowProviderLimits, u *windowUsage) { p.CacheHitInputRate = p.InputRate + 1 },
		func(p *windowProviderLimits, u *windowUsage) { u.Input = nil },
		func(p *windowProviderLimits, u *windowUsage) { u.Output = nil },
		func(p *windowProviderLimits, u *windowUsage) { u.Input = windowInt(-1) },
		func(p *windowProviderLimits, u *windowUsage) { u.Output = windowInt(-1) },
	} {
		bad := p
		u := windowCacheSample()
		mutate(&bad, u)
		if _, err := bad.settlementCost(u); err == nil {
			t.Fatal("invalid pure cost accepted", bad, u)
		}
	}
	jev := windowCacheContract().JEV
	jev.MaxInput = 64000
	jev.MaxOutput = 0
	jev.InputRate = 42000
	u := &windowUsage{Input: windowInt(64000), Output: windowInt(0)}
	got, err := jev.settlementCost(u)
	if err != nil || got != 2688 || jev.cost(jev.MaxInput, jev.MaxOutput) != 2688 {
		t.Fatal("JEV cost/hold changed", got, err)
	}
}
