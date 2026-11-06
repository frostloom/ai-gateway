package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func readWindowStoppedV13Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	fail := func(s string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v13 bundle: %s", s)
	}
	if !reflect.DeepEqual(first, expectedStoppedV12WindowBaseline()) || validateWindowV13ExecutionShape(m.Scope) != nil || len(files) != 3828 {
		return fail("fixed156/full inventory")
	}
	for _, name := range []string{"campaign/claim-000022.json", "campaign/checkpoint-000022.json"} {
		if _, ok := files[name]; ok {
			return fail("later claim/checkpoint")
		}
	}
	var cleanup map[string]any
	var owner map[string]any
	if json.Unmarshal(files["actual-owner-group-cleanup.json"], &cleanup) != nil || cleanup["group_empty"] != true || cleanup["leader_reaped"] != true || cleanup["confirmed"] != true || cleanup["owned_pgid"] != float64(69290) || json.Unmarshal(files["actual-owner-exit.json"], &owner) != nil || owner["exit_code"] != float64(1) || owner["relaunch_allowed"] != false {
		return fail("stopped/reaped owner")
	}
	var startup FullWindowActualStatus
	if decodeWindowActualJSON(files["records/startup.json"], &startup) != nil || validateWindowStoppedV11Observation(m, startup.Observation, false) != nil {
		return fail("startup DATA")
	}
	wantStartup := FullWindowActualStatus{Observation: startup.Observation, EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3275, PlannedArms: 6550, DefinedTurns: 10504, UnattemptedArms: 6550, GlobalUnrunTurns: 10504}
	if !reflect.DeepEqual(startup, wantStartup) {
		return fail("startup incomplete boundary")
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	priorClaim, priorCheckpoint, priorSummary := "", "", ""
	used := map[string]bool{}
	models, jevs, attempted, finished, turns := 0, 0, 0, 0, 0
	var cumulative *windowV8RuntimeSummary
	var last FullWindowActualStatus
	for i := 1; i <= 21; i++ {
		cn := fmt.Sprintf("campaign/claim-%06d.json", i)
		wd := fmt.Sprintf("records/window-%06d", i)
		ln := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		sn := fmt.Sprintf("records/window-%06d-summary.json", i)
		var c windowCampaignClaim
		var r windowAcquisitionReceipt
		var s FullWindowActualReceipt
		var payload windowPayloadManifest
		for _, x := range []struct {
			raw []byte
			v   any
		}{{files[cn], &c}, {files[wd+"/receipt.json"], &r}, {files[sn], &s}, {files[wd+"/payload-manifest.json"], &payload}} {
			if decodeWindowActualJSON(x.raw, x.v) != nil {
				return fail("original JSON")
			}
		}
		if validateWindowStoppedV11Observation(m, r.Observation, false) != nil || validateWindowStoppedV11Observation(m, s.Observation, false) != nil {
			return fail("original local observation DATA")
		}
		w := m.Scope.Windows[i-1]
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if c.Schema != "full-window-campaign-claim.v13" || c.Root != root+"/campaign" || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.Window != w || c.BaselineEvidenceSHA256 != windowV13BaselineSHA || c.BudgetApprovalSHA256 != windowStoppedV13AuthorizationSHA || c.ParentModel != 195000000 || c.ParentJEV != 3000000 || c.PreviousClaimSHA256 != priorClaim || c.PreviousCheckpointSHA256 != priorCheckpoint || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return fail("claim chain")
		}
		claimSHA := conversationHash(files[cn])
		base.ClaimSHA256[fmt.Sprintf("full3660-v13-r4-20261105/window-%06d", i)] = claimSHA
		halt := ""
		audit, e := readWindowStoppedV12Native(wd+"/native-payloads", payload, files[ln], files, used, false)
		if e != nil {
			return fail(e.Error())
		}
		a := audit.State
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(r.Accounting, a) || r.LedgerSHA256 != audit.JournalSHA256 || r.AccountingSource != "live_owner_snapshot" || r.LedgerCompletionProof != "unavailable_from_journal" {
			return fail("native/accounting linkage")
		}
		evidence := []windowCampaignArmEvidence{}
		rowMap := map[string]windowAcquisitionRow{}
		at, fin, obs := 0, 0, 0
		for j, row := range r.Rows {
			ordinal := w.FirstOrdinal + j/2
			slot := m.Scope.Slots[ordinal-1]
			if row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns || row.QualityEnd != nil || row.FailureEvidence != nil || row.ExcludeCompleteTaskComparison {
				return fail("row identity")
			}
			if !row.Attempted {
				return fail("unattempted row")
			}
			at++
			obs += row.ObservedTurns
			if row.Finished {
				fin++
			}
			if row.RecordingFile != fmt.Sprintf("arm-%06d-%s.json", ordinal, row.Arm) || conversationHash(files[wd+"/"+row.RecordingFile]) != row.RecordingSHA256 || len(row.TraceID) != 32 {
				return fail("record linkage")
			}
			record, e := decodeWindowStoppedV2Recording(files[wd+"/"+row.RecordingFile])
			if e != nil || windowV8DeclarationSHA(record.Result) != record.ResultSHA256 || record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Disposition != row.Disposition || validateWindowStoppedV13Recording(record, row, files, wd, used) != nil {
				return fail("original result DATA")
			}
			if !row.Finished || !row.Cleanup || !row.Observer || row.ObservedTurns != row.DefinedTurns {
				return fail("completed row DATA")
			}
			evidence = append(evidence, windowCampaignArmEvidence{Ordinal: ordinal, Pass: row.Pass, ObservedTurns: row.ObservedTurns, Key: row.CaseKey, Arm: row.Arm, RecordingSHA256: row.RecordingSHA256, Cleanup: row.Cleanup, Observer: row.Observer, PreviewFailure: row.PreviewFailure})
			rowMap[row.CaseKey+"/"+row.Arm] = row
		}
		for _, packet := range payload.Packets {
			row, ok := rowMap[packet.Identity.CaseKey+"/"+packet.Identity.Arm]
			if !ok || packet.Identity.Pass != row.Pass || packet.Identity.Turn < 1 || packet.Identity.Turn > row.ObservedTurns {
				return fail("native row coverage")
			}
		}
		runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: a.ModelAttempts, LogicalJEVCalls: a.JEVAttempts, NativeModelIntents: a.ModelAttempts, NativeJEVIntents: a.JEVAttempts, NativeSettlements: a.ModelAttempts + a.JEVAttempts, PendingHeld: windowV8Amounts{a.ModelHeld, a.JEVHeld}, Halt: halt, Known: windowV8Amounts{a.ModelKnown, a.JEVKnown}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{base.PermanentModel - first.PermanentModel, base.PermanentJEV - first.PermanentJEV}, TotalPermanent: windowV8Amounts{base.PermanentModel, base.PermanentJEV}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
		{
			var cp windowCampaignCheckpoint
			if decodeWindowActualJSON(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i)], &cp) != nil || cp.Schema != "full-window-campaign-owned-checkpoint.v13" || cp.Index != i || cp.ClaimSHA256 != claimSHA || cp.ScopeSHA256 != m.ScopeSHA256 || cp.ExecutionSHA256 != m.ExecutionSHA256 || cp.LedgerSHA256 != audit.JournalSHA256 || !reflect.DeepEqual(cp.Evidence, evidence) || !reflect.DeepEqual(cp.V8, &runtime) {
				return fail("original checkpoint DATA")
			}
			priorCheckpoint = conversationHash(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i)])
		}
		if i < 21 {
			nw := m.Scope.Windows[i]
			runtime.BasePermanent.Model += nw.ModelCap
			runtime.BasePermanent.JEV += nw.JEVCap
			runtime.TotalPermanent.Model += nw.ModelCap
			runtime.TotalPermanent.JEV += nw.JEVCap
			if r.Status != "collected_window" || !r.Cleanup || !r.Observer || !r.Advanced || !r.PayloadVerified || r.NextClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) || at != len(r.Rows) || fin != at {
				return fail("original completed window")
			}
		} else if r.Status != "stopped" || r.Cleanup || !r.Observer || r.Advanced || r.PayloadVerified || r.PayloadStatus != "audit_only" || r.FirstFailureStage != "campaign_advance" || r.FirstFailureOrdinal != 0 || r.FirstFailureArm != "" || r.NextClaimSHA256 != "" || at != 22 || fin != 22 || obs != 24 || r.DurationMS != 300012.679 {
			return fail("stopped21 original negative boundary")
		}

		if r.Schema != "full-window-acquisition-receipt.v13" || r.EvidenceKind != "recorded_run" || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != i || r.WindowID != w.ID || r.PlannedCampaignPairs != 3275 || r.PlannedCampaignArms != 6550 || r.PlannedCampaignTurns != 10504 || len(r.Rows) != 2*(w.LastOrdinal-w.FirstOrdinal+1) || r.AttemptedArms != at || r.FinishedArms != fin || r.ObservedTurns != obs || r.OperationLimitSeconds != 300 || !r.EnvironmentCleanup || !reflect.DeepEqual(r.Completion, m.Scope.Completion) || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || r.PayloadPackets != len(payload.Packets) || !reflect.DeepEqual(r.V8, &runtime) {
			return fail("receipt DATA linkage")
		}
		models += a.ModelAttempts
		jevs += a.JEVAttempts
		attempted += at
		finished += fin
		turns += obs
		cumulative = mergeWindowV8Runtime(cumulative, &runtime)
		want := FullWindowActualStatus{Completion: m.Scope.Completion, V8: cumulative, EvidenceKind: "recorded_run", Observation: s.Observation, BoundariesExcluded: 0, Closed: i == 21, NextWindowIndex: min(i+1, 21), LastReceiptSHA256: priorSummary, PlannedPairs: 3275, PlannedArms: 6550, DefinedTurns: 10504, AttemptedArms: attempted, FinishedArms: finished, FailedAttemptedArms: attempted - finished, UnattemptedArms: 6550 - attempted, ObservedTurns: turns, GlobalUnrunTurns: 10504 - turns, ModelAttempts: models, JEVAttempts: jevs}
		firstUnrun := w.LastOrdinal + 1
		firstArm := m.Scope.Slots[firstUnrun-1].ArmOrder[0]
		future := firstUnrun

		if s.FirstUnrunOrdinal != firstUnrun || s.FirstUnrunArm != firstArm || s.FutureUnrunFromOrdinal != future {
			return fail("original unrun positions")
		}
		if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != i || s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || s.LedgerSHA256 != r.LedgerSHA256 || s.ReportStatus != r.Status {
			return fail("summary chain")
		}
		last = want
		priorClaim = claimSHA
		priorSummary = conversationHash(files[sn])
	}
	for name := range files {
		if (strings.Contains(name, "/native-payloads/") || strings.Contains(name, "/preview-failure-")) && !used[name] {
			return fail("unreferenced native original")
		}
	}
	var terminal FullWindowActualStatus
	last.LastReceiptSHA256 = priorSummary
	if decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil {
		return fail("terminal JSON")
	}
	last.Observation = terminal.Observation
	if validateWindowStoppedV11Observation(m, terminal.Observation, true) != nil || !reflect.DeepEqual(terminal, last) {
		return fail("terminal incomplete")
	}
	base.ActualModelCalls += models
	base.ActualJEVCalls += jevs
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	return base, nil
}

// Replay the exact V13 collection policy with its original captured sidecar.
// Finished arms are DATA only; they cannot override stopped window flags.
func validateWindowStoppedV13Recording(r collectionRecording, row windowAcquisitionRow, files map[string][]byte, dir string, used map[string]bool) error {
	if r.PolicyID != collectionPreviewFailurePolicyID || r.PolicySHA256 != collectionPreviewFailurePolicySHA() || r.Result.Case.Key() != row.CaseKey || len(r.Result.Observations) != row.ObservedTurns || r.Result.Status != row.Status || r.Disposition != row.Disposition || r.Result.StructuralPass != row.StructuralPass || r.Result.Success != row.Success {
		return fmt.Errorf("original v13 result/disposition")
	}
	if row.PreviewFailure == nil {
		return verifyCollectionPreviewFailureRecording(collectionPreviewFailureRecording{collectionRecording: r})
	}
	if row.PreviewFailure.SidecarFile != fmt.Sprintf("preview-failure-%06d-%s.json", row.Ordinal, row.Arm) {
		return fmt.Errorf("original sidecar identity")
	}
	name := dir + "/" + row.PreviewFailure.SidecarFile
	used[name] = true
	return verifyWindowPreviewFailureSidecar(files[name], r, row.PreviewFailure)
}
