package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"os"
	"path/filepath"
	"reflect"
)

type windowQualityEndEvidence struct {
	PolicySHA256    string `json:"policy_sha256"`
	SourceSHA256    string `json:"source_sha256"`
	RuleSHA256      string `json:"rule_sha256"`
	CandidateFile   string `json:"candidate_file"`
	CandidateSHA256 string `json:"candidate_sha256"`
	CleanupFile     string `json:"cleanup_file"`
	CleanupSHA256   string `json:"cleanup_sha256"`
	EnvelopeSHA256  string `json:"envelope_sha256"`
	ObservedTurns   int    `json:"observed_turns"`
	UnrunTurns      int    `json:"unrun_turns"`
}
type windowQualityOwnedEnd struct {
	Evidence      windowQualityEndEvidence `json:"evidence"`
	Candidate     qualityPreviewSidecar    `json:"candidate"`
	PostCleanup   qualityEffectCapture     `json:"post_cleanup"`
	Deleted       int64                    `json:"deleted"`
	SessionAbsent bool                     `json:"session_absent"`
}

func windowOwnedQualityPolicySHA() string {
	raw, _ := json.Marshal([]string{"full-customer-owned-quality-end.v2", collectionPolicySHA(), qualityPreviewPolicySHA(), "live-owned-same-permit-run-campaign;full-case-v1-halt;preceding-sql-session;quiesce-before-confirmation;actual-final-rebind;durable-new-only-precleanup;exact-owned-session-del-one-exists-zero;postcleanup-whole-sql-audit-control-unchanged;ended-root-observer-flush;immutable-envelope-registry-seal;exact-defined-source-rule-unrun-suffix"})
	return conversationHash(raw)
}
func windowQualityProofEvidence(proof *windowOwnedArmProof) (*windowQualityEndEvidence, error) {
	reject := func() (*windowQualityEndEvidence, error) { return nil, fmt.Errorf("quality proof not sealed") }
	if proof == nil {
		return reject()
	}
	if proof.quality == nil && proof.qualitySHA == "" {
		if proof.driver != nil && proof.driver.qualityRetainedSHA != "" {
			return reject()
		}
		return nil, nil
	}
	if proof.quality == nil || proof.driver == nil || proof.permit == nil || proof.driver.permit != proof.permit || proof.driver.qualityCandidate == nil || proof.driver.qualityRetainedSHA == "" {
		return reject()
	}
	raw, err := json.Marshal(proof.quality)
	if err != nil || conversationHash(raw) != proof.qualitySHA || proof.qualitySHA != proof.driver.qualityRetainedSHA {
		return reject()
	}
	q := proof.quality
	if q.Deleted != 1 || !q.SessionAbsent || q.Evidence.PolicySHA256 != windowOwnedQualityPolicySHA() || q.Evidence.EnvelopeSHA256 != windowQualityEnvelopeSHA(q) || q.Evidence.SourceSHA256 != q.Candidate.SourceSHA256 || q.Evidence.RuleSHA256 != q.Candidate.RuleSHA256 || q.Evidence.ObservedTurns != len(proof.recording.Result.Observations) || q.Evidence.UnrunTurns != len(q.Candidate.Unrun) || q.Evidence.ObservedTurns+q.Evidence.UnrunTurns != len(proof.recording.Result.Case.Scenario.Turns) || !reflect.DeepEqual(q.Candidate.Evidence.Record, proof.recording) || verifyQualityPreviewSidecar(q.Candidate) != nil {
		return reject()
	}
	r := proof.permit.acquisition
	if r == nil || r != proof.driver.qualityAcquisition || r.permit != proof.permit || r.validateWindowQualityFiles() != nil {
		return reject()
	}
	found := false
	for _, known := range proof.permit.proofs {
		if known == proof {
			found = true
		}
	}
	if !found || r.recordFiles[q.Evidence.CandidateFile] != q.Evidence.CandidateSHA256 || r.recordFiles[q.Evidence.CleanupFile] != q.Evidence.CleanupSHA256 {
		return reject()
	}
	out := q.Evidence
	return &out, nil
}
func windowQualityEnvelopeSHA(q *windowQualityOwnedEnd) string {
	copy := *q
	copy.Evidence.EnvelopeSHA256 = ""
	raw, _ := json.Marshal(copy)
	return conversationHash(raw)
}
func (d *windowOwnedDriver) windowQualityMode() bool {
	return d != nil && d.permit != nil && d.permit.campaign != nil && d.permit.campaign.prepared.scope.Bindings.PolicySHA256 == windowOwnedQualityPolicySHA()
}
func (r *windowAcquisitionRun) validateWindowQualityFiles() error {
	if r == nil || r.permit == nil || r.permit.acquisition != r || r.auth.campaign != r.permit.campaign || r.ledger != r.permit.ledger || r.environment != r.permit.environment || r.rootInfo == nil || r.recordFiles == nil || !reflect.DeepEqual(r.scope, r.permit.campaign.prepared.scope) || r.permit.windowIndex < 1 || r.permit.windowIndex > len(r.scope.Windows) || r.window != r.scope.Windows[r.permit.windowIndex-1] {
		return fmt.Errorf("quality acquisition identity unavailable")
	}
	info, err := os.Lstat(r.auth.privateReportRoot)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !os.SameFile(info, r.rootInfo) {
		return fmt.Errorf("quality report root changed")
	}
	entries, err := os.ReadDir(r.auth.privateReportRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		// The payload subdirectory is independently sealed by the collector.
		if entry.Name() == "native-payloads" && entry.IsDir() {
			continue
		}
		digest, ok := r.recordFiles[entry.Name()]
		if !ok {
			return fmt.Errorf("unknown quality report artifact")
		}
		fi, e := entry.Info()
		if e != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 {
			return fmt.Errorf("quality artifact identity changed")
		}
		raw, e := os.ReadFile(filepath.Join(r.auth.privateReportRoot, entry.Name()))
		if e != nil || conversationHash(raw) != digest {
			return fmt.Errorf("quality artifact changed")
		}
	}
	for name, digest := range r.recordFiles {
		raw, e := os.ReadFile(filepath.Join(r.auth.privateReportRoot, name))
		if e != nil || conversationHash(raw) != digest {
			return fmt.Errorf("quality artifact missing")
		}
	}
	return nil
}
func (d *windowOwnedDriver) latchWindowQualityCandidate(ctx context.Context, before qualityEffectCapture, previous json.RawMessage) error {
	r := d.permit.acquisition
	if r == nil || r.validateWindowQualityFiles() != nil {
		return fmt.Errorf("quality acquisition unavailable")
	}
	replay := &replayConversationDriver{episode: ConversationEpisode{Key: d.caseDefinition.Key(), Fixture: d.fixture, Initial: d.initial, Final: d.observations[len(d.observations)-1].State, Steps: d.observations}}
	record := recordCollectionConversation(ctx, d.caseDefinition, replay)
	after, err := captureWindowQualityEffects(ctx, d)
	if err != nil {
		return err
	}
	_, pending, err := captureWindowQualitySession(ctx, d, false)
	if err != nil {
		return err
	}
	plans := []store.Plan{}
	if err = d.environment.db.WithContext(ctx).Order("id").Find(&plans).Error; err != nil {
		return err
	}
	candidate, err := qualityPreviewCandidate(qualityPreviewInput{Record: record, Turn: d.nextTurn, BeforeSession: previous, Pending: pending, Before: before, After: after, Plans: plans})
	if err != nil {
		return err
	}
	d.qualityCandidate = &candidate
	d.qualityAcquisition = r
	return nil
}
func (d *windowOwnedDriver) finishWindowQualityOwned(ctx context.Context, record collectionRecording) (*windowOwnedArmProof, error) {
	fail := func() (*windowOwnedArmProof, error) {
		if d.windowFailureDiagnostics() != nil {
			return nil, d.cleanupFailureLocked(ctx)
		}
		_ = d.cleanupWindowOwnedSession(ctx)
		return nil, d.stopWindowOwned()
	}
	r := d.permit.acquisition
	if !d.windowQualityMode() || r == nil || r != d.qualityAcquisition || r.validateWindowQualityFiles() != nil || d.phase != nil || d.qualityRoot == nil || !d.qualityRoot.SpanContext().IsValid() || d.qualityRoot.IsRecording() {
		return fail()
	}
	input := d.qualityCandidate.Evidence
	input.Record = record
	candidate, err := qualityPreviewCandidate(input)
	if err != nil {
		return fail()
	}
	finalCapture, err := captureWindowQualityEffects(ctx, d)
	if err != nil || !reflect.DeepEqual(finalCapture.Tables, candidate.Evidence.After.Tables) || !canonicalEqual(finalCapture.Audits, candidate.Evidence.After.Audits) || !canonicalEqual(finalCapture.Sessions, candidate.Evidence.After.Sessions) || !sameWindowQualityCapturedState(d.final, finalCapture.State) {
		return fail()
	}
	// Re-read the exact live pending immediately before durability and deletion.
	_, pending, err := captureWindowQualitySession(ctx, d, false)
	if err != nil || !qualityJSONEqual(pending, candidate.Evidence.Pending) {
		return fail()
	}
	prefix := fmt.Sprintf("quality-%06d-%s", d.slot.Ordinal, d.arm)
	candidateFile := prefix + "-candidate.json"
	raw, err := json.Marshal(candidate)
	if err != nil || r.persistWindowAcquisition(candidateFile, raw) != nil {
		return fail()
	}
	candidateSHA := conversationHash(raw)
	r.recordFiles[candidateFile] = candidateSHA
	if r.validateWindowQualityFiles() != nil {
		return fail()
	}
	n, err := d.environment.rdb.Del(ctx, "agent:session:"+d.session).Result()
	if err != nil || n != 1 {
		d.recordFailureCleanup("failed", true)
		return fail()
	}
	exists, err := d.environment.rdb.Exists(ctx, "agent:session:"+d.session).Result()
	if err != nil || exists != 0 {
		d.recordFailureCleanup("failed", true)
		return fail()
	}
	d.recordFailureCleanup("verified", true)
	post, err := captureWindowQualityEffects(ctx, d)
	if err != nil || !reflect.DeepEqual(post.Tables, finalCapture.Tables) || !canonicalEqual(post.Audits, finalCapture.Audits) || !canonicalEqual(post.Sessions, finalCapture.Sessions) || !sameWindowQualityCapturedState(post.State, finalCapture.State) {
		return fail()
	}
	if d.observer.ForceFlush(ctx) != nil {
		return fail()
	}
	stats := d.observer.Stats()
	if d.observer.EvaluationIdentity() != d.permit.observerIdentity || stats.Failed != 0 || stats.Dropped != 0 || stats.QueueDepth != 0 || stats.Accepted != stats.Exported || stats.Turns-d.observerStart.Turns != uint64(d.nextTurn) || stats.SampledTurns-d.observerStart.SampledTurns != uint64(d.nextTurn) {
		return fail()
	}
	cleanup := struct {
		Post    qualityEffectCapture `json:"post_cleanup"`
		Deleted int64                `json:"deleted"`
		Absent  bool                 `json:"session_absent"`
	}{post, n, true}
	raw, err = json.Marshal(cleanup)
	cleanupFile := prefix + "-cleanup.json"
	if err != nil || r.persistWindowAcquisition(cleanupFile, raw) != nil {
		return fail()
	}
	cleanupSHA := conversationHash(raw)
	r.recordFiles[cleanupFile] = cleanupSHA
	if r.validateWindowQualityFiles() != nil {
		return fail()
	}
	q := &windowQualityOwnedEnd{Candidate: candidate, PostCleanup: post, Deleted: n, SessionAbsent: true, Evidence: windowQualityEndEvidence{PolicySHA256: windowOwnedQualityPolicySHA(), SourceSHA256: candidate.SourceSHA256, RuleSHA256: candidate.RuleSHA256, CandidateFile: candidateFile, CandidateSHA256: candidateSHA, CleanupFile: cleanupFile, CleanupSHA256: cleanupSHA, ObservedTurns: d.nextTurn, UnrunTurns: len(candidate.Unrun)}}
	q.Evidence.EnvelopeSHA256 = windowQualityEnvelopeSHA(q)
	raw, err = json.Marshal(q)
	if err != nil {
		return fail()
	}
	qualitySHA := conversationHash(raw)
	raw, err = json.Marshal(record)
	if err != nil {
		return fail()
	}
	seal := conversationHash(raw)
	p := d.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.guardWindowOwnedLocked(d) != nil {
		return nil, fmt.Errorf("quality permit stopped before proof")
	}
	d.qualityCandidate = &candidate
	d.qualityRetainedSHA = qualitySHA
	proof := &windowOwnedArmProof{permit: p, driver: d, slot: d.slot, arm: d.arm, recording: record, recordingSHA: seal, quality: q, qualitySHA: qualitySHA}
	p.proofs = append(p.proofs, proof)
	p.proofSHA = append(p.proofSHA, seal)
	p.nextArm++
	p.active = nil
	return proof, nil
}
