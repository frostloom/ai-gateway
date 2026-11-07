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

// This new binding retains the existing unsafe-preview quality-end assessment
// and selects the separately versioned non-executing failed-preview recorder.
func windowOwnedPreviewFailurePolicySHA() string {
	return qualityJSONSHA([]string{"full-customer-owned-preview-failure.v13", windowOwnedQualityPolicySHA(), collectionPreviewFailurePolicySHA(), "quiesced-owned-capture;complete-orders-only-on-refund-failure;durable-sidecar-before-cleanup;opaque-owner-seal;all-original-turns;versioned-quality-end-with-verified-complete-admissions"})
}
func windowOwnedRecordingPolicyAllowed(scope fullWindowScope) bool {
	if scope.Schema == "full-window-scope.v16" {
		return windowRecoveryPolicyAllowed(scope)
	}
	if scope.Schema == "full-window-scope.v13" || (scope.Schema == "full-window-scope.v14" || scope.Schema == "full-window-scope.v15") {
		return scope.Bindings.PolicySHA256 == windowOwnedPreviewFailurePolicySHA() && scope.CollectionPolicySHA256 == collectionPreviewFailurePolicySHA()
	}
	return scope.Bindings.PolicySHA256 == collectionPolicySHA() || scope.Bindings.PolicySHA256 == windowOwnedQualityPolicySHA()
}
func (d *windowOwnedDriver) windowPreviewFailureMode() bool {
	return d != nil && d.permit != nil && d.permit.campaign != nil && (d.permit.campaign.prepared.scope.Schema == "full-window-scope.v13" || (d.permit.campaign.prepared.scope.Schema == "full-window-scope.v14" || d.permit.campaign.prepared.scope.Schema == "full-window-scope.v15")) && windowOwnedRecordingPolicyAllowed(d.permit.campaign.prepared.scope)
}

type windowPreviewFailureEvidence struct {
	PolicySHA256    string `json:"policy_sha256"`
	RecordingSHA256 string `json:"recording_sha256"`
	SidecarFile     string `json:"sidecar_file"`
	SidecarSHA256   string `json:"sidecar_sha256"`
}
type windowPreviewFailureSidecar struct {
	Schema string                            `json:"schema"`
	Record collectionPreviewFailureRecording `json:"record"`
}

func (d *windowOwnedDriver) collectionPreviewFailureEvidence(ctx context.Context, turn int) (collectionPreviewFailureEvidence, error) {
	if d == nil {
		return collectionPreviewFailureEvidence{}, fmt.Errorf("missing failed preview owner")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || !d.windowPreviewFailureMode() || d.phase != nil || turn != d.nextTurn || captureWindowQualityOwner(d) != nil {
		return collectionPreviewFailureEvidence{}, fmt.Errorf("stale failed preview owner")
	}
	for _, e := range d.previewFailures {
		if e.Turn == turn {
			return cloneWindowOwned(e)
		}
	}
	return collectionPreviewFailureEvidence{}, fmt.Errorf("missing owned failed preview capture")
}

// Called with driver.mu while no Handler/native phase is active. Captures do
// not grant a permit, and the exact policy is rerun before any next turn.
func (d *windowOwnedDriver) captureWindowPreviewFailure(ctx context.Context, before qualityEffectCapture, previous json.RawMessage, obs ConversationObservation) error {
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
	if err != nil || pending != nil {
		return fmt.Errorf("failed preview session not quiescent")
	}
	in := collectionPreviewFailureEvidence{Turn: d.nextTurn, BeforeSession: previous, AfterSession: session, Before: before, After: after}
	if orders {
		rows := []store.RechargeOrder{}
		if err = d.environment.db.WithContext(ctx).Order("id").Limit(100001).Find(&rows).Error; err != nil || len(rows) > 100000 {
			return fmt.Errorf("failed preview full orders unavailable")
		}
		in.Orders = &collectionPreviewFailureOrders{Columns: append([]string(nil), after.Tables["recharge_orders"].Columns...), Rows: rows}
	}
	if err = verifyCollectionPreviewFailureEvidence(d.caseDefinition, d.fixture, before.State, obs, in); err != nil {
		return err
	}
	if ctx.Err() != nil || captureWindowQualityOwner(d) != nil {
		return fmt.Errorf("failed preview owner changed after capture")
	}
	detached, err := cloneWindowOwned(in)
	if err != nil {
		return err
	}
	d.previewFailures = append(d.previewFailures, detached)
	return nil
}
func (d *windowOwnedDriver) previewFailureReplay(final ConversationState) *collectionPreviewFailureReplay {
	return &collectionPreviewFailureReplay{replayConversationDriver: &replayConversationDriver{episode: ConversationEpisode{Key: d.caseDefinition.Key(), Fixture: d.fixture, Initial: d.initial, Final: final, Steps: d.observations}}, evidence: d.previewFailures}
}
func (d *windowOwnedDriver) verifyOwnedCollectionRecord(record collectionRecording) error {
	if !d.windowPreviewFailureMode() {
		return verifyCollectionRecording(record)
	}
	return verifyCollectionPreviewFailureRecording(collectionPreviewFailureRecording{collectionRecording: record, PreviewFailures: d.previewFailures})
}
func (d *windowOwnedDriver) retainWindowPreviewFailure(record collectionRecording) error {
	if !d.windowPreviewFailureMode() || d.qualityCandidate != nil {
		return nil
	}
	r := d.permit.acquisition
	if r == nil || r.validateWindowQualityFiles() != nil || d.phase != nil {
		return fmt.Errorf("failed preview acquisition unavailable")
	}
	sidecar := windowPreviewFailureSidecar{Schema: "full-window-preview-failure-sidecar.v13", Record: collectionPreviewFailureRecording{collectionRecording: record, PreviewFailures: d.previewFailures}}
	if verifyCollectionPreviewFailureRecording(sidecar.Record) != nil {
		return fmt.Errorf("failed preview replay rejected")
	}
	raw, err := json.Marshal(sidecar)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("preview-failure-%06d-%s.json", d.slot.Ordinal, d.arm)
	if err = r.persistWindowAcquisition(name, raw); err != nil {
		return err
	}
	r.recordFiles[name] = conversationHash(raw)
	recordRaw, err := json.Marshal(record)
	if err != nil || r.validateWindowQualityFiles() != nil {
		return fmt.Errorf("failed preview persistence changed")
	}
	d.previewFailureRetained = &windowPreviewFailureEvidence{PolicySHA256: windowOwnedPreviewFailurePolicySHA(), RecordingSHA256: conversationHash(recordRaw), SidecarFile: name, SidecarSHA256: conversationHash(raw)}
	return nil
}

// Pure source replay checks the sidecar and matching base record. It never
// substitutes for the opaque live proof or the authenticated file namespace.
func verifyWindowPreviewFailureSidecar(raw []byte, record collectionRecording, evidence *windowPreviewFailureEvidence) error {
	if validateWindowPreviewFailureEvidence(evidence, windowV8DeclarationSHA(record)) != nil || conversationHash(raw) != evidence.SidecarSHA256 {
		return fmt.Errorf("failed preview sidecar binding differs")
	}
	var sidecar windowPreviewFailureSidecar
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if validateWindowJSONShape(raw, false) != nil || decoder.Decode(&sidecar) != nil || sidecar.Schema != "full-window-preview-failure-sidecar.v13" || !qualityJSONEqual(sidecar.Record.collectionRecording, record) || verifyCollectionPreviewFailureRecording(sidecar.Record) != nil {
		return fmt.Errorf("failed preview sidecar replay differs")
	}
	return nil
}
func validateWindowPreviewFailureEvidence(e *windowPreviewFailureEvidence, recordSHA string) error {
	if e == nil || e.PolicySHA256 != windowOwnedPreviewFailurePolicySHA() || !comparisonDigest(recordSHA) || e.RecordingSHA256 != recordSHA || !comparisonDigest(e.SidecarSHA256) || filepath.Base(e.SidecarFile) != e.SidecarFile || !strings.HasPrefix(e.SidecarFile, "preview-failure-") || strings.ContainsAny(e.SidecarFile, "/\\") || filepath.Ext(e.SidecarFile) != ".json" {
		return fmt.Errorf("failed preview evidence differs")
	}
	return nil
}
func windowPreviewFailureProofEvidence(p *windowOwnedArmProof) (*windowPreviewFailureEvidence, error) {
	if p == nil || p.driver == nil {
		return nil, fmt.Errorf("failed preview proof absent")
	}
	d := p.driver
	if !d.windowPreviewFailureMode() || p.quality != nil {
		if p.previewFailure != nil || d.previewFailureRetained != nil {
			return nil, fmt.Errorf("cross-policy failed preview evidence")
		}
		return nil, nil
	}
	r := p.permit.acquisition
	if p.permit != d.permit || p.permit.stopped || r == nil || r.validateWindowQualityFiles() != nil || !reflect.DeepEqual(p.previewFailure, d.previewFailureRetained) || validateWindowPreviewFailureEvidence(p.previewFailure, p.recordingSHA) != nil || r.recordFiles[p.previewFailure.SidecarFile] != p.previewFailure.SidecarSHA256 || d.verifyOwnedCollectionRecord(p.recording) != nil {
		return nil, fmt.Errorf("failed preview proof not sealed")
	}
	found := false
	for _, known := range p.permit.proofs {
		if known == p {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("failed preview proof not registered")
	}
	out := *p.previewFailure
	return &out, nil
}

func qualityPreviewCandidateV13(in qualityPreviewInput) (qualityPreviewSidecar, error) {
	if in.PreviewFailures == nil {
		in.PreviewFailures = []collectionPreviewFailureEvidence{}
	}
	recording := collectionPreviewFailureRecording{collectionRecording: in.Record, PreviewFailures: in.PreviewFailures}
	if verifyCollectionPreviewFailureRecording(recording) != nil {
		return qualityPreviewSidecar{}, fmt.Errorf("preview candidate rejected: v13 assessment")
	}
	out, err := qualityPreviewCandidateVerified(in)
	if err != nil {
		return out, err
	}
	// Reclone captures with their established recording decoder; the old core's
	// UseNumber decoder is for private pending maps and old capture semantics.
	out.Evidence.PreviewFailures, err = cloneWindowOwned(in.PreviewFailures)
	if err != nil {
		return qualityPreviewSidecar{}, err
	}
	out.PolicyID = "full-customer-owned-preview-candidate.v13"
	out.PolicySHA256 = qualityJSONSHA([]string{out.PolicyID, qualityPreviewPolicySHA(), collectionPreviewFailurePolicySHA()})
	return out, nil
}
func verifyWindowQualityCandidate(sidecar qualityPreviewSidecar) error {
	if sidecar.PolicyID == qualityPreviewPolicyID {
		return verifyQualityPreviewSidecar(sidecar)
	}
	expected, err := qualityPreviewCandidateV13(sidecar.Evidence)
	if err != nil {
		return err
	}
	if !qualityJSONEqual(expected, sidecar) {
		return fmt.Errorf("v13 preview sidecar binding differs")
	}
	return nil
}
func (d *windowOwnedDriver) windowQualityCandidate(in qualityPreviewInput) (qualityPreviewSidecar, error) {
	if d.windowPreviewFailureMode() {
		in.PreviewFailures = d.previewFailures
		return qualityPreviewCandidateV13(in)
	}
	return qualityPreviewCandidate(in)
}
func (d *windowOwnedDriver) windowQualityOwnedPolicySHA() string {
	if d.windowPreviewFailureMode() {
		return windowOwnedPreviewFailurePolicySHA()
	}
	return windowOwnedQualityPolicySHA()
}

// V13 returns the same wire representation used by its retained capture,
// without non-wire clock metadata or aliases into the owner's evidence.
func (d *windowOwnedDriver) observationForWindowRecording(live, stored ConversationObservation) (ConversationObservation, error) {
	if d.windowPreviewFailureMode() || d.windowPreviewRecoveryMode() {
		return cloneWindowOwned(stored)
	}
	return live, nil
}
