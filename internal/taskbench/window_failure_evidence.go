package taskbench

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/frostloom/ai-gateway/internal/observability"
)

// Diagnostic data is deliberately a different type from every completion and
// quality proof. No execution entry enables this option by default.
type windowFailureEvidence struct {
	Schema           string                           `json:"schema"`
	ExecutionSHA256  string                           `json:"execution_sha256"`
	ClaimSHA256      string                           `json:"claim_sha256"`
	WindowIndex      int                              `json:"window_index"`
	Ordinal          int                              `json:"ordinal"`
	Arm              string                           `json:"arm"`
	SessionSHA256    string                           `json:"session_sha256,omitempty"`
	SessionIdentity  string                           `json:"session_identity"`
	Final            string                           `json:"final"`
	FinalReason      string                           `json:"final_reason"`
	Cleanup          string                           `json:"cleanup"`
	CleanupAttempted bool                             `json:"cleanup_attempted"`
	CleanupScope     string                           `json:"cleanup_scope"`
	Observer         string                           `json:"observer"`
	AccountingSource string                           `json:"accounting_source"`
	PendingUnknown   bool                             `json:"pending_unknown"`
	ModelHeld        int64                            `json:"model_held"`
	JEVHeld          int64                            `json:"jev_held"`
	Transport        []windowFailureTransportEvidence `json:"transport,omitempty"`
}
type windowFailureTransportEvidence struct {
	Sequence              int    `json:"sequence"`
	Provider              string `json:"provider"`
	Code                  string `json:"code"`
	Cause                 string `json:"cause,omitempty"`
	Phase                 string `json:"phase,omitempty"`
	ResponseStage         string `json:"response_stage,omitempty"`
	ResponseStatus        int    `json:"response_status"`
	RequestPersistence    string `json:"request_persistence"`
	DescriptorPersistence string `json:"descriptor_persistence"`
	LedgerPersistence     string `json:"ledger_persistence"`
}
type windowFailureRecorder struct {
	mu       sync.Mutex
	evidence windowFailureEvidence
}

type windowFailureDiagnosticCapability interface {
	windowFailureDiagnostics() *windowFailureRecorder
}

func (d *windowOwnedDriver) windowFailureDiagnostics() *windowFailureRecorder {
	if d == nil || d.permit == nil || !d.permit.captureFailureEvidence {
		return nil
	}
	return &d.failure
}

// Caller holds driver.mu. This is identity validation, NOT the execution
// guard: it never grants an action, clears a stop, or returns a proof.
func (d *windowOwnedDriver) failureIdentityLocked() bool {
	p := d.permit
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active != d || p.ledger == nil || p.ledger != d.ledger || p.environment == nil || p.environment != d.environment || p.environment.closed || d.phase != nil || !d.setupDone || p.campaign == nil || d.base == nil {
		return false
	}
	c := p.campaign
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ledger != p.ledger || c.index != p.windowIndex || c.prepared.executionSHA != p.executionSHA || c.claimSHA != p.ledger.contract.ClaimSHA256 || c.index < 1 || c.index > len(c.prepared.scope.Windows) || c.index > len(c.prepared.contracts) {
		return false
	}
	contract := c.prepared.contracts[c.index-1]
	contract.ClaimSHA256 = c.claimSHA
	w := c.prepared.scope.Windows[c.index-1]
	if contract != p.ledger.contract || p.nextArm < 0 || p.nextArm >= 2*(w.LastOrdinal-w.FirstOrdinal+1) || len(p.proofs) != p.nextArm || len(p.proofSHA) != p.nextArm || w.FirstOrdinal < 1 || w.LastOrdinal > len(c.prepared.scope.Slots) {
		return false
	}
	slot := c.prepared.scope.Slots[w.FirstOrdinal-1+p.nextArm/2]
	return slot == d.slot && slot.ArmOrder[p.nextArm%2] == d.arm && c.inventoryWindowCampaign() == nil && p.ledger.validateOwnedPath() == nil
}

func (d *windowOwnedDriver) failureSessionKnownLocked() bool {
	return !d.failureSessionConflict && d.session != "" && len(d.observations) > 0 && d.observations[len(d.observations)-1].Turn.Response.SessionID == d.session
}

// The production State implementation calls MeOverview, which may repair the
// cash projection. Do not call it after a diagnostic stop, even if the runner
// supplies the new session. Preserve its last Step snapshot as a fallback only.
func (d *windowOwnedDriver) failureFinalLocked(ctx context.Context, f Fixture, session string) error {
	reason := "oracle_may_write"
	identity := d.failureIdentityLocked() && reflect.DeepEqual(f, d.fixture) && d.failureSessionKnownLocked() && (session == d.session || session == d.failureStepInputSession)
	if !identity {
		reason = "session_identity_unverified"
	}
	if ctx == nil || ctx.Err() != nil {
		reason = "context_unavailable"
	}
	d.failure.mu.Lock()
	d.failure.evidence.Final = "unknown"
	d.failure.evidence.FinalReason = reason
	d.failure.mu.Unlock()
	d.captureFailureEvidenceLocked()
	return fmt.Errorf("owned terminal final unknown: %s", reason)
}

// This snapshot is observational. It does not ForceFlush or infer delivery,
// and a pending native attempt is never converted into a zero settlement.
func (d *windowOwnedDriver) captureFailureEvidenceLocked() *windowFailureEvidence {
	r := d.windowFailureDiagnostics()
	if r == nil {
		return nil
	}
	identity := d.failureIdentityLocked()
	r.mu.Lock()
	defer r.mu.Unlock()
	e := &r.evidence
	e.Schema = "full-window-failure-diagnostic.v1"
	e.ExecutionSHA256, e.WindowIndex = d.permit.executionSHA, d.permit.windowIndex
	e.Ordinal, e.Arm = d.slot.Ordinal, d.arm
	if d.ledger != nil {
		e.ClaimSHA256 = d.ledger.contract.ClaimSHA256
	}
	e.SessionIdentity = "unknown"
	if d.session != "" {
		e.SessionSHA256 = conversationHash([]byte(d.session))
	}
	if identity && d.failureSessionKnownLocked() {
		e.SessionIdentity = "controlled_response"
	}
	if e.Final == "" {
		e.Final, e.FinalReason = "unknown", "not_obtained"
	}
	if e.Cleanup == "" {
		e.Cleanup = "not_attempted"
	}
	e.CleanupScope = "redis_session_absence_only"
	e.Observer = "unknown"
	if d.observer != nil {
		id := d.observer.EvaluationIdentity()
		if !id.Enabled || id != d.permit.observerIdentity {
			e.Observer = "incomplete"
		} else {
			s := d.observer.Stats()
			e.Observer = windowFailureObserverState(s, d.observerStart, d.nextTurn)
		}
	}
	e.AccountingSource, e.PendingUnknown = "unknown", true
	e.ModelHeld, e.JEVHeld = 0, 0
	// A window ledger is shared across arms. Pointer equality alone cannot
	// attribute its current pending amount to this row's driver/claim/schedule.
	if identity {
		s := d.ledger.Snapshot()
		e.AccountingSource = "owned_live_memory_only"
		e.PendingUnknown, e.ModelHeld, e.JEVHeld = s.Pending != nil, s.ModelHeld, s.JEVHeld
	}
	out := *e
	out.Transport = append([]windowFailureTransportEvidence(nil), e.Transport...)
	return &out
}

func windowFailureObserverState(s, start observability.Stats, turns int) string {
	if s.Failed != 0 || s.Dropped != 0 || s.QueueDepth != 0 || s.Accepted != s.Exported || turns < 0 || s.Turns < start.Turns || s.SampledTurns < start.SampledTurns || s.Turns-start.Turns != uint64(turns) || s.SampledTurns-start.SampledTurns != uint64(turns) {
		return "incomplete"
	}
	return "unknown"
}

// Only called after the synchronous Handler returned, its phase was removed,
// and the permit was revoked. Unknown ownership forbids even session deletion.
func (d *windowOwnedDriver) cleanupFailureLocked(ctx context.Context) error {
	d.stopWindowOwned()
	if d.server != nil {
		d.server.Close()
		d.server = nil
	}
	d.failure.mu.Lock()
	alreadyAttempted := d.failure.evidence.CleanupAttempted
	d.failure.mu.Unlock()
	if alreadyAttempted {
		// Preserve the first deletion/absence failure; a second DEL cannot wash
		// away the error that stopped quality cleanup.
		d.captureFailureEvidenceLocked()
		return fmt.Errorf("owned Handler arm stopped: prior cleanup retained")
	}
	status := "unknown"
	var err error
	if ctx == nil || ctx.Err() != nil || !d.failureIdentityLocked() || !d.failureSessionKnownLocked() || d.environment.rdb == nil {
		err = fmt.Errorf("owned failure cleanup unavailable")
	} else {
		d.failure.mu.Lock()
		d.failure.evidence.CleanupAttempted = true
		d.failure.mu.Unlock()
		err = d.cleanupWindowOwnedSession(ctx)
		status = "verified"
		if err != nil {
			status = "failed"
		}
	}
	d.failure.mu.Lock()
	d.failure.evidence.Cleanup = status
	d.failure.mu.Unlock()
	d.captureFailureEvidenceLocked()
	if err != nil {
		return fmt.Errorf("owned Handler arm stopped: cleanup %s", status)
	}
	return fmt.Errorf("owned Handler arm stopped")
}

func (d *windowOwnedDriver) recordFailureCleanup(status string, attempted bool) {
	if d.windowFailureDiagnostics() == nil {
		return
	}
	d.failure.mu.Lock()
	defer d.failure.mu.Unlock()
	if d.failure.evidence.Cleanup != "failed" {
		d.failure.evidence.Cleanup = status
	}
	d.failure.evidence.CleanupAttempted = d.failure.evidence.CleanupAttempted || attempted
}
