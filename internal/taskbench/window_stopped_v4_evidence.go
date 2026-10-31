package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

func readWindowStoppedV4Files(root string) (map[string][]byte, error) {
	return readWindowStoppedV4FilesWithPass(nil, root)
}

func readWindowStoppedV4FilesWithPass(p *historyReadPass, root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for name, sha := range windowStoppedV4ArtifactPins() {
		raw, err := historyStoppedFile(p, filepath.Join(root, filepath.FromSlash(name)), sha, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("stopped v4 original top artifact changed: %s", name)
		}
		files[name] = raw
	}
	return files, nil
}

// Reuses strict nullable-record and native body/descriptor/store readers. The
// P4 ledger is schema2; ancestors retain their original schema1 accounting.
// Independently add parsed native weighted input/output or JEV input costs to
// reconcile canonical replay. This is configured liability, not an invoice.
func readWindowStoppedV4Native(dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	return readWindowStoppedV4NativeWithPass(nil, dir, payload, ledger)
}

func readWindowStoppedV4NativeWithPass(p *historyReadPass, dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	reject := func() (windowAccountingAudit, error) {
		return windowAccountingAudit{}, fmt.Errorf("stopped v4 weighted native linkage rejected")
	}
	audit, err := readWindowStoppedV3NativeWithPass(p, dir, payload, ledger)
	if err != nil {
		return windowAccountingAudit{}, err
	}
	limits := func(p windowProviderLimits) windowCampaignProviderBounds {
		return windowCampaignProviderBounds{Model: p.Model, MaxRequestBytes: p.MaxRequestBytes, MaxInput: p.MaxInput, MaxOutput: p.MaxOutput, InputRate: p.InputRate, OutputRate: p.OutputRate, CacheHitInputRate: p.CacheHitInputRate}
	}
	if audit.Contract.Schema != "full-window-accounting.v2" || payload.EvidenceKind != "recorded_run" || audit.State.Halt != "" || !reflect.DeepEqual(windowCampaignLimits{Model: limits(audit.Contract.Model), JEV: limits(audit.Contract.JEV)}, windowV4ExpectedLimits()) {
		return reject()
	}
	var modelKnown, jevKnown int64
	models, jevs := 0, 0
	attempts := map[string]int{}
	for _, packet := range payload.Packets {
		identity := packet.Identity
		limit := audit.Contract.Model
		max := 5
		if identity.Provider == "jev" {
			limit = audit.Contract.JEV
			max = 1
			if identity.Arm != "jev_selective" {
				return reject()
			}
		} else if identity.Provider != "model" {
			return reject()
		}
		key := fmt.Sprintf("%s/%s/%d/%s/%d", identity.Provider, identity.CaseKey, identity.Pass, identity.Arm, identity.Turn)
		attempts[key]++
		if identity.Attempt != attempts[key] || identity.Attempt > max {
			return reject()
		}
		raw, err := historyStoppedFile(p, filepath.Join(dir, packet.Response.Body.File), packet.Response.Body.FullSHA256, windowMaxResponseBytes)
		if err != nil {
			return reject()
		}
		usage, err := parseWindowResponseUsage(identity.Provider, raw, limit)
		if err != nil {
			return reject()
		}
		if identity.Provider == "model" {
			if usage.CacheHit == nil || usage.CacheMiss == nil {
				return reject()
			}
			modelKnown += (*usage.CacheMiss*2000000+*usage.CacheHit*40000+999999)/1000000 + (*usage.Output*8000000+999999)/1000000
			models++
		} else {
			jevKnown += (*usage.Input*42000 + 999999) / 1000000
			jevs++
		}
	}
	if modelKnown != audit.State.ModelKnown || jevKnown != audit.State.JEVKnown || models != audit.State.ModelAttempts || jevs != audit.State.JEVAttempts {
		return reject()
	}
	return audit, nil
}

// Quality captures legitimately contain nullable business fields. Retain the
// record decoder's duplicate/depth/trailing/unknown-field rules, without the
// separate readiness non-null constraint. This never mints an owned proof.
func decodeWindowStoppedV4QualityData(raw []byte, target any) error {
	if err := validateWindowJSONShape(raw, false); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func readWindowStoppedV4Quality(dir, policy string, slot fullWindowSlot, row windowAcquisitionRow, record collectionRecording) (*windowQualityOwnedEnd, error) {
	return readWindowStoppedV4QualityWithPass(nil, dir, policy, slot, row, record)
}

func readWindowStoppedV4QualityWithPass(p *historyReadPass, dir, policy string, slot fullWindowSlot, row windowAcquisitionRow, record collectionRecording) (*windowQualityOwnedEnd, error) {
	reject := func() (*windowQualityOwnedEnd, error) {
		return nil, fmt.Errorf("stopped v4 quality sidecar linkage rejected")
	}
	q := row.QualityEnd
	if validateWindowQualityEndEvidence(policy, slot, row.Arm, row.ObservedTurns, q) != nil || row.Status != "failed" || row.StructuralPass || row.Success || record.Disposition != "halt" || q.CandidateFile != fmt.Sprintf("quality-%06d-%s-candidate.json", row.Ordinal, row.Arm) || q.CleanupFile != fmt.Sprintf("quality-%06d-%s-cleanup.json", row.Ordinal, row.Arm) {
		return reject()
	}
	candidateRaw, err := historyStoppedFile(p, filepath.Join(dir, q.CandidateFile), q.CandidateSHA256, 16<<20)
	if err != nil {
		return reject()
	}
	var candidate qualityPreviewSidecar
	if decodeWindowStoppedV4QualityData(candidateRaw, &candidate) != nil || verifyQualityPreviewSidecar(candidate) != nil || !reflect.DeepEqual(candidate.Evidence.Record, record) || candidate.SourceSHA256 != q.SourceSHA256 || candidate.RuleSHA256 != q.RuleSHA256 || len(candidate.Unrun) != q.UnrunTurns {
		return reject()
	}
	cleanupRaw, err := historyStoppedFile(p, filepath.Join(dir, q.CleanupFile), q.CleanupSHA256, 16<<20)
	if err != nil {
		return reject()
	}
	var cleanup struct {
		Post    qualityEffectCapture `json:"post_cleanup"`
		Deleted int64                `json:"deleted"`
		Absent  bool                 `json:"session_absent"`
	}
	if decodeWindowStoppedV4QualityData(cleanupRaw, &cleanup) != nil || cleanup.Deleted != 1 || !cleanup.Absent || !reflect.DeepEqual(cleanup.Post, candidate.Evidence.After) {
		return reject()
	}
	owned := &windowQualityOwnedEnd{Evidence: *q, Candidate: candidate, PostCleanup: cleanup.Post, Deleted: cleanup.Deleted, SessionAbsent: cleanup.Absent}
	if windowQualityEnvelopeSHA(owned) != q.EnvelopeSHA256 {
		return reject()
	}
	return owned, nil
}
