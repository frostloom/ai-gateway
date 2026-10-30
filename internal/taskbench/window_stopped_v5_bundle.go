package taskbench

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
)

// All top-level bytes have fixed source pins. Descendant records and native
// bodies must still be read and checked, rather than accepting their manifests
// as proof that those bytes exist. No old accounting state is mutated.
func readWindowStoppedV5Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	return readWindowStoppedV5BundleWithPass(nil, root, m, first, files)
}

func readWindowStoppedV5BundleWithPass(p *historyReadPass, root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func() (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v5 bundle linkage rejected")
	}
	if !reflect.DeepEqual(first, expectedStoppedV4WindowBaseline()) || len(m.Scope.Windows) != 131 || len(m.Scope.Slots) != 3660 || len(files) != 24 {
		return reject()
	}
	for name, sha := range windowStoppedV5ArtifactPins() {
		if conversationHash(files[name]) != sha {
			return reject()
		}
	}
	var original windowActualManifest
	if decodeWindowActualJSON(files["actual-manifest.json"], &original) != nil || !reflect.DeepEqual(original, m) {
		return reject()
	}
	var actor struct {
		ExitCode        *int     `json:"exit_code"`
		EndedEpoch      *float64 `json:"ended_epoch"`
		RelaunchAllowed *bool    `json:"relaunch_allowed"`
	}
	if decodeWindowActualJSON(files["actual-owner-exit.json"], &actor) != nil || actor.ExitCode == nil || *actor.ExitCode != 0 || actor.RelaunchAllowed == nil || *actor.RelaunchAllowed || actor.EndedEpoch == nil || *actor.EndedEpoch <= 0 || math.IsNaN(*actor.EndedEpoch) || math.IsInf(*actor.EndedEpoch, 0) {
		return reject()
	}
	var startup FullWindowActualStatus
	wantStartup := FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, UnattemptedArms: 7320, GlobalUnrunTurns: 11772}
	if decodeWindowActualJSON(files["records/startup.json"], &startup) != nil || !reflect.DeepEqual(startup, wantStartup) {
		return reject()
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	var prior FullWindowActualReceipt
	var lastAudit windowAccountingAudit
	totalPackets, totalRecords := 0, 0
	traces := map[string]bool{}
	campaignNames := map[string]bool{".lock": false}
	recordNames := map[string]bool{"startup.json": false, "terminal.json": false}
	for i := 1; i <= 4; i++ {
		claimName := fmt.Sprintf("campaign/claim-%06d.json", i)
		summaryName := fmt.Sprintf("records/window-%06d-summary.json", i)
		wd := fmt.Sprintf("records/window-%06d", i)
		ledgerName := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		var c windowCampaignClaim
		var s FullWindowActualReceipt
		var r windowAcquisitionReceipt
		var payload windowPayloadManifest
		if decodeWindowActualJSON(files[claimName], &c) != nil {
			return reject()
		}
		if i <= 3 {
			for name, target := range map[string]any{summaryName: &s, wd + "/receipt.json": &r, wd + "/payload-manifest.json": &payload} {
				if decodeWindowActualJSON(files[name], target) != nil {
					return reject()
				}
			}
		}
		if c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 {
			return reject()
		}
		claimSHA := conversationHash(files[claimName])
		w := m.Scope.Windows[i-1]
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if c.Schema != "full-window-campaign-claim.v5" || c.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || c.ParentModel != 105000000 || c.ParentJEV != 1000000 || c.BaselineEvidenceSHA256 != m.Scope.BaselineEvidenceSHA256 || c.Root != m.CampaignRoot || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return reject()
		}
		if i == 1 {
			if c.PreviousClaimSHA256 != "" || c.PreviousCheckpointSHA256 != "" {
				return reject()
			}
		} else if c.PreviousClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i-1)]) || c.PreviousCheckpointSHA256 != conversationHash(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i-1)]) {
			return reject()
		}
		base.ClaimSHA256[fmt.Sprintf("full1220-v5-20261029/window-%06d", i)] = claimSHA
		// Claim4 is permanently reserved by receipt3. Exact inventories below
		// reject ledger4, checkpoint4, report4, claim5 and extra sidecars.
		if i == 4 {
			campaignNames["claim-000004.json"] = false
			continue
		}
		audit, err := readWindowStoppedV5NativeWithPass(p, filepath.Join(root, wd, "native-payloads"), payload, files[ledgerName])
		if err != nil {
			return reject()
		}
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(payload.Contract, contract) || !reflect.DeepEqual(r.Accounting, audit.State) || !audit.State.Closed || audit.State.EnvelopeViolation || r.AccountingSource != "live_owner_snapshot" || r.LedgerSHA256 != conversationHash(files[ledgerName]) || s.LedgerSHA256 != r.LedgerSHA256 || r.LedgerCompletionProof != "unavailable_from_journal" {
			return reject()
		}
		if r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || r.PlannedCurrentPairs != 28 || r.PlannedCurrentArms != 56 || r.AttemptedArms != 56 || r.FinishedArms != 56 || r.UnrunArms != 0 || r.QualityEndUnrunTurns != 0 || r.PayloadAuditOnlyCompletion != "unavailable_from_payload_capture" || !r.EnvironmentCleanup {
			return reject()
		}
		defined := 0
		for _, slot := range m.Scope.Slots[w.FirstOrdinal-1 : w.LastOrdinal] {
			defined += 2 * slot.UserTurns
		}
		if r.PlannedCurrentTurns != defined || r.GlobalUnrunTurns != defined-r.ObservedTurns-r.QualityEndUnrunTurns {
			return reject()
		}
		if s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || r.PayloadPackets != len(payload.Packets) || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != i || r.WindowID != w.ID || r.EvidenceKind != "recorded_run" || r.OperationLimitSeconds != 300 || len(r.Rows) != 56 {
			return reject()
		}
		names := map[string]bool{"receipt.json": false, "payload-manifest.json": false, "native-payloads": true}
		attempted, finished, turns, models, jevs, negative, qualityArms, qualityUnrun := 0, 0, 0, 0, 0, 0, 0, 0
		qualityOrdinals := map[int]bool{}
		rows := map[string]windowAcquisitionRow{}
		for j, row := range r.Rows {
			ordinal := w.FirstOrdinal + j/2
			slot := m.Scope.Slots[ordinal-1]
			if row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns {
				return reject()
			}
			if !row.Attempted || !row.Finished || !row.Cleanup || !row.Observer || row.Success {
				return reject()
			}
			if row.Status == "failed" {
				if row.StructuralPass != (i == 3 && row.Arm == "jev_selective" && (row.Ordinal == 71 || row.Ordinal == 79)) {
					return reject()
				}
				negative++
			} else if row.Status != "needs_review" || !row.StructuralPass {
				return reject()
			}
			if row.RecordingFile != fmt.Sprintf("arm-%06d-%s.json", ordinal, row.Arm) || len(row.TraceID) != 32 || traces[row.TraceID] {
				return reject()
			}
			traces[row.TraceID] = true
			raw, err := historyStoppedFile(p, filepath.Join(root, wd, row.RecordingFile), row.RecordingSHA256, 8<<20)
			if err != nil {
				return reject()
			}
			record, decodeErr := decodeWindowStoppedV2Recording(raw)
			if decodeErr != nil || record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Result.Success != row.Success || record.Result.StructuralPass != row.StructuralPass || record.Disposition != row.Disposition || record.PolicyID != collectionPolicyID || record.PolicySHA256 != collectionPolicySHA() || len(record.Result.Case.Scenario.Turns) != row.DefinedTurns {
				return reject()
			}
			resultRaw, err := json.Marshal(record.Result)
			if err != nil || conversationHash(resultRaw) != record.ResultSHA256 {
				return reject()
			}
			for _, obs := range record.Result.Observations {
				if obs.Turn.Status != 200 || obs.Turn.Error != "" || obs.Turn.Response.HTTPError != "" || obs.Turn.Response.Telemetry == nil || obs.Turn.Response.Telemetry.LangfuseTraceID != row.TraceID {
					return reject()
				}
			}
			if row.QualityEnd != nil {
				if _, err := readWindowStoppedV4QualityWithPass(p, filepath.Join(root, wd), m.Scope.Bindings.PolicySHA256, slot, row, record); err != nil {
					return reject()
				}
				names[row.QualityEnd.CandidateFile] = false
				names[row.QualityEnd.CleanupFile] = false
				qualityArms++
				qualityUnrun += row.QualityEnd.UnrunTurns
				qualityOrdinals[row.Ordinal] = true
			} else if record.Disposition != "continue_recorded" || row.ObservedTurns != row.DefinedTurns {
				return reject()
			}
			names[row.RecordingFile] = false
			rows[row.CaseKey+"/"+row.Arm] = row
			attempted++
			if row.Finished {
				finished++
			}
			turns += row.ObservedTurns
		}
		for _, row := range r.Rows {
			if row.ExcludeCompleteTaskComparison != qualityOrdinals[row.Ordinal] {
				return reject()
			}
		}
		if qualityArms != 0 || qualityArms != r.QualityEndArms || qualityUnrun != 0 || qualityUnrun != r.QualityEndUnrunTurns || negative != [...]int{0, 3, 0, 2}[i] {
			return reject()
		}
		for _, p := range payload.Packets {
			row, ok := rows[p.Identity.CaseKey+"/"+p.Identity.Arm]
			if !ok || p.Identity.Pass != row.Pass || p.Identity.Turn < 1 || p.Identity.Turn > row.ObservedTurns {
				return reject()
			}
			if p.Identity.Provider == "model" {
				models++
			} else {
				jevs++
			}
		}
		if negative != r.QualityNegativeArms || models != audit.State.ModelAttempts || jevs != audit.State.JEVAttempts || attempted != r.AttemptedArms || finished != r.FinishedArms || turns != r.ObservedTurns || r.UnrunArms != 56-attempted {
			return reject()
		}
		if err = historyStoppedDir(p, filepath.Join(root, wd), names); err != nil {
			return fullWindowBaseline{}, err
		}
		if err = historyStoppedDir(p, filepath.Join(root, "campaign", fmt.Sprintf("ledger-%06d", i)), map[string]bool{"accounting.jsonl": false}); err != nil {
			return fullWindowBaseline{}, err
		}
		want := FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: i + 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: prior.AttemptedArms + attempted, FinishedArms: prior.FinishedArms + finished, FailedAttemptedArms: prior.FailedAttemptedArms + attempted - finished, ObservedTurns: prior.ObservedTurns + turns, QualityEndUnrunTurns: prior.QualityEndUnrunTurns + qualityUnrun, ModelAttempts: prior.ModelAttempts + models, JEVAttempts: prior.JEVAttempts + jevs}
		want.UnattemptedArms = 7320 - want.AttemptedArms
		want.GlobalUnrunTurns = 11772 - want.ObservedTurns - want.QualityEndUnrunTurns
		if i > 1 {
			want.LastReceiptSHA256 = conversationHash(files[fmt.Sprintf("records/window-%06d-summary.json", i-1)])
		}
		firstOrdinal, firstArm := closedFirstUnrun(m.Scope, i, r.Rows)
		if s.FirstUnrunOrdinal != firstOrdinal || s.FirstUnrunArm != firstArm || s.FutureUnrunFromOrdinal != w.LastOrdinal+1 {
			return reject()
		}
		if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != i {
			return reject()
		}
		if r.Status != "collected_window" || s.ReportStatus != "collected_window" || !r.Advanced || !r.Cleanup || !r.Observer || !r.PayloadVerified || r.PayloadStatus != "verified_ledger_linkage" || audit.State.Pending != nil || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.Halt != "" || r.NextClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) || r.FirstFailureStage != "" || r.FirstFailureOrdinal != 0 || r.FirstFailureArm != "" {
			return reject()
		}
		counters := r.ObserverCountersCumulative
		if counters.Failed != 0 || counters.Dropped != 0 || counters.QueueDepth != 0 || counters.Accepted == 0 || counters.Accepted != counters.Exported {
			return reject()
		}
		var checkpoint windowCampaignCheckpoint
		if decodeWindowActualJSON(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i)], &checkpoint) != nil {
			return reject()
		}
		expectedCheckpoint := windowCampaignCheckpoint{Schema: "full-window-campaign-owned-checkpoint.v1", Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: m.Scope.DefinitionSHA256, ScheduleSHA256: m.Scope.ScheduleSHA256, PolicySHA256: m.Scope.Bindings.PolicySHA256, ClaimSHA256: claimSHA, LedgerSHA256: r.LedgerSHA256, Evidence: []windowCampaignArmEvidence{}}
		for _, row := range r.Rows {
			expectedCheckpoint.Evidence = append(expectedCheckpoint.Evidence, windowCampaignArmEvidence{QualityEnd: row.QualityEnd, Ordinal: row.Ordinal, Pass: row.Pass, ObservedTurns: row.ObservedTurns, Key: row.CaseKey, Arm: row.Arm, RecordingSHA256: row.RecordingSHA256, Cleanup: true, Observer: true})
		}
		if !reflect.DeepEqual(checkpoint, expectedCheckpoint) {
			return reject()
		}
		campaignNames[fmt.Sprintf("checkpoint-%06d.json", i)] = false
		base.ActualModelCalls += models
		base.ActualJEVCalls += jevs
		prior = s
		lastAudit = audit
		totalRecords += attempted
		totalPackets += len(payload.Packets)
		campaignNames[fmt.Sprintf("claim-%06d.json", i)] = false
		campaignNames[fmt.Sprintf("ledger-%06d", i)] = true
		recordNames[fmt.Sprintf("window-%06d", i)] = true
		recordNames[fmt.Sprintf("window-%06d-summary.json", i)] = false
	}
	var terminal FullWindowActualStatus
	if decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil {
		return reject()
	}
	want := prior.FullWindowActualStatus
	want.LastReceiptSHA256 = conversationHash(files["records/window-000003-summary.json"])
	want.Closed = true
	if !reflect.DeepEqual(terminal, want) || terminal.AttemptedArms != 168 || terminal.FinishedArms != 168 || terminal.FailedAttemptedArms != 0 || terminal.ObservedTurns != 200 || terminal.ModelAttempts != 307 || terminal.JEVAttempts != 84 || terminal.UnattemptedArms != 7152 || terminal.GlobalUnrunTurns != 11572 || terminal.NextWindowIndex != 4 || terminal.AcquisitionDone || totalRecords != 168 || totalPackets != 391 || lastAudit.State.Pending != nil || lastAudit.State.Halt != "" {
		return reject()
	}

	if err := historyStoppedDir(p, filepath.Join(root, "campaign"), campaignNames); err != nil {
		return fullWindowBaseline{}, err
	}
	if err := historyStoppedDir(p, filepath.Join(root, "records"), recordNames); err != nil {
		return fullWindowBaseline{}, err
	}
	base.TotalModel = 105000000
	base.TotalJEV = 1000000
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	if !reflect.DeepEqual(base, expectedStoppedV5WindowBaseline()) {
		return reject()
	}
	return base, nil
}

func validateWindowStoppedV5Namespace(m windowActualManifest, e windowStoppedV5Evidence, p windowStoppedV4Baseline) error {
	if e.Schema != "full-window-stopped-baseline.v5" || e.PreviousBaseline.Path != p.evidencePath || e.PreviousBaseline.SHA256 != p.evidenceSHA256 || e.PreviousBaseline.SHA256 != windowStoppedV5PreviousSHA || e.BudgetApproval.SHA256 != windowBudget105ApprovalSHA || !reflect.DeepEqual(p.base, expectedStoppedV4WindowBaseline()) || !reflect.DeepEqual(p.previous.base, expectedStoppedV3WindowBaseline()) || !reflect.DeepEqual(p.previous.previous.base, expectedStoppedV2WindowBaseline()) || !reflect.DeepEqual(p.previous.previous.first.base, expectedStoppedWindowBaseline()) || p.previous.previous.first.liabilityMicroCNY != 3832 || p.historicalDir != p.previous.historicalDir || p.historicalDir != p.previous.previous.first.historicalDir {
		return fmt.Errorf("stopped v5 concrete32 namespace rejected")
	}
	roots := [5]string{p.previous.previous.first.originalRoot, p.previous.previous.originalRoot, p.previous.originalRoot, p.originalRoot, e.OriginalRoot}
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
	if m.Schema != "full-window-actual-ready.v5" || m.Scope.Schema != "full-window-scope.v5" || m.ArchiveCommit != "740db25818eccebe8363b282a109a14ca610a0be" || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || m.Scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || m.Scope.BaselineEvidenceSHA256 != e.PreviousBaseline.SHA256 || m.CampaignRoot != filepath.Join(e.OriginalRoot, "campaign") || m.SessionRoot != filepath.Join(e.OriginalRoot, "records") || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.PreviousBaseline || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() {
		return fmt.Errorf("stopped v5 original namespace changed")
	}
	return nil
}
