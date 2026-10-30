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
func readWindowStoppedV3Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	return readWindowStoppedV3BundleWithPass(nil, root, m, first, files)
}

func readWindowStoppedV3BundleWithPass(p *historyReadPass, root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func() (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v3 bundle linkage rejected")
	}
	if !reflect.DeepEqual(first, expectedStoppedV2WindowBaseline()) || len(m.Scope.Windows) != 131 || len(m.Scope.Slots) != 3660 || len(files) != 57 {
		return reject()
	}
	for name, sha := range windowStoppedV3ArtifactPins() {
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
	if decodeWindowActualJSON(files["actual-owner-exit.json"], &actor) != nil || actor.ExitCode == nil || *actor.ExitCode != 1 || actor.RelaunchAllowed == nil || *actor.RelaunchAllowed || actor.EndedEpoch == nil || *actor.EndedEpoch <= 0 || math.IsNaN(*actor.EndedEpoch) || math.IsInf(*actor.EndedEpoch, 0) {
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
	for i := 1; i <= 9; i++ {
		claimName := fmt.Sprintf("campaign/claim-%06d.json", i)
		summaryName := fmt.Sprintf("records/window-%06d-summary.json", i)
		wd := fmt.Sprintf("records/window-%06d", i)
		ledgerName := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		var c windowCampaignClaim
		var s FullWindowActualReceipt
		var r windowAcquisitionReceipt
		var payload windowPayloadManifest
		for name, target := range map[string]any{claimName: &c, summaryName: &s, wd + "/receipt.json": &r, wd + "/payload-manifest.json": &payload} {
			if decodeWindowActualJSON(files[name], target) != nil {
				return reject()
			}
		}
		if c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 {
			return reject()
		}
		claimSHA := conversationHash(files[claimName])
		w := m.Scope.Windows[i-1]
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if c.Schema != "full-window-campaign-claim.v3" || c.BaselineEvidenceSHA256 != m.Scope.BaselineEvidenceSHA256 || c.Root != m.CampaignRoot || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return reject()
		}
		if i == 1 {
			if c.PreviousClaimSHA256 != "" || c.PreviousCheckpointSHA256 != "" {
				return reject()
			}
		} else if c.PreviousClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i-1)]) || c.PreviousCheckpointSHA256 != conversationHash(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i-1)]) {
			return reject()
		}
		base.ClaimSHA256[fmt.Sprintf("full1220-v3-20261028/window-%06d", i)] = claimSHA
		audit, err := readWindowStoppedV3NativeWithPass(p, filepath.Join(root, wd, "native-payloads"), payload, files[ledgerName])
		if err != nil {
			return reject()
		}
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(payload.Contract, contract) || !reflect.DeepEqual(r.Accounting, audit.State) || !audit.State.Closed || audit.State.EnvelopeViolation || r.AccountingSource != "live_owner_snapshot" || r.LedgerSHA256 != conversationHash(files[ledgerName]) || s.LedgerSHA256 != r.LedgerSHA256 || r.LedgerCompletionProof != "unavailable_from_journal" {
			return reject()
		}
		if r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || r.PlannedCurrentPairs != 28 || r.PlannedCurrentArms != 56 || r.QualityEndArms != 0 || r.QualityEndUnrunTurns != 0 || r.PayloadAuditOnlyCompletion != "unavailable_from_payload_capture" || !r.EnvironmentCleanup {
			return reject()
		}
		defined := 0
		for _, slot := range m.Scope.Slots[w.FirstOrdinal-1 : w.LastOrdinal] {
			defined += 2 * slot.UserTurns
		}
		if r.PlannedCurrentTurns != defined || r.GlobalUnrunTurns != defined-r.ObservedTurns {
			return reject()
		}
		if s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || r.PayloadPackets != len(payload.Packets) || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != i || r.WindowID != w.ID || r.EvidenceKind != "recorded_run" || r.OperationLimitSeconds != 300 || len(r.Rows) != 56 {
			return reject()
		}
		names := map[string]bool{"receipt.json": false, "payload-manifest.json": false, "native-payloads": true}
		attempted, finished, turns, models, jevs := 0, 0, 0, 0, 0
		rows := map[string]windowAcquisitionRow{}
		for j, row := range r.Rows {
			ordinal := w.FirstOrdinal + j/2
			slot := m.Scope.Slots[ordinal-1]
			if row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns {
				return reject()
			}
			if !row.Attempted {
				if row.Finished || row.ObservedTurns != 0 || row.RecordingFile != "" || row.RecordingSHA256 != "" || row.TraceID != "" {
					return reject()
				}
				continue
			}
			if row.RecordingFile != fmt.Sprintf("arm-%06d-%s.json", ordinal, row.Arm) || row.TraceID == "" || traces[row.TraceID] {
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
				if obs.Turn.Response.Telemetry == nil || obs.Turn.Response.Telemetry.LangfuseTraceID != row.TraceID {
					return reject()
				}
			}
			names[row.RecordingFile] = false
			rows[row.CaseKey+"/"+row.Arm] = row
			attempted++
			if row.Finished {
				finished++
			}
			turns += row.ObservedTurns
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
		if models != audit.State.ModelAttempts || jevs != audit.State.JEVAttempts || attempted != r.AttemptedArms || finished != r.FinishedArms || turns != r.ObservedTurns || r.UnrunArms != 56-attempted {
			return reject()
		}
		if err = historyStoppedDir(p, filepath.Join(root, wd), names); err != nil {
			return fullWindowBaseline{}, err
		}
		if err = historyStoppedDir(p, filepath.Join(root, "campaign", fmt.Sprintf("ledger-%06d", i)), map[string]bool{"accounting.jsonl": false}); err != nil {
			return fullWindowBaseline{}, err
		}
		want := FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: i + 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: prior.AttemptedArms + attempted, FinishedArms: prior.FinishedArms + finished, FailedAttemptedArms: prior.FailedAttemptedArms + attempted - finished, ObservedTurns: prior.ObservedTurns + turns, ModelAttempts: prior.ModelAttempts + models, JEVAttempts: prior.JEVAttempts + jevs}
		want.UnattemptedArms = 7320 - want.AttemptedArms
		want.GlobalUnrunTurns = 11772 - want.ObservedTurns
		if i > 1 {
			want.LastReceiptSHA256 = conversationHash(files[fmt.Sprintf("records/window-%06d-summary.json", i-1)])
		}
		if i == 9 {
			want.Closed = true
			want.NextWindowIndex = 9
		}
		firstOrdinal, firstArm := closedFirstUnrun(m.Scope, i, r.Rows)
		if s.FirstUnrunOrdinal != firstOrdinal || s.FirstUnrunArm != firstArm || s.FutureUnrunFromOrdinal != w.LastOrdinal+1 {
			return reject()
		}
		if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != i {
			return reject()
		}
		if i < 9 {
			if r.Status != "collected_window" || s.ReportStatus != "collected_window" || !r.Advanced || !r.Cleanup || !r.Observer || !r.PayloadVerified || r.PayloadStatus != "verified_ledger_linkage" || audit.State.Pending != nil || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.Halt != "" || r.NextClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) {
				return reject()
			}
			campaignNames[fmt.Sprintf("checkpoint-%06d.json", i)] = false
		} else if r.Status != "stopped" || s.ReportStatus != "stopped" || r.Advanced || r.Cleanup || r.Observer || !r.EnvironmentCleanup || r.PayloadVerified || r.PayloadStatus != "audit_only" || r.NextClaimSHA256 != "" || audit.State.Halt != "budget_exhausted" || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.Pending != nil || r.FirstFailureStage != "arm_proof" || r.FirstFailureOrdinal != 241 || r.FirstFailureArm != "jev_selective" || audit.State.ModelPermanent-audit.State.ModelKnown != 71370 || audit.Contract.Model.cost(audit.Contract.Model.MaxInput, audit.Contract.Model.MaxOutput) != 74752 || audit.State.ModelPermanent-audit.State.ModelKnown >= 74752 {
			return reject()
		}
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
	want.LastReceiptSHA256 = conversationHash(files["records/window-000009-summary.json"])
	if !reflect.DeepEqual(terminal, want) || terminal.AttemptedArms != 482 || terminal.FinishedArms != 481 || terminal.ObservedTurns != 597 || terminal.ModelAttempts != 982 || terminal.JEVAttempts != 241 || terminal.UnattemptedArms != 6838 || terminal.GlobalUnrunTurns != 11175 || totalRecords != 482 || totalPackets != 1223 || lastAudit.State.Pending != nil || lastAudit.State.Halt != "budget_exhausted" {
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
	if !reflect.DeepEqual(base, expectedStoppedV3WindowBaseline()) {
		return reject()
	}
	return base, nil
}

func validateWindowStoppedV3Namespace(m windowActualManifest, e windowStoppedV3Evidence, p windowStoppedV2Baseline) error {
	if e.Schema != "full-window-stopped-baseline.v3" || e.PreviousBaseline.Path != p.evidencePath || e.PreviousBaseline.SHA256 != p.evidenceSHA256 || e.PreviousBaseline.SHA256 != windowStoppedV3PreviousSHA || e.BudgetApproval.SHA256 != windowBudget105ApprovalSHA || !reflect.DeepEqual(p.base, expectedStoppedV2WindowBaseline()) || !reflect.DeepEqual(p.first.base, expectedStoppedWindowBaseline()) || p.first.liabilityMicroCNY != 3832 || p.historicalDir != p.first.historicalDir {
		return fmt.Errorf("stopped v3 concrete predecessor namespace rejected")
	}
	roots := [3]string{p.first.originalRoot, p.originalRoot, e.OriginalRoot}
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
	if m.Schema != "full-window-actual-ready.v3" || m.Scope.Schema != "full-window-scope.v3" || m.ArchiveCommit != "f48bb0ac8ecc690605c4f2662b7ad3250b6888a7" || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || m.Scope.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || m.Scope.BaselineEvidenceSHA256 != e.PreviousBaseline.SHA256 || m.CampaignRoot != filepath.Join(e.OriginalRoot, "campaign") || m.SessionRoot != filepath.Join(e.OriginalRoot, "records") || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.PreviousBaseline || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) {
		return fmt.Errorf("stopped v3 original namespace changed")
	}
	return nil
}
