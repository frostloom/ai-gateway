package taskbench

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (w *windowTLSRetryWitness) handoffWindowV8(stop func()) error {
	if w == nil || stop == nil {
		return fmt.Errorf("v8 handoff absent")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runtimeHandoff || !w.eligibleWindowLocked() {
		return fmt.Errorf("v8 handoff rejected")
	}
	w.runtimeHandoff = true
	w.runtimeStop = stop
	return nil
}
func (w *windowTLSRetryWitness) checkWindowV8() error {
	if w == nil {
		return fmt.Errorf("v8 witness absent")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.runtimeHandoff || !w.eligibleWindowLocked() {
		return fmt.Errorf("v8 live handoff invalidated")
	}
	return nil
}
func (w *windowTLSRetryWitness) dispatchWindowV8(send func() (*http.Response, error)) (*http.Response, error) {
	if w == nil || send == nil {
		return nil, fmt.Errorf("v8 dispatch absent")
	}
	w.mu.Lock()
	if !w.runtimeHandoff || w.runtimeDispatched || !w.eligibleWindowLocked() {
		w.mu.Unlock()
		return nil, fmt.Errorf("v8 handoff dispatch rejected")
	}
	w.runtimeDispatched = true
	w.mu.Unlock()
	// The original invocation has returned and sealed before this handoff. Never
	// hold the witness mutex across transport callbacks. Late events stop its
	// owned campaign; original contexts remain attached throughout the new call.
	response, err := send()
	if w.checkWindowV8() != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, fmt.Errorf("v8 handoff invalidated during dispatch")
	}
	return response, err
}

// Every handoff guard checks the same private driver, original phase/context,
// campaign authorization and transport object; arbitrary RoundTrippers cannot retry.
func (t *windowProviderTransport) guardWindowV8(req *http.Request, r windowRequestIdentity, phase *windowOwnedPhase) error {
	d, ok := t.capability.(*windowOwnedDriver)
	if !ok || d == nil || d.permit == nil || req == nil || phase == nil || phase.parent == nil || req.Context().Err() != nil || phase.parent.Err() != nil {
		return fmt.Errorf("v8 controlled transport phase absent")
	}
	p := d.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	if d.phase != phase || phase.driver != d || phase.nonce == "" || req.Context().Value(windowOwnedPhaseKey{}) != phase || !d.setupDone || r.CaseKey != d.slot.CaseKey || r.Pass != d.slot.Pass || r.Arm != d.arm || r.Turn != d.nextTurn+1 || p.guardWindowOwnedLocked(d) != nil || p.acquisition == nil {
		return fmt.Errorf("v8 transport owner changed")
	}
	expected := p.acquisition.auth.modelUpstream
	if t.provider == "jev" {
		expected = p.acquisition.auth.jevUpstream
	}
	tr, ok := t.upstream.(*windowActualNoRedirectTransport)
	if !ok || tr == nil || tr.base == nil || tr.base != t.v8Base || expected != t.upstream {
		return fmt.Errorf("v8 fixed owned transport required")
	}
	c := p.campaign
	c.mu.Lock()
	defer c.mu.Unlock()
	scopeRaw, _ := json.Marshal(c.prepared.scope)
	if c.prepared.scope.Schema != "full-window-scope.v8" || conversationHash(scopeRaw) != c.prepared.scopeSHA || validateWindowV8Declarations(c.prepared.scope) != nil || c.prepared.v8Authority == nil || c.prepared.v8Authority.revalidate() != nil || c.prepared.v8Authority.binding.ScopeSHA256 != c.prepared.scopeSHA || c.prepared.v8Authority.binding.ExecutionSHA256 != c.prepared.executionSHA {
		return fmt.Errorf("v8 live execution authority rejected")
	}
	if p.evidenceKind == "recorded_run" {
		a := c.prepared.v8Authority
		if validateWindowActualSourceFiles(a.manifest.SourceRoot, a.manifest.Sources) != nil {
			return fmt.Errorf("v8 frozen source changed")
		}
	}
	return nil
}
func (t *windowProviderTransport) stopWindowV8Transition() (*http.Response, error) {
	// Native1 has ended unknown and its old ticket is invalid. Do not Fail it.
	_ = windowTransportStop(t.ledger, "upstream_failure")
	if d, ok := t.capability.(*windowOwnedDriver); ok && d.permit != nil && d.permit.campaign != nil {
		c := d.permit.campaign
		c.mu.Lock()
		c.poisoned = true
		c.mu.Unlock()
	}
	return nil, fmt.Errorf("v8 retry transition stopped with permanent liability")
}
