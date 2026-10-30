package taskbench

import (
	"fmt"
	"math"
	"path/filepath"
	"reflect"
)

// A historical replay, not an acquisition completion or a paid-ready token.
// Fixed top hashes retain the original failed flags; descendants are all read.
func readWindowStoppedV6Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	return readWindowStoppedV6BundleWithPass(nil, root, m, first, files)
}

func readWindowStoppedV6BundleWithPass(p *historyReadPass, root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func(reason string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v6 bundle rejected: %s", reason)
	}
	if !reflect.DeepEqual(first, expectedStoppedV5WindowBaseline()) || len(files) != len(windowStoppedV6ArtifactPins()) || len(m.Scope.Windows) != 131 || len(m.Scope.Slots) != 3660 {
		return reject("predecessor/declaration")
	}
	for name, sha := range windowStoppedV6ArtifactPins() {
		if conversationHash(files[name]) != sha {
			return reject("top artifact " + name)
		}
	}
	var original windowActualManifest
	if decodeWindowActualJSON(files["actual-manifest.json"], &original) != nil || !reflect.DeepEqual(original, m) {
		return reject("manifest")
	}
	if err := historyStoppedBinaryAttestation(p, files["actual-manifest.json.binary-attestation.json"], m, windowStoppedV6ArtifactPins()["actual-manifest.json"], filepath.Join(root, "full-window-actual-v6")); err != nil {
		return reject("signed original binary")
	}
	var actor struct {
		ExitCode        *int     `json:"exit_code"`
		EndedEpoch      *float64 `json:"ended_epoch"`
		RelaunchAllowed *bool    `json:"relaunch_allowed"`
	}
	if decodeWindowActualJSON(files["actual-owner-exit.json"], &actor) != nil || actor.ExitCode == nil || *actor.ExitCode != 1 || actor.RelaunchAllowed == nil || *actor.RelaunchAllowed || actor.EndedEpoch == nil || *actor.EndedEpoch <= 0 || math.IsNaN(*actor.EndedEpoch) || math.IsInf(*actor.EndedEpoch, 0) {
		return reject("owner exit")
	}
	var startup FullWindowActualStatus
	wantStart := FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, UnattemptedArms: 7320, GlobalUnrunTurns: 11772}
	if decodeWindowActualJSON(files["records/startup.json"], &startup) != nil || !reflect.DeepEqual(startup, wantStart) {
		return reject("startup")
	}
	var c windowCampaignClaim
	var s FullWindowActualReceipt
	var r windowAcquisitionReceipt
	var payload windowPayloadManifest
	wd := "records/window-000001"
	ledgerName := "campaign/ledger-000001/accounting.jsonl"
	for name, target := range map[string]any{"campaign/claim-000001.json": &c, "records/window-000001-summary.json": &s, wd + "/receipt.json": &r, wd + "/payload-manifest.json": &payload} {
		if decodeWindowActualJSON(files[name], target) != nil {
			return reject("decode " + name)
		}
	}
	claimSHA := conversationHash(files["campaign/claim-000001.json"])
	w := m.Scope.Windows[0]
	if c.Schema != "full-window-campaign-claim.v6" || c.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || c.ParentModel != 130000000 || c.ParentJEV != 1000000 || c.BaselineEvidenceSHA256 != windowStoppedV6PreviousSHA || c.Root != m.CampaignRoot || c.Index != 1 || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != first.PermanentModel+w.ModelCap || c.JEVPermanent != first.PermanentJEV+w.JEVCap || c.PreviousClaimSHA256 != "" || c.PreviousCheckpointSHA256 != "" {
		return reject("claim")
	}
	if c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 {
		return reject("claim bindings")
	}
	audit, err := readWindowStoppedV6NativeWithPass(p, filepath.Join(root, wd, "native-payloads"), payload, files[ledgerName])
	if err != nil {
		return reject("native: " + err.Error())
	}
	contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
	contract.ClaimSHA256 = claimSHA
	if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(payload.Contract, contract) || !reflect.DeepEqual(r.Accounting, audit.State) || r.AccountingSource != "live_owner_snapshot" || r.LedgerSHA256 != conversationHash(files[ledgerName]) || s.LedgerSHA256 != r.LedgerSHA256 || r.LedgerCompletionProof != "unavailable_from_journal" {
		return reject("accounting")
	}
	if audit.State.ModelKnown != 38873 || audit.State.JEVKnown != 1127 || audit.State.ModelAttempts != 47 || audit.State.JEVAttempts != 22 || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.Pending != nil || !audit.State.Closed || audit.State.Halt != "invalid_request" || audit.State.EnvelopeViolation {
		return reject("settled stop")
	}
	if r.Schema != "full-window-acquisition-receipt.v1" || r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || r.PlannedCurrentPairs != 28 || r.PlannedCurrentArms != 56 || r.PlannedCurrentTurns != 60 || r.AttemptedArms != 43 || r.FinishedArms != 42 || r.UnrunArms != 13 || r.ObservedTurns != 47 || r.GlobalUnrunTurns != 13 || r.QualityEndArms != 0 || r.QualityEndUnrunTurns != 0 || r.QualityNegativeArms != 0 {
		return reject("partitions")
	}
	if r.PayloadAuditOnlyCompletion != "unavailable_from_payload_capture" || !r.EnvironmentCleanup || r.Status != "stopped" || s.ReportStatus != "stopped" || r.Advanced || r.Cleanup || r.Observer || r.PayloadVerified || r.PayloadStatus != "audit_only" || r.NextClaimSHA256 != "" || r.FirstFailureStage != "arm_proof" || r.FirstFailureOrdinal != 22 || r.FirstFailureArm != "jev_selective" {
		return reject("original negative flags")
	}
	if s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || r.PayloadPackets != 69 || len(payload.Packets) != 69 || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != 1 || r.WindowID != w.ID || r.EvidenceKind != "recorded_run" || r.OperationLimitSeconds != 300 || len(r.Rows) != 56 {
		return reject("receipt linkage")
	}
	counters := r.ObserverCountersCumulative
	if counters.Accepted != 166 || counters.Exported != 165 || counters.Failed != 1 || counters.Dropped != 0 || counters.QueueDepth != 0 {
		return reject("original observer failure")
	}
	names := map[string]bool{"receipt.json": false, "payload-manifest.json": false, "native-payloads": true}
	traces := map[string]bool{}
	rows := map[string]windowAcquisitionRow{}
	turns := 0
	for j, row := range r.Rows {
		ordinal := w.FirstOrdinal + j/2
		slot := m.Scope.Slots[ordinal-1]
		if row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns || row.QualityEnd != nil || row.ExcludeCompleteTaskComparison || row.Success {
			return reject("row identity")
		}
		if j >= 43 {
			want := windowAcquisitionRow{Ordinal: ordinal, Pass: slot.Pass, CaseKey: slot.CaseKey, Arm: slot.ArmOrder[j%2], DefinedTurns: slot.UserTurns, Status: "not_started"}
			if !reflect.DeepEqual(row, want) {
				return reject("unstarted row")
			}
			continue
		}
		finished := j < 42
		if !row.Attempted || row.Finished != finished || row.Cleanup != finished || row.Observer != finished || row.Status != "needs_review" || !row.StructuralPass || row.Disposition != "continue_recorded" || row.ObservedTurns != row.DefinedTurns {
			return reject("attempted state")
		}
		if row.RecordingFile != fmt.Sprintf("arm-%06d-%s.json", ordinal, row.Arm) || len(row.TraceID) != 32 || traces[row.TraceID] {
			return reject("record/trace identity")
		}
		traces[row.TraceID] = true
		raw, err := historyStoppedFile(p, filepath.Join(root, wd, row.RecordingFile), row.RecordingSHA256, 8<<20)
		if err != nil {
			return reject("record bytes")
		}
		record, err := decodeWindowStoppedV2Recording(raw)
		if err != nil || record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Result.Success != row.Success || record.Result.StructuralPass != row.StructuralPass || record.Disposition != row.Disposition || len(record.Result.Case.Scenario.Turns) != row.DefinedTurns || verifyCollectionRecording(record) != nil {
			return reject("record replay")
		}
		for _, obs := range record.Result.Observations {
			if obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" || obs.Turn.Response.Telemetry == nil || obs.Turn.Response.Telemetry.LangfuseTraceID != row.TraceID {
				return reject("observation trace")
			}
		}
		names[row.RecordingFile] = false
		rows[row.CaseKey+"/"+row.Arm] = row
		turns += row.ObservedTurns
	}
	if turns != 47 || len(rows) != 43 {
		return reject("record count")
	}
	models, jevs := 0, 0
	for _, packet := range payload.Packets {
		row, ok := rows[packet.Identity.CaseKey+"/"+packet.Identity.Arm]
		if !ok || packet.Identity.Pass != row.Pass || packet.Identity.Turn < 1 || packet.Identity.Turn > row.ObservedTurns {
			return reject("native row")
		}
		if packet.Identity.Provider == "model" {
			models++
		} else {
			jevs++
		}
	}
	if models != 47 || jevs != 22 {
		return reject("attempt count")
	}
	want := FullWindowActualStatus{EvidenceKind: "recorded_run", Closed: true, NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: 43, FinishedArms: 42, FailedAttemptedArms: 1, UnattemptedArms: 7277, ObservedTurns: 47, GlobalUnrunTurns: 11725, ModelAttempts: 47, JEVAttempts: 22}
	if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != 1 || s.FirstUnrunOrdinal != 22 || s.FirstUnrunArm != "model" || s.FutureUnrunFromOrdinal != 29 {
		return reject("summary")
	}
	var terminal FullWindowActualStatus
	want.LastReceiptSHA256 = conversationHash(files["records/window-000001-summary.json"])
	if decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil || !reflect.DeepEqual(terminal, want) {
		return reject("terminal")
	}
	for dir, inventory := range map[string]map[string]bool{wd: names, "campaign/ledger-000001": {"accounting.jsonl": false}, "campaign": {".lock": false, "claim-000001.json": false, "ledger-000001": true}, "records": {"startup.json": false, "terminal.json": false, "window-000001-summary.json": false, "window-000001": true}} {
		if err := historyStoppedDir(p, filepath.Join(root, dir), inventory); err != nil {
			return reject("inventory: " + err.Error())
		}
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	base.ClaimSHA256["full1220-v6-20261029/window-000001"] = claimSHA
	base.TotalModel, base.TotalJEV = 130000000, 1000000
	base.PermanentModel += w.ModelCap
	base.PermanentJEV += w.JEVCap
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	base.ActualModelCalls += models
	base.ActualJEVCalls += jevs
	if !reflect.DeepEqual(base, expectedStoppedV6WindowBaseline()) {
		return reject("derived baseline")
	}
	return base, nil
}
func validateWindowStoppedV6Namespace(m windowActualManifest, e windowStoppedV6Evidence, p windowStoppedV5Baseline) error {
	if e.Schema != "full-window-stopped-baseline.v6" || e.PreviousBaseline.Path != p.evidencePath || e.PreviousBaseline.SHA256 != p.evidenceSHA256 || e.PreviousBaseline.SHA256 != windowStoppedV6PreviousSHA || e.BudgetApproval.SHA256 != windowBudget130ApprovalSHA || !reflect.DeepEqual(p.base, expectedStoppedV5WindowBaseline()) || p.historicalDir != p.previous.historicalDir {
		return fmt.Errorf("stopped v6 predecessor namespace rejected")
	}
	roots := (windowStoppedV6Baseline{previous: p, originalRoot: e.OriginalRoot}).windowStoppedPredecessorRootsV7()
	for i, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return fmt.Errorf("noncanonical predecessor root")
		}
		for j := 0; j < i; j++ {
			if !windowPayloadDisjoint(root, roots[j]) {
				return fmt.Errorf("crossed predecessor roots")
			}
		}
	}
	if m.Schema != "full-window-actual-ready.v6" || m.Scope.Schema != "full-window-scope.v6" || m.ArchiveCommit != "4ccec396f628a15050376eb1b34946e39d79a34a" || m.ArchiveTree != "64b6a1b0caa8497e74dc9c56fd670fe1aa4d01ed" || m.SourceRoot != filepath.Join(e.OriginalRoot, "regression/source") || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || m.Scope.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || m.Scope.BaselineEvidenceSHA256 != e.PreviousBaseline.SHA256 || m.CampaignRoot != filepath.Join(e.OriginalRoot, "campaign") || m.SessionRoot != filepath.Join(e.OriginalRoot, "records") || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.PreviousBaseline || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || m.Observer.ExportTimeoutMS != 2000 {
		return fmt.Errorf("stopped v6 original namespace changed")
	}
	caps := make([]fullWindowCaps, len(m.Scope.Windows))
	for i, w := range m.Scope.Windows {
		caps[i] = fullWindowCaps{ModelCap: w.ModelCap, JEVCap: w.JEVCap}
	}
	return validateWindowV6Declaration(m.Scope.Width, caps, e.PreviousBaseline.SHA256, e.BudgetApproval.SHA256)
}
