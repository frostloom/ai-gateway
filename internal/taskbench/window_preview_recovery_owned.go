package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func windowOwnedPreviewRecoveryPolicySHA() string {
	return qualityJSONSHA([]string{"full-customer-owned-preview-recovery.v16", windowOwnedQualityPolicySHA(), collectionPreviewRecoveryPolicySHA(), "quiesced-owned-full-sql-session-capture;durable-sidecar-before-cleanup;opaque-owner-seal;all-original-turns;no-old-epoch-selection"})
}
func windowRecoveryPolicyAllowed(scope fullWindowScope) bool {
	return scope.Schema == "full-window-scope.v16" && scope.Bindings.PolicySHA256 == windowOwnedPreviewRecoveryPolicySHA() && scope.CollectionPolicySHA256 == collectionPreviewRecoveryPolicySHA()
}
func (d *windowOwnedDriver) windowPreviewRecoveryMode() bool {
	return d != nil && d.permit != nil && d.permit.campaign != nil && windowRecoveryPolicyAllowed(d.permit.campaign.prepared.scope)
}

type windowPreviewRecoveryEvidence struct {
	PolicySHA256    string `json:"policy_sha256"`
	RecordingSHA256 string `json:"recording_sha256"`
	SidecarFile     string `json:"sidecar_file"`
	SidecarSHA256   string `json:"sidecar_sha256"`
}
type windowPreviewRecoverySidecar struct {
	Schema string                             `json:"schema"`
	Record collectionPreviewRecoveryRecording `json:"record"`
}

func (d *windowOwnedDriver) collectionPreviewRecoveryEvidence(ctx context.Context, turn int) (collectionPreviewRecoveryEvidence, error) {
	if d == nil {
		return collectionPreviewRecoveryEvidence{}, fmt.Errorf("missing preview recovery owner")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || !d.windowPreviewRecoveryMode() || d.phase != nil || turn != d.nextTurn || captureWindowQualityOwner(d) != nil {
		return collectionPreviewRecoveryEvidence{}, fmt.Errorf("stale preview recovery owner")
	}
	for _, e := range d.previewRecoveries {
		if e.Turn == turn {
			return cloneWindowOwned(e)
		}
	}
	return collectionPreviewRecoveryEvidence{}, fmt.Errorf("missing owned preview recovery capture")
}

// Called with driver.mu while no Handler/native phase is active. Captures do
// not grant a permit, and the exact policy is rerun before any next turn.
func (d *windowOwnedDriver) captureWindowPreviewRecovery(ctx context.Context, before qualityEffectCapture, previous json.RawMessage, obs ConversationObservation) error {
	if d == nil || ctx == nil || ctx.Err() != nil || !d.windowPreviewRecoveryMode() || d.phase != nil || captureWindowQualityOwner(d) != nil {
		return fmt.Errorf("recovery owner unavailable")
	}
	needed, orders := false, false
	for _, tr := range obs.Turn.Response.Trace {
		if tr.Phase == "preview" && tr.Outcome == "failure" {
			needed = true
			orders = orders || tr.Tool == "refund_recharge"
		}
	}
	if !needed {
		return nil
	}
	after, err := captureWindowQualityEffects(ctx, d)
	if err != nil {
		return err
	}
	session, pending, err := captureWindowQualitySession(ctx, d, false)
	if err != nil {
		return fmt.Errorf("preview recovery session not quiescent")
	}
	in := collectionPreviewRecoveryEvidence{Turn: d.nextTurn, BeforeSession: previous, AfterSession: session, Before: before, After: after, Pending: pending}
	if orders {
		rows := []store.RechargeOrder{}
		if err = d.environment.db.WithContext(ctx).Order("id").Limit(100001).Find(&rows).Error; err != nil || len(rows) > 100000 {
			return fmt.Errorf("preview recovery full orders unavailable")
		}
		in.Orders = &collectionPreviewFailureOrders{Columns: append([]string(nil), after.Tables["recharge_orders"].Columns...), Rows: rows}
	}
	if err = verifyCollectionPreviewRecoveryEvidence(d.caseDefinition, d.fixture, before.State, obs, in); err != nil {
		return err
	}
	if ctx.Err() != nil || captureWindowQualityOwner(d) != nil {
		return fmt.Errorf("preview recovery owner changed after capture")
	}
	detached, err := cloneWindowOwned(in)
	if err != nil {
		return err
	}
	d.previewRecoveries = append(d.previewRecoveries, detached)
	return nil
}
func (d *windowOwnedDriver) previewRecoveryReplay(final ConversationState) *collectionPreviewRecoveryReplay {
	return &collectionPreviewRecoveryReplay{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: d.caseDefinition.Key(), Fixture: d.fixture, Initial: d.initial, Final: final, Steps: d.observations}}, evidence: d.previewRecoveries}
}
func (d *windowOwnedDriver) verifyOwnedRecoveryRecord(record collectionRecording) error {
	if !d.windowPreviewRecoveryMode() {
		return fmt.Errorf("not a recovery owner")
	}
	return verifyCollectionPreviewRecoveryRecording(collectionPreviewRecoveryRecording{collectionRecording: record, PreviewRecoveries: d.previewRecoveries})
}
func (d *windowOwnedDriver) retainWindowPreviewRecovery(record collectionRecording) error {
	if !d.windowPreviewRecoveryMode() || d.qualityCandidate != nil {
		return nil
	}
	r := d.permit.acquisition
	if r == nil || r.validateWindowQualityFiles() != nil || d.phase != nil {
		return fmt.Errorf("preview recovery acquisition unavailable")
	}
	sidecar := windowPreviewRecoverySidecar{Schema: "full-window-preview-recovery-sidecar.v16", Record: collectionPreviewRecoveryRecording{collectionRecording: record, PreviewRecoveries: d.previewRecoveries}}
	if verifyCollectionPreviewRecoveryRecording(sidecar.Record) != nil {
		return fmt.Errorf("preview recovery replay rejected")
	}
	raw, err := json.Marshal(sidecar)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("preview-recovery-%06d-%s.json", d.slot.Ordinal, d.arm)
	if err = r.persistWindowAcquisition(name, raw); err != nil {
		return err
	}
	r.recordFiles[name] = conversationHash(raw)
	recordRaw, err := json.Marshal(record)
	if err != nil || r.validateWindowQualityFiles() != nil {
		return fmt.Errorf("preview recovery persistence changed")
	}
	d.previewRecoveryRetained = &windowPreviewRecoveryEvidence{PolicySHA256: windowOwnedPreviewRecoveryPolicySHA(), RecordingSHA256: conversationHash(recordRaw), SidecarFile: name, SidecarSHA256: conversationHash(raw)}
	return nil
}

// Pure source replay checks the sidecar and matching base record. It never
// substitutes for the opaque live proof or the authenticated file namespace.
func verifyWindowPreviewRecoverySidecar(raw []byte, record collectionRecording, evidence *windowPreviewRecoveryEvidence) error {
	if validateWindowPreviewRecoveryEvidence(evidence, windowV8DeclarationSHA(record)) != nil || conversationHash(raw) != evidence.SidecarSHA256 {
		return fmt.Errorf("preview recovery sidecar binding differs")
	}
	var sidecar windowPreviewRecoverySidecar
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if validateWindowJSONShape(raw, false) != nil || decoder.Decode(&sidecar) != nil || sidecar.Schema != "full-window-preview-recovery-sidecar.v16" || !qualityJSONEqual(sidecar.Record.collectionRecording, record) || verifyCollectionPreviewRecoveryRecording(sidecar.Record) != nil {
		return fmt.Errorf("preview recovery sidecar replay differs")
	}
	return nil
}
func validateWindowPreviewRecoveryEvidence(e *windowPreviewRecoveryEvidence, recordSHA string) error {
	if e == nil || e.PolicySHA256 != windowOwnedPreviewRecoveryPolicySHA() || !comparisonDigest(recordSHA) || e.RecordingSHA256 != recordSHA || !comparisonDigest(e.SidecarSHA256) || filepath.Base(e.SidecarFile) != e.SidecarFile || !strings.HasPrefix(e.SidecarFile, "preview-recovery-") || strings.ContainsAny(e.SidecarFile, "/\\") || filepath.Ext(e.SidecarFile) != ".json" {
		return fmt.Errorf("preview recovery evidence differs")
	}
	return nil
}
func windowPreviewRecoveryProofEvidence(p *windowOwnedArmProof) (*windowPreviewRecoveryEvidence, error) {
	if p == nil || p.driver == nil {
		return nil, fmt.Errorf("preview recovery proof absent")
	}
	d := p.driver
	if !d.windowPreviewRecoveryMode() || p.quality != nil {
		if p.previewRecovery != nil || d.previewRecoveryRetained != nil {
			return nil, fmt.Errorf("cross-policy preview recovery evidence")
		}
		return nil, nil
	}
	if p.previewFailure != nil || d.previewFailureRetained != nil {
		return nil, fmt.Errorf("cross-policy recovery proof")
	}
	if p.permit == nil || p.permit != d.permit {
		return nil, fmt.Errorf("recovery proof owner differs")
	}
	r := p.permit.acquisition
	if p.permit != d.permit || p.permit.stopped || r == nil || r.validateWindowQualityFiles() != nil || !reflect.DeepEqual(p.previewRecovery, d.previewRecoveryRetained) || validateWindowPreviewRecoveryEvidence(p.previewRecovery, p.recordingSHA) != nil || p.previewRecovery.SidecarFile != fmt.Sprintf("preview-recovery-%06d-%s.json", d.slot.Ordinal, d.arm) || r.recordFiles[p.previewRecovery.SidecarFile] != p.previewRecovery.SidecarSHA256 || d.verifyOwnedRecoveryRecord(p.recording) != nil {
		return nil, fmt.Errorf("preview recovery proof not sealed")
	}
	found := false
	for _, known := range p.permit.proofs {
		if known == p {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("preview recovery proof not registered")
	}
	out := *p.previewRecovery
	return &out, nil
}
