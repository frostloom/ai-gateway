package taskbench

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// These private primitives are not a claim validator or an execution capability.
// No network client, provider key or legacy budget mutation exists here.
type windowProviderLimits struct {
	Model           string `json:"model"`
	MaxAttempts     int    `json:"max_attempts"`
	MaxRequestBytes int    `json:"max_request_bytes"`
	MaxInput        int64  `json:"max_input_tokens"`
	MaxOutput       int64  `json:"max_output_tokens"`
	InputRate       int64  `json:"input_micro_per_million"`
	OutputRate      int64  `json:"output_micro_per_million"`
}
type windowAccountingContract struct {
	Schema         string               `json:"schema"`
	ScopeSHA256    string               `json:"scope_sha256"`
	ScheduleSHA256 string               `json:"schedule_sha256"`
	PolicySHA256   string               `json:"policy_sha256"`
	ClaimSHA256    string               `json:"claim_sha256"`
	WindowID       string               `json:"window_id"`
	ModelCap       int64                `json:"permanent_model_micro_cny"`
	JEVCap         int64                `json:"permanent_jev_micro_usd"`
	Model          windowProviderLimits `json:"model"`
	JEV            windowProviderLimits `json:"jev"`
}
type windowRequestIdentity struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	CaseKey    string `json:"case_key"`
	Pass       int    `json:"pass"`
	Arm        string `json:"arm"`
	Turn       int    `json:"turn"`
	Attempt    int    `json:"attempt"`
	BodySHA256 string `json:"body_sha256"`
	Bytes      int    `json:"bytes"`
}
type windowUsage struct {
	Input     *int64 `json:"input_tokens"`
	Output    *int64 `json:"output_tokens"`
	CacheHit  *int64 `json:"cache_hit_tokens,omitempty"`
	CacheMiss *int64 `json:"cache_miss_tokens,omitempty"`
}
type windowPendingIntent struct {
	Sequence int                   `json:"sequence"`
	Request  windowRequestIdentity `json:"request"`
}
type windowAccountingState struct {
	ModelPermanent    int64                `json:"permanent_model_micro_cny"`
	JEVPermanent      int64                `json:"permanent_jev_micro_usd"`
	ModelKnown        int64                `json:"known_model_micro_cny"`
	JEVKnown          int64                `json:"known_jev_micro_usd"`
	ModelHeld         int64                `json:"held_model_micro_cny"`
	JEVHeld           int64                `json:"held_jev_micro_usd"`
	ModelAttempts     int                  `json:"model_intents"`
	JEVAttempts       int                  `json:"jev_intents"`
	Pending           *windowPendingIntent `json:"pending,omitempty"`
	Halt              string               `json:"halt,omitempty"`
	Closed            bool                 `json:"closed"`
	EnvelopeViolation bool                 `json:"envelope_violation"`
}
type windowAccountingAudit struct {
	// A readable event proves only its logical content. In particular, bytes
	// written before a failed Sync can still be read as a logical close. Only
	// a future live owned checkpoint after successful Close may advance work.
	CompletionProof string                   `json:"completion_proof"`
	Contract        windowAccountingContract `json:"contract"`
	State           windowAccountingState    `json:"state"`
	JournalSHA256   string                   `json:"journal_sha256"`
	Events          int                      `json:"events"`
}
type windowAccountingEvent struct {
	Kind           string                    `json:"kind"`
	Index          int                       `json:"index"`
	PreviousSHA256 string                    `json:"previous_sha256,omitempty"`
	Contract       *windowAccountingContract `json:"contract,omitempty"`
	Sequence       int                       `json:"sequence,omitempty"`
	Request        *windowRequestIdentity    `json:"request,omitempty"`
	ResponseSHA256 string                    `json:"response_sha256,omitempty"`
	Usage          *windowUsage              `json:"usage,omitempty"`
	Code           string                    `json:"code,omitempty"`
}
type windowJournalFile interface {
	io.Writer
	Sync() error
	Close() error
}
type windowRequestTicket struct {
	owner    *windowAccounting
	sequence int
}
type windowAccounting struct {
	mu               sync.Mutex
	file             windowJournalFile
	journal          *os.File
	directory        string
	directoryInfo    os.FileInfo
	journalDigest    hash.Hash
	contract         windowAccountingContract
	state            windowAccountingState
	seen             map[string]bool
	events           int
	previous         string
	poisoned, closed bool
	closeErr         error
}

var (
	windowProviderModelPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]{1,128}$`)
	windowIDPattern            = regexp.MustCompile(`^window-[0-9]{3,6}$`)
	windowCaseKeyPattern       = regexp.MustCompile(`^[a-z0-9_./-]{1,256}$`)
)

func (p windowProviderLimits) cost(input, output int64) int64 {
	return (input*p.InputRate+999999)/1000000 + (output*p.OutputRate+999999)/1000000
}
func (p windowProviderLimits) validate(model bool) error {
	if !windowProviderModelPattern.MatchString(p.Model) || p.MaxAttempts < 1 || p.MaxAttempts > 10000 || p.MaxRequestBytes < 1 || p.MaxRequestBytes > 1<<20 || p.MaxInput < 1 || p.MaxInput > 10000000 || p.MaxOutput < 0 || p.MaxOutput > 1000000 || p.InputRate < 1 || p.InputRate > 1000000000 || p.OutputRate < 0 || p.OutputRate > 1000000000 {
		return fmt.Errorf("invalid provider bounds/rates")
	}
	if model && (p.MaxOutput < 1 || p.MaxOutput > 512 || p.OutputRate == 0) {
		return fmt.Errorf("invalid model output bound/rate")
	}
	return nil
}
func (c windowAccountingContract) validate() error {
	if c.Schema != "full-window-accounting.v1" || !windowIDPattern.MatchString(c.WindowID) || c.ModelCap < 1 || c.ModelCap > 1000000000 || c.JEVCap < 1 || c.JEVCap > 1000000000 {
		return fmt.Errorf("invalid window declaration")
	}
	for _, h := range []string{c.ScopeSHA256, c.ScheduleSHA256, c.PolicySHA256, c.ClaimSHA256} {
		if !comparisonDigest(h) {
			return fmt.Errorf("window identity digest missing")
		}
	}
	if err := c.Model.validate(true); err != nil {
		return err
	}
	if err := c.JEV.validate(false); err != nil {
		return err
	}
	if c.Model.cost(c.Model.MaxInput, c.Model.MaxOutput) > c.ModelCap || c.JEV.cost(c.JEV.MaxInput, c.JEV.MaxOutput) > c.JEVCap {
		return fmt.Errorf("window cap cannot admit one maximum hold")
	}
	return nil
}
func (c windowAccountingContract) provider(name string) (windowProviderLimits, error) {
	if name == "model" {
		return c.Model, nil
	}
	if name == "jev" {
		return c.JEV, nil
	}
	return windowProviderLimits{}, fmt.Errorf("unknown provider")
}
func windowRequestSlot(r windowRequestIdentity) string {
	r.BodySHA256 = ""
	r.Bytes = 0
	raw, _ := json.Marshal(r)
	return conversationHash(raw)
}
func (c windowAccountingContract) validateRequest(r windowRequestIdentity) error {
	p, err := c.provider(r.Provider)
	if err != nil {
		return err
	}
	if r.Model != p.Model || !windowCaseKeyPattern.MatchString(r.CaseKey) || r.Pass < 1 || r.Pass > 3 || (r.Arm != "model" && r.Arm != "jev_selective") || (r.Provider == "jev" && r.Arm != "jev_selective") || r.Turn < 1 || r.Turn > 32 || r.Attempt < 1 || r.Attempt > p.MaxAttempts || !comparisonDigest(r.BodySHA256) || r.Bytes < 1 || r.Bytes > p.MaxRequestBytes {
		return fmt.Errorf("invalid request identity/bounds")
	}
	return nil
}
func windowUsageValid(p windowProviderLimits, u *windowUsage) bool {
	if u == nil || u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 || *u.Input > p.MaxInput || *u.Output > p.MaxOutput {
		return false
	}
	if (u.CacheHit == nil) != (u.CacheMiss == nil) {
		return false
	}
	return u.CacheHit == nil || (*u.CacheHit >= 0 && *u.CacheMiss >= 0 && *u.CacheHit <= p.MaxInput && *u.CacheMiss <= p.MaxInput && *u.CacheHit+*u.CacheMiss == *u.Input)
}
func windowSyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func newWindowAccounting(dir string, c windowAccountingContract) (*windowAccounting, error) {
	return newWindowAccountingWithFile(dir, c, nil)
}
func newWindowAccountingWithFile(dir string, c windowAccountingContract, wrap func(*os.File) windowJournalFile) (*windowAccounting, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, fmt.Errorf("new owned window directory required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, fmt.Errorf("window already exists or parent unavailable")
	}
	f, err := os.OpenFile(filepath.Join(dir, "accounting.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	var file windowJournalFile = f
	if wrap != nil {
		file = wrap(f)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		file.Close()
		return nil, err
	}
	l := &windowAccounting{file: file, journal: f, directory: dir, directoryInfo: info, journalDigest: sha256.New(), contract: c, state: windowAccountingState{ModelPermanent: c.ModelCap, JEVPermanent: c.JEVCap}, seen: map[string]bool{}}
	if err = l.append(windowAccountingEvent{Kind: "config", Contract: &c}); err == nil {
		err = windowSyncDirectory(dir)
	}
	if err == nil {
		err = windowSyncDirectory(filepath.Dir(dir))
	}
	if err != nil {
		file.Close()
		return nil, err
	} // Created evidence is deliberately never removed.
	return l, nil
}

// validatePathLocked compares named evidence to the actual live descriptor and
// the bytes successfully synced by this owner. It never provides replay authority.
func (l *windowAccounting) validatePathLocked() error {
	reject := func() error { return fmt.Errorf("owned ledger path changed") }
	if l.journal == nil || l.journalDigest == nil || l.directoryInfo == nil {
		return reject()
	}
	dir, err := os.Lstat(l.directory)
	if err != nil || !dir.IsDir() || !os.SameFile(dir, l.directoryInfo) {
		return reject()
	}
	entries, err := os.ReadDir(l.directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "accounting.jsonl" {
		return reject()
	}
	live, err := l.journal.Stat()
	if err != nil {
		return reject()
	}
	path := filepath.Join(l.directory, "accounting.jsonl")
	named, err := os.Lstat(path)
	if err != nil || !named.Mode().IsRegular() || !os.SameFile(live, named) || named.Size() > 64<<20 {
		return reject()
	}
	raw, err := os.ReadFile(path)
	if err != nil || conversationHash(raw) != fmt.Sprintf("%x", l.journalDigest.Sum(nil)) {
		return reject()
	}
	return nil
}
func (l *windowAccounting) validateOwnedPath() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned || l.closed {
		return fmt.Errorf("owned ledger unavailable")
	}
	if err := l.validatePathLocked(); err != nil {
		l.poisoned = true
		l.state.Halt = "durability_failure"
		return err
	}
	return nil
}
func (l *windowAccounting) append(e windowAccountingEvent) error {
	if err := l.validatePathLocked(); err != nil {
		l.poisoned = true
		l.state.Halt = "durability_failure"
		return err
	}
	e.Index = l.events
	e.PreviousSHA256 = l.previous
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	n, err := l.file.Write(append(raw, '\n'))
	if err == nil && n != len(raw)+1 {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = l.file.Sync()
	}
	if err != nil {
		l.poisoned = true
		l.state.Halt = "durability_failure"
		return err
	}
	// A replacement or unexpected write during Sync must not settle an event.
	l.journalDigest.Write(append(raw, '\n'))
	if err = l.validatePathLocked(); err != nil {
		l.poisoned = true
		l.state.Halt = "durability_failure"
		return err
	}
	if err = l.apply(e); err != nil {
		l.poisoned = true
		l.state.Halt = "invalid_internal_event"
		return err
	}
	l.previous = conversationHash(raw)
	l.events++
	return nil
}
func (l *windowAccounting) apply(e windowAccountingEvent) error {
	if e.Index != l.events || e.PreviousSHA256 != l.previous {
		return fmt.Errorf("ledger order/hash mismatch")
	}
	if l.events == 0 {
		if e.Kind != "config" || e.Contract == nil || *e.Contract != l.contract || e.Sequence != 0 || e.Request != nil || e.Usage != nil || e.Code != "" || e.ResponseSHA256 != "" {
			return fmt.Errorf("invalid ledger header")
		}
		return nil
	}
	if l.state.Closed || e.Contract != nil {
		return fmt.Errorf("event after close or duplicate header")
	}
	switch e.Kind {
	case "intent":
		if l.state.Halt != "" || l.state.Pending != nil || e.Request == nil || e.Usage != nil || e.ResponseSHA256 != "" || e.Code != "" || e.Sequence != l.state.ModelAttempts+l.state.JEVAttempts+1 {
			return fmt.Errorf("invalid intent sequence")
		}
		r := *e.Request
		if err := l.contract.validateRequest(r); err != nil {
			return err
		}
		if l.seen[windowRequestSlot(r)] {
			return fmt.Errorf("duplicate request identity")
		}
		p, _ := l.contract.provider(r.Provider)
		hold := p.cost(p.MaxInput, p.MaxOutput)
		if r.Provider == "model" {
			if l.state.ModelAttempts >= p.MaxAttempts || l.state.ModelKnown > l.contract.ModelCap-hold {
				return fmt.Errorf("model cap exceeded")
			}
			l.state.ModelHeld = hold
			l.state.ModelAttempts++
		} else {
			if l.state.JEVAttempts >= p.MaxAttempts || l.state.JEVKnown > l.contract.JEVCap-hold {
				return fmt.Errorf("JEV cap exceeded")
			}
			l.state.JEVHeld = hold
			l.state.JEVAttempts++
		}
		l.seen[windowRequestSlot(r)] = true
		l.state.Pending = &windowPendingIntent{Sequence: e.Sequence, Request: r}
	case "settle":
		if l.state.Halt != "" || l.state.Pending == nil || e.Sequence != l.state.Pending.Sequence || e.Request != nil || e.Code != "" || !comparisonDigest(e.ResponseSHA256) {
			return fmt.Errorf("invalid settlement sequence")
		}
		provider := l.state.Pending.Request.Provider
		p, _ := l.contract.provider(provider)
		if !windowUsageValid(p, e.Usage) {
			return fmt.Errorf("invalid bounded usage")
		}
		cost := p.cost(*e.Usage.Input, *e.Usage.Output)
		if provider == "model" {
			if l.state.ModelKnown > l.contract.ModelCap-cost {
				return fmt.Errorf("settlement exceeds cap")
			}
			l.state.ModelKnown += cost
			l.state.ModelHeld = 0
		} else {
			if l.state.JEVKnown > l.contract.JEVCap-cost {
				return fmt.Errorf("settlement exceeds cap")
			}
			l.state.JEVKnown += cost
			l.state.JEVHeld = 0
		}
		l.state.Pending = nil
	case "halt":
		if l.state.Halt != "" || e.Sequence != 0 || e.Request != nil || e.Usage != nil || e.ResponseSHA256 != "" || !hasString([]string{"invalid_request", "concurrent_request", "duplicate_request", "budget_exhausted", "invalid_ticket", "upstream_failure", "protocol_failure", "invalid_failure", "usage_missing", "usage_invalid", "usage_out_of_bounds", "response_identity", "pending_at_close"}, e.Code) {
			return fmt.Errorf("invalid halt")
		}
		l.state.Halt = e.Code
		if e.Code == "usage_out_of_bounds" {
			l.state.EnvelopeViolation = true
		}
	case "close":
		if e.Sequence != 0 || e.Request != nil || e.Usage != nil || e.ResponseSHA256 != "" || e.Code != "" || (l.state.Pending != nil && l.state.Halt == "") {
			return fmt.Errorf("unresolved close")
		}
		l.state.Closed = true
	default:
		return fmt.Errorf("unknown ledger event")
	}
	return nil
}
func (l *windowAccounting) halt(code string) error {
	if l.poisoned || l.closed || l.state.Halt != "" {
		return fmt.Errorf("window halted or closed")
	}
	if err := l.append(windowAccountingEvent{Kind: "halt", Code: code}); err != nil {
		return err
	}
	return fmt.Errorf("window halted: %s", code)
}
func (l *windowAccounting) Begin(r windowRequestIdentity) (windowRequestTicket, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned || l.closed || l.state.Halt != "" {
		return windowRequestTicket{}, fmt.Errorf("window halted or closed")
	}
	if l.state.Pending != nil {
		return windowRequestTicket{}, l.halt("concurrent_request")
	}
	if l.contract.validateRequest(r) != nil {
		return windowRequestTicket{}, l.halt("invalid_request")
	}
	if l.seen[windowRequestSlot(r)] {
		return windowRequestTicket{}, l.halt("duplicate_request")
	}
	p, _ := l.contract.provider(r.Provider)
	hold := p.cost(p.MaxInput, p.MaxOutput)
	known, cap, attempts := l.state.ModelKnown, l.contract.ModelCap, l.state.ModelAttempts
	if r.Provider == "jev" {
		known, cap, attempts = l.state.JEVKnown, l.contract.JEVCap, l.state.JEVAttempts
	}
	if attempts >= p.MaxAttempts || known > cap-hold {
		return windowRequestTicket{}, l.halt("budget_exhausted")
	}
	seq := l.state.ModelAttempts + l.state.JEVAttempts + 1
	if err := l.append(windowAccountingEvent{Kind: "intent", Sequence: seq, Request: &r}); err != nil {
		return windowRequestTicket{}, err
	}
	return windowRequestTicket{owner: l, sequence: seq}, nil
}
func (l *windowAccounting) validTicket(t windowRequestTicket) bool {
	return t.owner == l && l.state.Pending != nil && t.sequence == l.state.Pending.Sequence
}
func (l *windowAccounting) Settle(t windowRequestTicket, responseSHA string, usage *windowUsage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned || l.closed || l.state.Halt != "" {
		return fmt.Errorf("window halted or closed")
	}
	if !l.validTicket(t) {
		return l.halt("invalid_ticket")
	}
	if !comparisonDigest(responseSHA) {
		return l.halt("response_identity")
	}
	p, _ := l.contract.provider(l.state.Pending.Request.Provider)
	if !windowUsageValid(p, usage) {
		code := "usage_invalid"
		if usage == nil || usage.Input == nil || usage.Output == nil {
			code = "usage_missing"
		} else if *usage.Input > p.MaxInput || *usage.Output > p.MaxOutput {
			code = "usage_out_of_bounds"
		}
		return l.halt(code)
	}
	return l.append(windowAccountingEvent{Kind: "settle", Sequence: t.sequence, ResponseSHA256: responseSHA, Usage: usage})
}
func (l *windowAccounting) Fail(t windowRequestTicket, code string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned || l.closed || l.state.Halt != "" {
		return fmt.Errorf("window halted or closed")
	}
	if !l.validTicket(t) {
		return l.halt("invalid_ticket")
	}
	if code != "upstream_failure" && code != "protocol_failure" && code != "usage_out_of_bounds" {
		code = "invalid_failure"
	}
	return l.halt(code)
}
func (l *windowAccounting) Snapshot() windowAccountingState {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state
	if s.Pending != nil {
		copy := *s.Pending
		s.Pending = &copy
	}
	return s
}
func (l *windowAccounting) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeErr
	}
	var err error
	if !l.poisoned {
		if l.state.Pending != nil && l.state.Halt == "" {
			_ = l.halt("pending_at_close")
		}
		if !l.poisoned {
			err = l.append(windowAccountingEvent{Kind: "close"})
		}
	}
	if l.poisoned {
		err = errors.Join(err, fmt.Errorf("window journal durability not verified"))
	}
	if l.state.Halt != "" {
		err = errors.Join(err, fmt.Errorf("window halted: %s", l.state.Halt))
	}
	l.closed = true
	l.closeErr = errors.Join(err, l.file.Close())
	return l.closeErr
}
func readWindowAccounting(dir string) (windowAccountingAudit, error) {
	var audit windowAccountingAudit
	entries, err := os.ReadDir(dir)
	if err != nil {
		return audit, err
	}
	if len(entries) != 1 || entries[0].Name() != "accounting.jsonl" || !entries[0].Type().IsRegular() {
		return audit, fmt.Errorf("unknown/orphaned window files")
	}
	file := filepath.Join(dir, "accounting.jsonl")
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return audit, fmt.Errorf("invalid ledger file")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return audit, err
	}
	return readWindowAccountingBytes(raw)
}

// Reuse the canonical replay rules after a caller has safely read and bound
// immutable bytes, without reopening an evidence path during verification.
func readWindowAccountingBytes(raw []byte) (windowAccountingAudit, error) {
	var audit windowAccountingAudit
	if len(raw) == 0 || len(raw) > 64<<20 || raw[len(raw)-1] != '\n' {
		return audit, fmt.Errorf("truncated ledger")
	}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), 1<<20)
	l := &windowAccounting{seen: map[string]bool{}}
	for scan.Scan() {
		line := scan.Bytes()
		var e windowAccountingEvent
		if json.Unmarshal(line, &e) != nil {
			return audit, fmt.Errorf("invalid ledger JSON")
		}
		canonical, err := json.Marshal(e)
		if err != nil || !bytes.Equal(line, canonical) {
			return audit, fmt.Errorf("noncanonical/unknown ledger fields")
		}
		if l.events == 0 {
			if e.Contract == nil {
				return audit, fmt.Errorf("missing contract")
			}
			l.contract = *e.Contract
			if err := l.contract.validate(); err != nil {
				return audit, err
			}
			l.state = windowAccountingState{ModelPermanent: l.contract.ModelCap, JEVPermanent: l.contract.JEVCap}
		}
		if err := l.apply(e); err != nil {
			return audit, err
		}
		l.previous = conversationHash(line)
		l.events++
	}
	if err := scan.Err(); err != nil {
		return audit, err
	}
	if !l.state.Closed && l.state.Halt == "" {
		l.state.Halt = "unsealed_window"
	}
	return windowAccountingAudit{CompletionProof: "unavailable_from_journal", Contract: l.contract, State: l.state, JournalSHA256: conversationHash(raw), Events: l.events}, nil
}
