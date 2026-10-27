package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowTestContract() windowAccountingContract {
	return windowAccountingContract{Schema: "full-window-accounting.v1", ScopeSHA256: strings.Repeat("a", 64), ScheduleSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), ClaimSHA256: strings.Repeat("d", 64), WindowID: "window-001", ModelCap: 100, JEVCap: 100,
		Model: windowProviderLimits{Model: "test-model", MaxAttempts: 10, MaxRequestBytes: 4096, MaxInput: 10, MaxOutput: 10, InputRate: 1000000, OutputRate: 1000000},
		JEV:   windowProviderLimits{Model: "test-judge", MaxAttempts: 10, MaxRequestBytes: 4096, MaxInput: 10, MaxOutput: 10, InputRate: 1000000, OutputRate: 0}}
}
func windowTestRequest(provider string) windowRequestIdentity {
	model := "test-model"
	arm := "model"
	if provider == "jev" {
		model = "test-judge"
		arm = "jev_selective"
	}
	return windowRequestIdentity{Provider: provider, Model: model, CaseKey: "balance-001/answer_complete", Pass: 1, Arm: arm, Turn: 1, Attempt: 1, BodySHA256: strings.Repeat("e", 64), Bytes: 64}
}
func windowInt(n int64) *int64 { return &n }
func TestWindowAccountingRepeatedClosePreservesFailure(t *testing.T) {
	l, e := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = l.Begin(windowTestRequest("model")); e != nil {
		t.Fatal(e)
	}
	if e = l.Close(); e == nil {
		t.Fatal("pending close succeeded")
	}
	if e = l.Close(); e == nil {
		t.Fatal("second close erased failure")
	}
}

func TestWindowAccountingReadbackCannotProveCloseDurability(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "window")
	var fake *failingWindowFile
	l, e := newWindowAccountingWithFile(dir, windowTestContract(), func(f *os.File) windowJournalFile { fake = &failingWindowFile{windowJournalFile: f}; return fake })
	if e != nil {
		t.Fatal(e)
	}
	fake.syncFailure = true
	if e = l.Close(); e == nil {
		t.Fatal("failed close succeeded")
	}
	audit, e := readWindowAccounting(dir)
	if e != nil {
		t.Fatal(e)
	}
	if !audit.State.Closed || l.Snapshot().Halt != "durability_failure" {
		t.Fatal("counterfactual missing", audit, l.Snapshot())
	}
	raw, _ := json.Marshal(audit)
	var v map[string]any
	json.Unmarshal(raw, &v)
	if v["completion_proof"] != "unavailable_from_journal" {
		t.Fatal("readable close bytes misrepresented as completion proof", string(raw))
	}
}
func TestWindowAccountingSettlesInsidePermanentCap(t *testing.T) {
	c := windowTestContract()
	c.ModelCap = 25
	dir := filepath.Join(t.TempDir(), "window")
	l, err := newWindowAccounting(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	req := windowTestRequest("model")
	ticket, err := l.Begin(req)
	if err != nil {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.ModelKnown != 0 || s.ModelHeld != 20 || s.ModelPermanent != 25 || s.Pending == nil {
		t.Fatal("pre-call hold missing", s)
	}
	if err := l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(2), Output: windowInt(1)}); err != nil {
		t.Fatal(err)
	}
	req.Attempt = 2
	ticket, err = l.Begin(req)
	if err != nil {
		t.Fatal("known usage did not replace transient hold", err)
	}
	if err := l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}); err != nil {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.ModelKnown != 5 || s.ModelHeld != 0 || s.ModelPermanent != 25 || s.ModelAttempts != 2 {
		t.Fatal(s)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	audit, err := readWindowAccounting(dir)
	if err != nil || audit.State.ModelKnown != 5 || !audit.State.Closed || audit.State.ModelPermanent != 25 {
		t.Fatal(audit, err)
	}
	if _, err := newWindowAccounting(dir, c); err == nil {
		t.Fatal("closed window resumed")
	}
}
func TestWindowAccountingFailureRetainsHoldAndBlocksBothProviders(t *testing.T) {
	for _, failure := range []string{"upstream", "missing-usage", "excess-usage", "concurrent"} {
		t.Run(failure, func(t *testing.T) {
			l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			ticket, err := l.Begin(windowTestRequest("jev"))
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "upstream":
				err = l.Fail(ticket, "upstream_failure")
			case "missing-usage":
				err = l.Settle(ticket, strings.Repeat("f", 64), nil)
			case "excess-usage":
				err = l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(11), Output: windowInt(1)})
			case "concurrent":
				_, err = l.Begin(windowTestRequest("model"))
			}
			if err == nil {
				t.Fatal("failure hidden")
			}
			s := l.Snapshot()
			if s.Halt == "" || s.JEVHeld != 10 || s.JEVKnown != 0 || s.Pending == nil {
				t.Fatal("unknown charge lost", s)
			}
			if _, err := l.Begin(windowTestRequest("model")); err == nil {
				t.Fatal("model fallback after failure")
			}
		})
	}
}
func TestWindowAccountingInterruptedReadbackIsAuditOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "window")
	l, err := newWindowAccounting(dir, windowTestContract())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Begin(windowTestRequest("model")); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss, without a successful checkpoint or a cleanup event.
	if err := l.file.Close(); err != nil {
		t.Fatal(err)
	}
	audit, err := readWindowAccounting(dir)
	if err != nil || audit.State.Halt == "" || audit.State.ModelHeld != 20 || audit.State.Pending == nil {
		t.Fatal("interrupted request became free", audit, err)
	}
	if _, err := newWindowAccounting(dir, windowTestContract()); err == nil {
		t.Fatal("interrupted window reopened")
	}
	file := filepath.Join(dir, "accounting.jsonl")
	raw, _ := os.ReadFile(file)
	os.WriteFile(file, raw[:len(raw)-2], 0600)
	if _, err := readWindowAccounting(dir); err == nil {
		t.Fatal("truncated journal accepted")
	}
}
func TestWindowAccountingRejectsInvalidIdentityBeforeDirectory(t *testing.T) {
	for _, fault := range []string{"claim", "cap", "bounds", "rate", "window"} {
		t.Run(fault, func(t *testing.T) {
			c := windowTestContract()
			switch fault {
			case "claim":
				c.ClaimSHA256 = ""
			case "cap":
				c.ModelCap = 19
			case "bounds":
				c.Model.MaxInput = 0
			case "rate":
				c.Model.InputRate = 0
			case "window":
				c.WindowID = "../other"
			}
			dir := filepath.Join(t.TempDir(), "window")
			if _, err := newWindowAccounting(dir, c); err == nil {
				t.Fatal("invalid contract accepted")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("invalid contract wrote output")
			}
		})
	}
}

func TestWindowAccountingCloseUnsettledIsFailure(t *testing.T) {
	l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Begin(windowTestRequest("model")); err != nil {
		t.Fatal(err)
	}
	if l.Close() == nil {
		t.Fatal("unsettled close falsely returned success")
	}
	if l.Snapshot().ModelHeld != 20 || l.Snapshot().Halt == "" {
		t.Fatal("closing erased uncertain hold")
	}
}

func TestWindowAccountingExactAdmissionAndRounding(t *testing.T) {
	c := windowTestContract()
	c.ModelCap = 25
	l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for i := 1; i <= 3; i++ {
		r := windowTestRequest("model")
		r.Attempt = i
		ticket, err := l.Begin(r)
		if err != nil {
			t.Fatal("exact fit denied", i, err)
		}
		in, out := int64(1), int64(1)
		if i == 1 {
			in = 2
		}
		if err = l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: &in, Output: &out}); err != nil {
			t.Fatal(err)
		}
	}
	r := windowTestRequest("model")
	r.Attempt = 4
	if _, err = l.Begin(r); err == nil {
		t.Fatal("overcap admission accepted")
	}
	s := l.Snapshot()
	if s.ModelKnown != 7 || s.ModelHeld != 0 || s.ModelPermanent != 25 || s.ModelAttempts != 3 || s.Halt != "budget_exhausted" {
		t.Fatal(s)
	}
	c = windowTestContract()
	c.Model.MaxInput = 1
	c.Model.MaxOutput = 1
	c.Model.InputRate = 1000001
	c.Model.OutputRate = 2000001
	rounded, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
	if err != nil {
		t.Fatal(err)
	}
	defer rounded.Close()
	ticket, err := rounded.Begin(windowTestRequest("model"))
	if err != nil {
		t.Fatal(err)
	}
	if rounded.Snapshot().ModelHeld != 5 {
		t.Fatal("component rounding changed")
	}
	if err := rounded.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}); err != nil {
		t.Fatal(err)
	}
	if rounded.Snapshot().ModelKnown != 5 {
		t.Fatal("rounding differs at settle")
	}
}
func TestWindowAccountingProviderCurrenciesStaySeparate(t *testing.T) {
	c := windowTestContract()
	c.ModelCap = 20
	c.JEVCap = 10
	l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), c)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ticket, err := l.Begin(windowTestRequest("model"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(10), Output: windowInt(10)}); err != nil {
		t.Fatal(err)
	}
	ticket, err = l.Begin(windowTestRequest("jev"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(3), Output: windowInt(7)}); err != nil {
		t.Fatal(err)
	}
	if s := l.Snapshot(); s.ModelKnown != 20 || s.JEVKnown != 3 || s.ModelPermanent != 20 || s.JEVPermanent != 10 {
		t.Fatal("currencies mixed", s)
	}
	r := windowTestRequest("model")
	r.Attempt = 2
	if _, err := l.Begin(r); err == nil {
		t.Fatal("USD balance funded CNY request")
	}
}
func TestWindowAccountingInvalidUsageRetainsUnknownHold(t *testing.T) {
	for _, kind := range []string{"input-missing", "output-missing", "input-negative", "output-negative", "cache-partial", "cache-negative", "cache-mismatch", "output-excess", "response"} {
		t.Run(kind, func(t *testing.T) {
			l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			ticket, err := l.Begin(windowTestRequest("model"))
			if err != nil {
				t.Fatal(err)
			}
			usage := &windowUsage{Input: windowInt(2), Output: windowInt(1)}
			response := strings.Repeat("f", 64)
			switch kind {
			case "input-missing":
				usage.Input = nil
			case "output-missing":
				usage.Output = nil
			case "input-negative":
				usage.Input = windowInt(-1)
			case "output-negative":
				usage.Output = windowInt(-1)
			case "cache-partial":
				usage.CacheHit = windowInt(1)
			case "cache-negative":
				usage.CacheHit = windowInt(-1)
				usage.CacheMiss = windowInt(3)
			case "cache-mismatch":
				usage.CacheHit = windowInt(2)
				usage.CacheMiss = windowInt(2)
			case "output-excess":
				usage.Output = windowInt(11)
			case "response":
				response = "not-a-digest"
			}
			if l.Settle(ticket, response, usage) == nil {
				t.Fatal("invalid settlement accepted")
			}
			s := l.Snapshot()
			if s.Halt == "" || s.ModelKnown != 0 || s.ModelHeld != 20 || s.Pending == nil {
				t.Fatal("unknown charge became free", s)
			}
		})
	}
}
func TestWindowAccountingCacheAndDetachedSnapshot(t *testing.T) {
	l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	r := windowTestRequest("model")
	ticket, err := l.Begin(r)
	if err != nil {
		t.Fatal(err)
	}
	s := l.Snapshot()
	s.Pending.Request.CaseKey = "changed"
	s.ModelPermanent = 0
	if now := l.Snapshot(); now.Pending.Request.CaseKey != r.CaseKey || now.ModelPermanent != 100 {
		t.Fatal("snapshot aliases ledger")
	}
	if err = l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(4), Output: windowInt(1), CacheHit: windowInt(3), CacheMiss: windowInt(1)}); err != nil {
		t.Fatal(err)
	}
	if l.Snapshot().ModelKnown != 5 {
		t.Fatal("cache was underpriced against declared peak")
	}
	r.Attempt = 2
	next, err := l.Begin(r)
	if err != nil {
		t.Fatal(err)
	}
	if l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}) == nil {
		t.Fatal("stale token accepted")
	}
	if next.sequence == ticket.sequence || l.Snapshot().ModelHeld != 20 {
		t.Fatal("next hold lost")
	}
}
func TestWindowAccountingDuplicateSlotCannotChangeBodyAndRetry(t *testing.T) {
	l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	r := windowTestRequest("model")
	ticket, err := l.Begin(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}); err != nil {
		t.Fatal(err)
	}
	r.BodySHA256 = strings.Repeat("0", 64)
	if _, err = l.Begin(r); err == nil {
		t.Fatal("same slot retried under another body")
	}
	if l.Snapshot().ModelAttempts != 1 {
		t.Fatal("duplicate sent")
	}
}
func TestWindowAccountingInvalidRequestStopsBeforeIntent(t *testing.T) {
	for _, kind := range []string{"provider", "model", "arm", "digest", "bytes", "case", "pass", "turn", "attempt"} {
		t.Run(kind, func(t *testing.T) {
			l, err := newWindowAccounting(filepath.Join(t.TempDir(), "window"), windowTestContract())
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			r := windowTestRequest("model")
			switch kind {
			case "provider":
				r.Provider = "other"
			case "model":
				r.Model = "other"
			case "arm":
				r.Arm = "other"
			case "digest":
				r.BodySHA256 = ""
			case "bytes":
				r.Bytes = 4097
			case "case":
				r.CaseKey = ""
			case "pass":
				r.Pass = 4
			case "turn":
				r.Turn = 0
			case "attempt":
				r.Attempt = 0
			}
			if _, err := l.Begin(r); err == nil {
				t.Fatal("invalid request admitted")
			}
			if s := l.Snapshot(); s.ModelAttempts != 0 || s.JEVAttempts != 0 || s.Pending != nil || s.Halt == "" {
				t.Fatal(s)
			}
		})
	}
}

type failingWindowFile struct {
	windowJournalFile
	syncFailure, writeFailure, shortWrite bool
}

func (f *failingWindowFile) Sync() error {
	if f.syncFailure {
		return fmt.Errorf("injected sync failure")
	}
	return f.windowJournalFile.Sync()
}
func (f *failingWindowFile) Write(raw []byte) (int, error) {
	if f.writeFailure {
		return 0, fmt.Errorf("injected write failure")
	}
	if f.shortWrite {
		return f.windowJournalFile.Write(raw[:len(raw)/2])
	}
	return f.windowJournalFile.Write(raw)
}
func TestWindowAccountingDurabilityFailuresNeverPermitAnotherAdmission(t *testing.T) {
	for _, stage := range []string{"header-sync", "intent-sync", "intent-write", "intent-short", "settle-sync", "close-sync"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "window")
			var fake *failingWindowFile
			l, err := newWindowAccountingWithFile(dir, windowTestContract(), func(f *os.File) windowJournalFile {
				fake = &failingWindowFile{windowJournalFile: f, syncFailure: stage == "header-sync"}
				return fake
			})
			if stage == "header-sync" {
				if err == nil {
					t.Fatal("failed header returned ledger")
				}
				if _, err := newWindowAccounting(dir, windowTestContract()); err == nil {
					t.Fatal("failed creation evidence reused")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if strings.HasPrefix(stage, "intent") {
				fake.syncFailure = stage == "intent-sync"
				fake.writeFailure = stage == "intent-write"
				fake.shortWrite = stage == "intent-short"
				if _, err = l.Begin(windowTestRequest("model")); err == nil {
					t.Fatal("intent failure hidden")
				}
			} else {
				ticket, err := l.Begin(windowTestRequest("model"))
				if err != nil {
					t.Fatal(err)
				}
				if stage == "settle-sync" {
					fake.syncFailure = true
					if l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}) == nil {
						t.Fatal("settlement failure hidden")
					}
					if l.Snapshot().ModelHeld != 20 || l.Snapshot().ModelKnown != 0 {
						t.Fatal("undurable usage replaced hold")
					}
				} else {
					if err = l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}); err != nil {
						t.Fatal(err)
					}
					fake.syncFailure = true
					if l.Close() == nil {
						t.Fatal("close failure hidden")
					}
				}
			}
			if _, err = l.Begin(windowTestRequest("jev")); err == nil {
				t.Fatal("I/O failure allowed next provider")
			}
			if l.Snapshot().Halt == "" {
				t.Fatal("durability not recorded")
			}
		})
	}
}
func TestWindowAccountingAuditRejectsAlteredOrOrphanedRecords(t *testing.T) {
	for _, kind := range []string{"header", "reorder", "duplicate", "unknown-field", "duplicate-key", "trailing-event", "orphan"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "window")
			l, err := newWindowAccounting(dir, windowTestContract())
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := l.Begin(windowTestRequest("model"))
			if err != nil {
				t.Fatal(err)
			}
			if err = l.Settle(ticket, strings.Repeat("f", 64), &windowUsage{Input: windowInt(1), Output: windowInt(1)}); err != nil {
				t.Fatal(err)
			}
			if err = l.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "accounting.jsonl")
			raw, _ := os.ReadFile(path)
			lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
			switch kind {
			case "header":
				raw = bytes.Replace(raw, []byte(`"permanent_model_micro_cny":100`), []byte(`"permanent_model_micro_cny":101`), 1)
			case "reorder":
				lines[1], lines[2] = lines[2], lines[1]
				raw = append(bytes.Join(lines, []byte("\n")), '\n')
			case "duplicate":
				raw = append(raw, append(lines[1], '\n')...)
			case "unknown-field":
				raw = bytes.Replace(raw, []byte(`"kind":"settle"`), []byte(`"kind":"settle","unexpected":true`), 1)
			case "duplicate-key":
				raw = bytes.Replace(raw, []byte(`"kind":"settle"`), []byte(`"kind":"halt","kind":"settle"`), 1)
			case "trailing-event":
				var e windowAccountingEvent
				json.Unmarshal(lines[len(lines)-1], &e)
				e.Index++
				e.PreviousSHA256 = conversationHash(lines[len(lines)-1])
				extra, _ := json.Marshal(e)
				raw = append(raw, append(extra, '\n')...)
			case "orphan":
				os.WriteFile(filepath.Join(dir, "orphan"), []byte("unclaimed"), 0600)
			}
			os.WriteFile(path, raw, 0600)
			if _, err := readWindowAccounting(dir); err == nil {
				t.Fatal("invalid audit accepted", kind)
			}
		})
	}
}
