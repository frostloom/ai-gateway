package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func windowV3Contract() windowAccountingContract {
	c := windowTestContract()
	c.Schema = "full-window-accounting.v3"
	c.Model.CacheHitInputRate = 40000
	c.Model.InputRate = 2000000
	c.Model.OutputRate = 8000000
	c.Model.MaxInput = 35328
	c.Model.MaxOutput = 512
	c.ModelCap = 74752
	c.JEV.InputRate = 42000
	c.JEV.MaxInput = 64000
	c.JEVCap = 2688
	return c
}
func TestWindowAccountingV3Declaration(t *testing.T) {
	l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowV3Contract())
	if err != nil {
		t.Fatal("v3 isolated accounting declaration rejected:", err)
	}
	defer l.Close()
	if l.contract.ModelCap != 74752 || l.contract.JEVCap != 2688 {
		t.Fatal("base changed")
	}
}

type windowV3Operations interface {
	EndUnknown(windowRequestTicket, string) error
	RecordExtraFunding(int, string, int64, string) error
}

func windowV3API(t *testing.T, l *windowAccounting) windowV3Operations {
	t.Helper()
	v, ok := any(l).(windowV3Operations)
	if !ok {
		t.Fatal("missing permanent-unknown accounting operations")
	}
	return v
}
func windowV3Request(provider string, native int) windowRequestIdentity {
	r := windowTestRequest(provider)
	raw, _ := json.Marshal(r)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	fields["native_attempt"] = native
	raw, _ = json.Marshal(fields)
	json.Unmarshal(raw, &r)
	return r
}
func windowV3New(t *testing.T, provider string, funded bool) (*windowAccounting, windowV3Operations, windowRequestIdentity, windowRequestTicket) {
	t.Helper()
	l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowV3Contract())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	api := windowV3API(t, l)
	r := windowV3Request(provider, 1)
	ticket, e := l.Begin(r)
	if e != nil {
		t.Fatal(e)
	}
	if e = api.EndUnknown(ticket, strings.Repeat("1", 64)); e != nil {
		t.Fatal(e)
	}
	if funded {
		p, _ := l.contract.provider(provider)
		if e = api.RecordExtraFunding(ticket.sequence, provider, p.cost(p.MaxInput, p.MaxOutput), strings.Repeat("2", 64)); e != nil {
			t.Fatal(e)
		}
	}
	return l, api, r, ticket
}
func windowV3State(t *testing.T, l *windowAccounting) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(l.Snapshot())
	var m map[string]any
	json.Unmarshal(raw, &m)
	return m
}
func TestWindowAccountingV3UnknownAndExactFunding(t *testing.T) {
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			l, _, _, _ := windowV3New(t, provider, true)
			p, _ := l.contract.provider(provider)
			hold := p.cost(p.MaxInput, p.MaxOutput)
			s := windowV3State(t, l)
			if s["unknown_held_"+provider] != float64(hold) || s["extra_funded_"+provider] != float64(hold) || s["retry_required"] == nil {
				t.Fatal(s)
			}
			r := windowV3Request(provider, 2)
			ticket, e := l.Begin(r)
			if e != nil {
				t.Fatal(e)
			}
			if ticket.sequence != 2 {
				t.Fatal(ticket)
			}
			u := &windowUsage{Input: windowInt(1), Output: windowInt(0)}
			if e = l.Settle(ticket, strings.Repeat("3", 64), u); e != nil {
				t.Fatal(e)
			}
			s = windowV3State(t, l)
			if s["unknown_held_"+provider] != float64(hold) || s["extra_funded_"+provider] != float64(hold) || s["retry_required"] != nil || l.Snapshot().Pending != nil {
				t.Fatal(s)
			}
			if e = l.Close(); e != nil {
				t.Fatal(e)
			}
			a, e := readWindowAccounting(l.directory)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(a.State, l.Snapshot()) || a.CompletionProof != "unavailable_from_journal" || a.Contract != windowV3Contract() {
				t.Fatal(a, e)
			}
		})
	}
}
func TestWindowAccountingV3AdmissionFailuresHalt(t *testing.T) {
	for _, fault := range []string{"unfunded", "new-key", "body", "bytes", "provider", "model", "case", "pass", "arm", "turn", "attempt", "native0", "native3"} {
		t.Run(fault, func(t *testing.T) {
			l, _, _, _ := windowV3New(t, "jev", fault != "unfunded")
			r := windowV3Request("jev", 2)
			switch fault {
			case "new-key", "attempt":
				r.Attempt++
			case "body":
				r.BodySHA256 = strings.Repeat("4", 64)
			case "bytes":
				r.Bytes++
			case "provider":
				r = windowV3Request("model", 2)
			case "model":
				r.Model = "other"
			case "case":
				r.CaseKey = "other"
			case "pass":
				r.Pass = 2
			case "arm":
				r.Arm = "model"
			case "turn":
				r.Turn++
			case "native0":
				r = windowV3Request("jev", 0)
			case "native3":
				r = windowV3Request("jev", 3)
			}
			if _, e := l.Begin(r); e == nil {
				t.Fatal("invalid retry accepted")
			}
			if l.Snapshot().Halt == "" {
				t.Fatal("admission did not halt")
			}
			if _, e := l.Begin(windowV3Request("model", 1)); e == nil {
				t.Fatal("new logical escaped halt")
			}
		})
	}
}
func TestWindowAccountingV3FundingFailuresHalt(t *testing.T) {
	for _, fault := range []string{"minus1", "plus1", "negative", "overflow", "provider", "sequence", "digest", "duplicate", "pending"} {
		t.Run(fault, func(t *testing.T) {
			l, api, _, ticket := windowV3New(t, "jev", fault == "duplicate" || fault == "pending")
			amount := int64(2688)
			provider := "jev"
			seq := ticket.sequence
			digest := strings.Repeat("2", 64)
			switch fault {
			case "minus1":
				amount--
			case "plus1":
				amount++
			case "negative":
				amount = -1
			case "overflow":
				amount = math.MaxInt64
			case "provider":
				provider = "model"
			case "sequence":
				seq++
			case "digest":
				digest = "bad"
			case "pending":
				if _, e := l.Begin(windowV3Request("jev", 2)); e != nil {
					t.Fatal(e)
				}
			}
			if e := api.RecordExtraFunding(seq, provider, amount, digest); e == nil {
				t.Fatal("bad funding accepted")
			}
			if l.Snapshot().Halt == "" {
				t.Fatal("funding mismatch did not halt")
			}
		})
	}
}
func TestWindowAccountingV3SecondFailurePermanentStop(t *testing.T) {
	for _, mode := range []string{"fail", "unknown", "oldticket", "fake"} {
		t.Run(mode, func(t *testing.T) {
			l, api, _, old := windowV3New(t, "jev", true)
			ticket, e := l.Begin(windowV3Request("jev", 2))
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "fail":
				e = l.Fail(ticket, "upstream_failure")
			case "unknown":
				e = api.EndUnknown(ticket, strings.Repeat("3", 64))
			case "oldticket":
				e = l.Settle(old, strings.Repeat("3", 64), &windowUsage{Input: windowInt(1), Output: windowInt(0)})
			case "fake":
				e = api.EndUnknown(windowRequestTicket{}, strings.Repeat("3", 64))
			}
			if e == nil || l.Snapshot().Halt == "" || l.Snapshot().JEVHeld != 2688 || windowV3State(t, l)["unknown_held_jev"] != float64(2688) {
				t.Fatal(l.Snapshot(), e)
			}
			if _, e = l.Begin(windowV3Request("model", 1)); e == nil {
				t.Fatal("escaped failure")
			}
		})
	}
}
func TestWindowAccountingV3NextHoldIncludesUnknown(t *testing.T) {
	l, _, _, _ := windowV3New(t, "jev", true)
	ticket, e := l.Begin(windowV3Request("jev", 2))
	if e != nil {
		t.Fatal(e)
	}
	if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(2360), Output: windowInt(0)}); e != nil {
		t.Fatal(e)
	}
	if l.Snapshot().JEVKnown != 100 {
		t.Fatal(l.Snapshot())
	}
	r := windowV3Request("jev", 1)
	r.Attempt = 2
	if _, e = l.Begin(r); e == nil {
		t.Fatal("ignored permanent unknown in next admission")
	}
}
func TestWindowAccountingV3CloseUnresolved(t *testing.T) {
	for _, stage := range []string{"unknown", "funded", "pending"} {
		t.Run(stage, func(t *testing.T) {
			l, api, _, _ := windowV3New(t, "jev", stage != "unknown")
			if stage == "pending" {
				if _, e := l.Begin(windowV3Request("jev", 2)); e != nil {
					t.Fatal(e)
				}
			}
			if e := l.Close(); e == nil {
				t.Fatal("unresolved close succeeded")
			}
			if e := api.EndUnknown(windowRequestTicket{}, strings.Repeat("3", 64)); e == nil {
				t.Fatal("closed unknown")
			}
			if e := api.RecordExtraFunding(1, "jev", 2688, strings.Repeat("3", 64)); e == nil {
				t.Fatal("closed funding")
			}
			a, e := readWindowAccounting(l.directory)
			if e != nil || !a.State.Closed || a.State.Halt == "" {
				t.Fatal(a, e)
			}
		})
	}
}

func TestWindowAccountingV3FirstNativeAndDuplicate(t *testing.T) {
	for _, native := range []int{0, 2, 3} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowV3Contract())
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			if _, e = l.Begin(windowV3Request("jev", native)); e == nil || l.Snapshot().Halt == "" {
				t.Fatal("invalid first native", native)
			}
		})
	}
	for _, native := range []int{1, 2} {
		t.Run("duplicate"+fmt.Sprint(native), func(t *testing.T) {
			l, _, _, _ := windowV3New(t, "jev", true)
			ticket, e := l.Begin(windowV3Request("jev", 2))
			if e != nil {
				t.Fatal(e)
			}
			if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(0), Output: windowInt(0)}); e != nil {
				t.Fatal(e)
			}
			r := windowV3Request("jev", native)
			r.BodySHA256 = strings.Repeat("4", 64)
			if _, e = l.Begin(r); e == nil {
				t.Fatal("duplicate logical/native accepted")
			}
		})
	}
}
func TestWindowAccountingV3NormalSettlementAndUnknownRetention(t *testing.T) {
	c := windowV3Contract()
	c.JEVCap = 2688 * 5
	l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	api := windowV3API(t, l)
	for logical := 1; logical <= 3; logical++ {
		r := windowV3Request("jev", 1)
		r.Attempt = logical
		ticket, e := l.Begin(r)
		if e != nil {
			t.Fatal(e)
		}
		if logical != 1 {
			if e = api.EndUnknown(ticket, strings.Repeat("1", 64)); e != nil {
				t.Fatal(e)
			}
			digest := fmt.Sprintf("%064x", logical)
			if e = api.RecordExtraFunding(ticket.sequence, "jev", 2688, digest); e != nil {
				t.Fatal(e)
			}
			r = windowV3Request("jev", 2)
			r.Attempt = logical
			ticket, e = l.Begin(r)
			if e != nil {
				t.Fatal(e)
			}
		}
		if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(0), Output: windowInt(0)}); e != nil {
			t.Fatal(e)
		}
	}
	s := windowV3State(t, l)
	if s["unknown_held_jev"] != float64(5376) || s["extra_funded_jev"] != float64(5376) || l.Snapshot().JEVAttempts != 5 || len(s["unknown_intents"].([]any)) != 2 {
		t.Fatal(s)
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
	a, e := readWindowAccounting(l.directory)
	if e != nil || !reflect.DeepEqual(a.State, l.Snapshot()) {
		t.Fatal(a, e)
	}
}
func TestWindowAccountingV3DuplicateClaimAndLocalLimit(t *testing.T) {
	for _, mode := range []string{"duplicate", "limit", "attempt-limit"} {
		t.Run(mode, func(t *testing.T) {
			c := windowV3Contract()
			c.JEV.MaxAttempts = 100
			c.JEVCap = 2688 * 30
			if mode == "attempt-limit" {
				c.JEV.MaxAttempts = 1
			}
			l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			api := windowV3API(t, l)
			for logical := 1; logical <= 21; logical++ {
				r := windowV3Request("jev", 1)
				r.Attempt = logical
				ticket, e := l.Begin(r)
				if e != nil {
					t.Fatal(e)
				}
				if e = api.EndUnknown(ticket, strings.Repeat("1", 64)); e != nil {
					t.Fatal(e)
				}
				digest := fmt.Sprintf("%064x", logical+100)
				if mode == "duplicate" {
					digest = strings.Repeat("2", 64)
				}
				e = api.RecordExtraFunding(ticket.sequence, "jev", 2688, digest)
				if (mode == "duplicate" && logical == 2) || (mode == "limit" && logical == 21) {
					if e == nil || l.Snapshot().Halt == "" {
						t.Fatal("extra limit/digest bypass")
					}
					return
				}
				if e != nil {
					t.Fatal(e)
				}
				r = windowV3Request("jev", 2)
				r.Attempt = logical
				ticket, e = l.Begin(r)
				if mode == "attempt-limit" {
					if e == nil || l.Snapshot().JEVAttempts != 1 {
						t.Fatal("native count bypass")
					}
					return
				}
				if e != nil {
					t.Fatal(e)
				}
				if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(0), Output: windowInt(0)}); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
func TestWindowAccountingV3DeepSnapshot(t *testing.T) {
	l, _, _, _ := windowV3New(t, "jev", true)
	before := l.Snapshot()
	s := l.Snapshot()
	s.UnknownIntents[0].Request.BodySHA256 = "changed"
	s.UnknownIntents[0].MaxHold = 0
	s.UnknownIntents[0].ExtraClaimSHA256 = "changed"
	s.RetryRequired.Request.CaseKey = "changed"
	s.RetryRequired.Sequence = 900
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("snapshot aliases liability or identity")
	}
}
func TestWindowAccountingV3LegacyFieldIsolation(t *testing.T) {
	for _, schema := range []string{"full-window-accounting.v1", "full-window-accounting.v2"} {
		t.Run(schema, func(t *testing.T) {
			c := windowTestContract()
			if strings.HasSuffix(schema, "v2") {
				c = windowCacheContract()
			}
			l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
			if e != nil {
				t.Fatal(e)
			}
			r := windowTestRequest("jev")
			ticket, e := l.Begin(r)
			if e != nil {
				t.Fatal(e)
			}
			if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(1), Output: windowInt(0)}); e != nil {
				t.Fatal(e)
			}
			if e = l.Close(); e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile(filepath.Join(l.directory, "accounting.jsonl"))
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(raw, []byte("native_attempt")) || bytes.Contains(raw, []byte("unknown_sequence")) {
				t.Fatal("legacy bytes changed")
			}
			for _, field := range []string{"native_attempt", "descriptor_sha256", "unknown_sequence", "provider", "amount", "extra_claim_sha256"} {
				lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
				if field == "native_attempt" {
					lines[1] = bytes.Replace(lines[1], []byte(`"request":{`), []byte(`"request":{"native_attempt":0,`), 1)
				} else {
					lines[1] = bytes.Replace(lines[1], []byte(`{"kind"`), []byte(`{"`+field+`":0,"kind"`), 1)
				}
				changed := append(bytes.Join(lines, []byte("\n")), '\n')
				if _, e = readWindowAccountingBytes(changed); e == nil {
					t.Fatal("legacy accepted explicit v3 field", field)
				}
			}
			r = windowV3Request("jev", 1)
			if c.validateRequest(r) == nil {
				t.Fatal("legacy accepted new native identity")
			}
		})
	}
}
func windowV3Rewrite(t *testing.T, raw []byte, edit func([]windowAccountingEvent) []windowAccountingEvent) []byte {
	t.Helper()
	var events []windowAccountingEvent
	for _, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
		var e windowAccountingEvent
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	events = edit(events)
	var result []byte
	previous := ""
	for i, e := range events {
		e.Index = i
		e.PreviousSHA256 = previous
		line, _ := json.Marshal(e)
		previous = conversationHash(line)
		result = append(result, line...)
		result = append(result, '\n')
	}
	return result
}
func TestWindowAccountingV3CanonicalReplayTransitions(t *testing.T) {
	l, _, _, _ := windowV3New(t, "jev", true)
	ticket, e := l.Begin(windowV3Request("jev", 2))
	if e != nil {
		t.Fatal(e)
	}
	if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(0), Output: windowInt(0)}); e != nil {
		t.Fatal(e)
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(filepath.Join(l.directory, "accounting.jsonl"))
	for _, mode := range []string{"sequence", "descriptor", "claim", "amount", "provider", "unknown-association", "body", "missing-funding", "order", "third", "fields", "extra-on-header", "extra-on-settle"} {
		t.Run(mode, func(t *testing.T) {
			changed := windowV3Rewrite(t, raw, func(es []windowAccountingEvent) []windowAccountingEvent {
				switch mode {
				case "sequence":
					es[2].Sequence++
				case "descriptor":
					es[2].DescriptorSHA256 = "bad"
				case "claim":
					es[3].ExtraClaimSHA256 = "bad"
				case "amount":
					es[3].Amount++
				case "provider":
					es[3].Provider = "model"
				case "unknown-association":
					es[3].UnknownSequence++
				case "body":
					es[4].Request.BodySHA256 = strings.Repeat("5", 64)
				case "missing-funding":
					es = append(es[:3], es[4:]...)
				case "order":
					es[3], es[4] = es[4], es[3]
				case "third":
					copy := *es[4].Request
					copy.NativeAttempt = 3
					es[4].Request = &copy
				case "fields":
					es[2].Amount = 1
				case "extra-on-header":
					es[0].Amount = 1
				case "extra-on-settle":
					es[5].ExtraClaimSHA256 = strings.Repeat("6", 64)
				}
				return es
			})
			if _, e := readWindowAccountingBytes(changed); e == nil {
				t.Fatal("accepted incoherent canonical transition", mode)
			}
		})
	}
	// Hash checking is distinct from external authentication of arbitrary digests.
	changed := bytes.Replace(raw, []byte(strings.Repeat("1", 64)), []byte(strings.Repeat("7", 64)), 1)
	if _, e = readWindowAccountingBytes(changed); e == nil {
		t.Fatal("accepted broken hash chain")
	}
}

type windowV3HookFile struct {
	windowJournalFile
	hook func() error
}

func (f *windowV3HookFile) Sync() error {
	if err := f.windowJournalFile.Sync(); err != nil {
		return err
	}
	if f.hook != nil {
		return f.hook()
	}
	return nil
}
func TestWindowAccountingV3PersistencePoison(t *testing.T) {
	for _, stage := range []string{"unknown", "funding", "retry-intent", "retry-settle", "close"} {
		for _, fault := range []string{"write", "short", "sync", "identity"} {
			t.Run(stage+"/"+fault, func(t *testing.T) {
				c := windowV3Contract()
				dir := filepath.Join(t.TempDir(), "window")
				var bad *failingWindowFile
				var hook *windowV3HookFile
				l, e := newWindowAccountingWithFile(dir, c, func(f *os.File) windowJournalFile {
					bad = &failingWindowFile{windowJournalFile: f}
					hook = &windowV3HookFile{windowJournalFile: bad}
					return hook
				})
				if e != nil {
					t.Fatal(e)
				}
				defer l.Close()
				api := windowV3API(t, l)
				r := windowV3Request("jev", 1)
				ticket, e := l.Begin(r)
				if e != nil {
					t.Fatal(e)
				}
				if stage != "unknown" {
					if e = api.EndUnknown(ticket, strings.Repeat("1", 64)); e != nil {
						t.Fatal(e)
					}
				}
				if stage != "unknown" && stage != "funding" {
					if e = api.RecordExtraFunding(ticket.sequence, "jev", 2688, strings.Repeat("2", 64)); e != nil {
						t.Fatal(e)
					}
				}
				if stage == "retry-settle" || stage == "close" {
					ticket, e = l.Begin(windowV3Request("jev", 2))
					if e != nil {
						t.Fatal(e)
					}
				}
				if stage == "close" {
					if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(0), Output: windowInt(0)}); e != nil {
						t.Fatal(e)
					}
				}
				switch fault {
				case "write":
					bad.writeFailure = true
				case "short":
					bad.shortWrite = true
				case "sync":
					bad.syncFailure = true
				case "identity":
					hook.hook = func() error {
						return os.Rename(filepath.Join(dir, "accounting.jsonl"), filepath.Join(dir, "replaced.jsonl"))
					}
				}
				switch stage {
				case "unknown":
					e = api.EndUnknown(ticket, strings.Repeat("1", 64))
				case "funding":
					e = api.RecordExtraFunding(ticket.sequence, "jev", 2688, strings.Repeat("2", 64))
				case "retry-intent":
					var next windowRequestTicket
					next, e = l.Begin(windowV3Request("jev", 2))
					if next.owner != nil {
						t.Fatal("ticket escaped failed durability")
					}
				case "retry-settle":
					e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(0), Output: windowInt(0)})
				case "close":
					e = l.Close()
				}
				if e == nil || !l.poisoned || l.Snapshot().Halt != "durability_failure" {
					t.Fatal("durability failure hidden", l.Snapshot(), e)
				}
				if stage == "unknown" && l.Snapshot().JEVHeld != 2688 {
					t.Fatal("pending unknown lost")
				}
				if stage != "unknown" && l.Snapshot().JEVUnknownHeld != 2688 {
					t.Fatal("old unknown lost")
				}
				if stage == "retry-settle" && l.Snapshot().JEVHeld != 2688 {
					t.Fatal("retry hold lost")
				}
				if _, e = l.Begin(windowV3Request("model", 1)); e == nil {
					t.Fatal("poison bypass")
				}
				if e = api.EndUnknown(ticket, strings.Repeat("4", 64)); e == nil {
					t.Fatal("poison unknown bypass")
				}
				if e = api.RecordExtraFunding(1, "jev", 2688, strings.Repeat("4", 64)); e == nil {
					t.Fatal("poison funding bypass")
				}
			})
		}
	}
}
func TestWindowAccountingV3FailedFirstCannotRecover(t *testing.T) {
	for _, mode := range []string{"fail", "usage-missing", "usage-bounds", "cache-invalid", "bad-descriptor"} {
		t.Run(mode, func(t *testing.T) {
			l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowV3Contract())
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			api := windowV3API(t, l)
			ticket, e := l.Begin(windowV3Request("jev", 1))
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "fail":
				e = l.Fail(ticket, "upstream_failure")
			case "usage-missing":
				e = l.Settle(ticket, strings.Repeat("3", 64), nil)
			case "usage-bounds":
				e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(64001), Output: windowInt(0)})
			case "cache-invalid":
				e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(1), Output: windowInt(0), CacheHit: windowInt(2), CacheMiss: windowInt(0)})
			case "bad-descriptor":
				e = api.EndUnknown(ticket, "bad")
			}
			if e == nil || l.Snapshot().JEVHeld != 2688 || l.Snapshot().Halt == "" {
				t.Fatal(l.Snapshot(), e)
			}
			if e = api.EndUnknown(ticket, strings.Repeat("1", 64)); e == nil {
				t.Fatal("recovered halted first attempt")
			}
			if e = api.RecordExtraFunding(1, "jev", 2688, strings.Repeat("2", 64)); e == nil {
				t.Fatal("funded halted first attempt")
			}
		})
	}
}
func TestWindowAccountingV3ConcurrentOperations(t *testing.T) {
	for _, stage := range []string{"begin", "unknown", "funding"} {
		t.Run(stage, func(t *testing.T) {
			l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowV3Contract())
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			api := windowV3API(t, l)
			var ticket windowRequestTicket
			if stage != "begin" {
				ticket, e = l.Begin(windowV3Request("jev", 1))
				if e != nil {
					t.Fatal(e)
				}
			}
			if stage == "funding" {
				if e = api.EndUnknown(ticket, strings.Repeat("1", 64)); e != nil {
					t.Fatal(e)
				}
			}
			var wg sync.WaitGroup
			results := make(chan error, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var err error
					switch stage {
					case "begin":
						_, err = l.Begin(windowV3Request("jev", 1))
					case "unknown":
						err = api.EndUnknown(ticket, strings.Repeat("1", 64))
					case "funding":
						err = api.RecordExtraFunding(ticket.sequence, "jev", 2688, strings.Repeat("2", 64))
					}
					results <- err
				}()
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				}
			}
			if success != 1 {
				t.Fatal("duplicate successful transitions", success)
			}
			s := l.Snapshot()
			if s.JEVAttempts != 1 || s.JEVExtraFunded > 2688 || s.JEVUnknownHeld > 2688 || s.Halt == "" {
				t.Fatal(s)
			}
		})
	}
}
func TestWindowAccountingV3CheckedArithmetic(t *testing.T) {
	for _, pair := range [][2]int64{{-1, 0}, {0, -1}, {math.MaxInt64, 1}, {math.MaxInt64 - 1, 2}} {
		if _, e := windowCheckedAdd(pair[0], pair[1]); e == nil {
			t.Fatal("invalid sum", pair)
		}
	}
	if n, e := windowCheckedAdd(math.MaxInt64-1, 1); e != nil || n != math.MaxInt64 {
		t.Fatal(n, e)
	}
	for _, fault := range []string{"negative-known", "unknown-overflow", "extra-overflow", "pending-overflow"} {
		t.Run(fault, func(t *testing.T) {
			l := &windowAccounting{contract: windowV3Contract()}
			switch fault {
			case "negative-known":
				l.state.JEVKnown = -1
			case "unknown-overflow":
				l.state.JEVUnknownHeld = math.MaxInt64
			case "extra-overflow":
				l.state.JEVExtraFunded = math.MaxInt64
			case "pending-overflow":
				l.state.JEVHeld = math.MaxInt64
			}
			if e := l.liabilityV3("jev", 2688); e == nil {
				t.Fatal("invalid liability arithmetic", fault)
			}
		})
	}
}
func TestWindowAccountingV3LegacyBytes(t *testing.T) {
	for _, schema := range []string{"full-window-accounting.v1", "full-window-accounting.v2"} {
		t.Run(schema, func(t *testing.T) {
			c := windowTestContract()
			if strings.HasSuffix(schema, "v2") {
				c = windowCacheContract()
			}
			r := windowTestRequest("jev")
			request := `{"provider":"jev","model":"test-judge","case_key":"balance-001/answer_complete","pass":1,"arm":"jev_selective","turn":1,"attempt":1,"body_sha256":"` + strings.Repeat("e", 64) + `","bytes":64}`
			got, _ := json.Marshal(r)
			if string(got) != request {
				t.Fatal("legacy request bytes changed", string(got))
			}
			state := windowAccountingState{ModelPermanent: c.ModelCap, JEVPermanent: c.JEVCap}
			got, _ = json.Marshal(state)
			expected := fmt.Sprintf(`{"permanent_model_micro_cny":%d,"permanent_jev_micro_usd":%d,"known_model_micro_cny":0,"known_jev_micro_usd":0,"held_model_micro_cny":0,"held_jev_micro_usd":0,"model_intents":0,"jev_intents":0,"closed":false,"envelope_violation":false}`, c.ModelCap, c.JEVCap)
			if string(got) != expected {
				t.Fatal("legacy state bytes changed", string(got))
			}
			l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
			if e != nil {
				t.Fatal(e)
			}
			ticket, e := l.Begin(r)
			if e != nil {
				t.Fatal(e)
			}
			if e = l.Settle(ticket, strings.Repeat("3", 64), &windowUsage{Input: windowInt(1), Output: windowInt(0)}); e != nil {
				t.Fatal(e)
			}
			if e = l.Close(); e != nil {
				t.Fatal(e)
			}
			contract, _ := json.Marshal(c)
			header := `{"kind":"config","index":0,"contract":` + string(contract) + `}`
			intent := `{"kind":"intent","index":1,"previous_sha256":"` + conversationHash([]byte(header)) + `","sequence":1,"request":` + request + `}`
			settle := `{"kind":"settle","index":2,"previous_sha256":"` + conversationHash([]byte(intent)) + `","sequence":1,"response_sha256":"` + strings.Repeat("3", 64) + `","usage":{"input_tokens":1,"output_tokens":0}}`
			closeEvent := `{"kind":"close","index":3,"previous_sha256":"` + conversationHash([]byte(settle)) + `"}`
			want := header + "\n" + intent + "\n" + settle + "\n" + closeEvent + "\n"
			raw, _ := os.ReadFile(filepath.Join(l.directory, "accounting.jsonl"))
			if string(raw) != want {
				t.Fatal("legacy journal bytes changed", string(raw))
			}
		})
	}
}
