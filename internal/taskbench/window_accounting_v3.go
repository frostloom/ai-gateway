package taskbench

import (
	"fmt"
	"math"
)

const windowAccountingV3 = "full-window-accounting.v3"

// These records are accounting data supplied by a future verified adapter.
// Neither a digest nor a ticket establishes a real claim, TLS eligibility,
// global quota, ownership of a driver, or permission to send a request.
type windowUnknownIntent struct {
	Sequence         int                   `json:"sequence"`
	Request          windowRequestIdentity `json:"request"`
	DescriptorSHA256 string                `json:"descriptor_sha256"`
	MaxHold          int64                 `json:"max_hold"`
	ExtraClaimSHA256 string                `json:"extra_claim_sha256,omitempty"`
}

func (e windowAccountingEvent) hasV3Fields() bool {
	return e.DescriptorSHA256 != "" || e.UnknownSequence != 0 || e.Provider != "" || e.Amount != 0 || e.ExtraClaimSHA256 != ""
}
func windowCheckedAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, fmt.Errorf("invalid accounting sum")
	}
	return a + b, nil
}

// liabilityV3 never changes the immutable base declaration.
func (l *windowAccounting) liabilityV3(provider string, next int64) error {
	known, unknown, pending, base, extra := l.state.ModelKnown, l.state.ModelUnknownHeld, l.state.ModelHeld, l.contract.ModelCap, l.state.ModelExtraFunded
	if provider == "jev" {
		known, unknown, pending, base, extra = l.state.JEVKnown, l.state.JEVUnknownHeld, l.state.JEVHeld, l.contract.JEVCap, l.state.JEVExtraFunded
	}
	cap, err := windowCheckedAdd(base, extra)
	if err != nil {
		return err
	}
	total, err := windowCheckedAdd(known, unknown)
	if err != nil {
		return err
	}
	total, err = windowCheckedAdd(total, pending)
	if err != nil {
		return err
	}
	total, err = windowCheckedAdd(total, next)
	if err != nil {
		return err
	}
	if total > cap {
		return fmt.Errorf("funded cap exceeded")
	}
	return nil
}
func windowSameLogical(a, b windowRequestIdentity) bool {
	a.NativeAttempt = 0
	b.NativeAttempt = 0
	return a == b
}
func (l *windowAccounting) requiredUnknownV3() *windowUnknownIntent {
	if l.state.RetryRequired == nil {
		return nil
	}
	for i := range l.state.UnknownIntents {
		u := &l.state.UnknownIntents[i]
		if u.Sequence == l.state.RetryRequired.Sequence && u.Request == l.state.RetryRequired.Request {
			return u
		}
	}
	return nil
}
func (l *windowAccounting) checkIntentV3(r windowRequestIdentity) error {
	if l.state.Halt != "" || l.state.Pending != nil {
		return fmt.Errorf("pending or halted")
	}
	if err := l.contract.validateRequest(r); err != nil {
		return err
	}
	if l.seen[windowRequestSlot(r)] {
		return fmt.Errorf("duplicate native intent")
	}
	if l.state.RetryRequired != nil {
		u := l.requiredUnknownV3()
		if u == nil || u.ExtraClaimSHA256 == "" || r.NativeAttempt != 2 || !windowSameLogical(u.Request, r) {
			return fmt.Errorf("required funded retry mismatch")
		}
	} else if r.NativeAttempt != 1 {
		return fmt.Errorf("unsolicited retry")
	}
	p, _ := l.contract.provider(r.Provider)
	attempts := l.state.ModelAttempts
	if r.Provider == "jev" {
		attempts = l.state.JEVAttempts
	}
	if attempts >= p.MaxAttempts {
		return fmt.Errorf("native intent limit exceeded")
	}
	return l.liabilityV3(r.Provider, p.cost(p.MaxInput, p.MaxOutput))
}
func (l *windowAccounting) beginV3(r windowRequestIdentity) (windowRequestTicket, error) {
	if err := l.checkIntentV3(r); err != nil {
		return windowRequestTicket{}, l.halt("invalid_request")
	}
	seq := l.state.ModelAttempts + l.state.JEVAttempts + 1
	if err := l.append(windowAccountingEvent{Kind: "intent", Sequence: seq, Request: &r}); err != nil {
		return windowRequestTicket{}, err
	}
	return windowRequestTicket{owner: l, sequence: seq}, nil
}
func (l *windowAccounting) EndUnknown(t windowRequestTicket, descriptorSHA256 string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned || l.closed || l.state.Halt != "" {
		return fmt.Errorf("window halted or closed")
	}
	if l.contract.Schema != windowAccountingV3 || !l.validTicket(t) || l.state.Pending.Request.NativeAttempt != 1 || l.state.RetryRequired != nil || !comparisonDigest(descriptorSHA256) {
		return l.halt("invalid_request")
	}
	return l.append(windowAccountingEvent{Kind: "unknown", Sequence: t.sequence, DescriptorSHA256: descriptorSHA256})
}
func (l *windowAccounting) checkFundingV3(sequence int, provider string, amount int64, claimSHA string) error {
	if l.contract.Schema != windowAccountingV3 || l.state.Halt != "" || l.state.Pending != nil || !comparisonDigest(claimSHA) {
		return fmt.Errorf("funding unavailable")
	}
	u := l.requiredUnknownV3()
	if u == nil || u.Sequence != sequence || u.Request.Provider != provider || u.MaxHold != amount || amount <= 0 || u.ExtraClaimSHA256 != "" {
		return fmt.Errorf("funding association mismatch")
	}
	count := 0
	for _, other := range l.state.UnknownIntents {
		if other.ExtraClaimSHA256 == claimSHA {
			return fmt.Errorf("duplicate extra claim digest")
		}
		if other.Request.Provider == provider && other.ExtraClaimSHA256 != "" {
			count++
		}
	}
	if count >= 20 {
		return fmt.Errorf("local extra count exceeded")
	}
	extra := l.state.ModelExtraFunded
	if provider == "jev" {
		extra = l.state.JEVExtraFunded
	}
	_, err := windowCheckedAdd(extra, amount)
	return err
}

// RecordExtraFunding records externally verified data; it does not create or
// verify a financial claim and must never be interpreted as egress authority.
func (l *windowAccounting) RecordExtraFunding(sequence int, provider string, amount int64, claimSHA256 string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned || l.closed || l.state.Halt != "" {
		return fmt.Errorf("window halted or closed")
	}
	if err := l.checkFundingV3(sequence, provider, amount, claimSHA256); err != nil {
		return l.halt("invalid_request")
	}
	return l.append(windowAccountingEvent{Kind: "extra_funding", UnknownSequence: sequence, Provider: provider, Amount: amount, ExtraClaimSHA256: claimSHA256})
}
func (l *windowAccounting) applyV3(e windowAccountingEvent) error {
	if e.Index != l.events || e.PreviousSHA256 != l.previous {
		return fmt.Errorf("ledger order/hash mismatch")
	}
	if l.events == 0 {
		if e.hasV3Fields() {
			return fmt.Errorf("extra fields in header")
		}
		return l.applyLegacy(e)
	}
	if l.state.Closed || e.Contract != nil {
		return fmt.Errorf("event after close or duplicate header")
	}
	if e.Kind != "unknown" && e.Kind != "extra_funding" && e.hasV3Fields() {
		return fmt.Errorf("extra v3 event fields")
	}
	switch e.Kind {
	case "intent":
		if e.Request == nil || e.Sequence != l.state.ModelAttempts+l.state.JEVAttempts+1 || e.Usage != nil || e.Code != "" || e.ResponseSHA256 != "" {
			return fmt.Errorf("invalid native intent event")
		}
		r := *e.Request
		if err := l.checkIntentV3(r); err != nil {
			return err
		}
		p, _ := l.contract.provider(r.Provider)
		hold := p.cost(p.MaxInput, p.MaxOutput)
		if r.Provider == "model" {
			l.state.ModelHeld = hold
			l.state.ModelAttempts++
		} else {
			l.state.JEVHeld = hold
			l.state.JEVAttempts++
		}
		l.seen[windowRequestSlot(r)] = true
		l.state.Pending = &windowPendingIntent{Sequence: e.Sequence, Request: r}
	case "unknown":
		if l.state.Halt != "" || l.state.Pending == nil || l.state.RetryRequired != nil || l.state.Pending.Request.NativeAttempt != 1 || e.Sequence != l.state.Pending.Sequence || !comparisonDigest(e.DescriptorSHA256) || e.UnknownSequence != 0 || e.Provider != "" || e.Amount != 0 || e.ExtraClaimSHA256 != "" || e.Request != nil || e.Usage != nil || e.Code != "" || e.ResponseSHA256 != "" {
			return fmt.Errorf("invalid unknown event")
		}
		pending := *l.state.Pending
		p, _ := l.contract.provider(pending.Request.Provider)
		hold := p.cost(p.MaxInput, p.MaxOutput)
		old := l.state.ModelUnknownHeld
		if pending.Request.Provider == "jev" {
			old = l.state.JEVUnknownHeld
		}
		total, err := windowCheckedAdd(old, hold)
		if err != nil {
			return err
		}
		l.state.UnknownIntents = append(l.state.UnknownIntents, windowUnknownIntent{Sequence: e.Sequence, Request: pending.Request, DescriptorSHA256: e.DescriptorSHA256, MaxHold: hold})
		if pending.Request.Provider == "model" {
			l.state.ModelUnknownHeld = total
			l.state.ModelHeld = 0
		} else {
			l.state.JEVUnknownHeld = total
			l.state.JEVHeld = 0
		}
		l.state.Pending = nil
		l.state.RetryRequired = &pending
	case "extra_funding":
		if e.Sequence != 0 || e.Request != nil || e.Usage != nil || e.ResponseSHA256 != "" || e.Code != "" || e.DescriptorSHA256 != "" {
			return fmt.Errorf("invalid funding fields")
		}
		if err := l.checkFundingV3(e.UnknownSequence, e.Provider, e.Amount, e.ExtraClaimSHA256); err != nil {
			return err
		}
		u := l.requiredUnknownV3()
		if e.Provider == "model" {
			l.state.ModelExtraFunded += e.Amount
		} else {
			l.state.JEVExtraFunded += e.Amount
		}
		u.ExtraClaimSHA256 = e.ExtraClaimSHA256
	case "settle":
		if l.state.Halt != "" || l.state.Pending == nil || e.Sequence != l.state.Pending.Sequence || e.Request != nil || e.Code != "" || !comparisonDigest(e.ResponseSHA256) {
			return fmt.Errorf("invalid settlement sequence")
		}
		r := l.state.Pending.Request
		p, _ := l.contract.provider(r.Provider)
		cost, err := p.settlementCost(e.Usage)
		if err != nil {
			return err
		}
		// A valid bounded settlement cannot cost more than its existing maximum hold.
		if cost > p.cost(p.MaxInput, p.MaxOutput) {
			return fmt.Errorf("settlement exceeds hold")
		}
		if err = l.liabilityV3(r.Provider, 0); err != nil {
			return err
		}
		known := l.state.ModelKnown
		if r.Provider == "jev" {
			known = l.state.JEVKnown
		}
		known, err = windowCheckedAdd(known, cost)
		if err != nil {
			return err
		}
		if r.Provider == "model" {
			l.state.ModelKnown = known
			l.state.ModelHeld = 0
		} else {
			l.state.JEVKnown = known
			l.state.JEVHeld = 0
		}
		l.state.Pending = nil
		if r.NativeAttempt == 2 {
			l.state.RetryRequired = nil
		}
	case "close":
		if l.state.RetryRequired != nil && l.state.Halt == "" {
			return fmt.Errorf("required retry unresolved")
		}
		return l.applyLegacy(e)
	case "halt":
		return l.applyLegacy(e)
	default:
		return fmt.Errorf("unknown ledger event")
	}
	return nil
}
