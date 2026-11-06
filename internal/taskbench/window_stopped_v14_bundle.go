package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// This single consumed window is incomplete DATA. No checkpoint is synthesized.
func readWindowStoppedV14Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	fail := func(s string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped14 DATA: %s", s)
	}
	if root != windowStoppedV14Root || !reflect.DeepEqual(first, expectedStoppedV13WindowBaseline()) || validateWindowV14ExecutionShape(m.Scope) != nil {
		return fail("fixed177/root/scope")
	}
	for name := range files {
		if strings.HasPrefix(name, "campaign/claim-") && name != "campaign/claim-000001.json" || strings.HasPrefix(name, "campaign/checkpoint-") {
			return fail("later claim/checkpoint")
		}
	}
	var cleanup, owner map[string]any
	if json.Unmarshal(files["actual-owner-group-cleanup.json"], &cleanup) != nil || cleanup["group_empty"] != true || cleanup["leader_reaped"] != true || cleanup["confirmed"] != true || cleanup["owned_pgid"] != float64(694) || json.Unmarshal(files["actual-owner-exit.json"], &owner) != nil || owner["exit_code"] != float64(1) || owner["relaunch_allowed"] != false {
		return fail("stopped owner")
	}
	var startup FullWindowActualStatus
	if decodeWindowActualJSON(files["records/startup.json"], &startup) != nil || validateWindowStoppedV11Observation(m, startup.Observation, false) != nil {
		return fail("startup")
	}
	if !reflect.DeepEqual(startup, FullWindowActualStatus{Observation: startup.Observation, EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3275, PlannedArms: 6550, DefinedTurns: 10504, UnattemptedArms: 6550, GlobalUnrunTurns: 10504}) {
		return fail("startup boundary")
	}
	var c windowCampaignClaim
	var r windowAcquisitionReceipt
	var s FullWindowActualReceipt
	var payload windowPayloadManifest
	wd := "records/window-000001"
	for _, x := range []struct {
		raw []byte
		v   any
	}{{files["campaign/claim-000001.json"], &c}, {files[wd+"/receipt.json"], &r}, {files["records/window-000001-summary.json"], &s}, {files[wd+"/payload-manifest.json"], &payload}} {
		if decodeWindowActualJSON(x.raw, x.v) != nil {
			return fail("JSON")
		}
	}
	w := m.Scope.Windows[0]
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	base.PermanentModel += w.ModelCap
	base.PermanentJEV += w.JEVCap
	if c.Schema != "full-window-campaign-claim.v14" || c.Root != root+"/campaign" || c.Index != 1 || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.Window != w || c.BaselineEvidenceSHA256 != windowStoppedV13InventorySHA || c.BudgetApprovalSHA256 != windowStoppedV14AuthorizationSHA || c.ParentModel != 200000000 || c.ParentJEV != 3000000 || c.PreviousClaimSHA256 != "" || c.PreviousCheckpointSHA256 != "" || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV || conversationHash(files["campaign/claim-000001.json"]) != windowStoppedV14ClaimSHA {
		return fail("claim")
	}
	base.ClaimSHA256[windowStoppedV14ClaimKey] = windowStoppedV14ClaimSHA
	used := map[string]bool{}
	audit, e := readWindowStoppedV14Native(wd+"/native-payloads", payload, files["campaign/ledger-000001/accounting.jsonl"], files, used)
	if e != nil {
		return fail(e.Error())
	}
	a := audit.State
	contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
	contract.ClaimSHA256 = windowStoppedV14ClaimSHA
	if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(r.Accounting, a) || r.LedgerSHA256 != audit.JournalSHA256 || r.AccountingSource != "live_owner_snapshot" || r.LedgerCompletionProof != "unavailable_from_journal" {
		return fail("accounting linkage")
	}
	rows := map[string]windowAcquisitionRow{}
	at, fin, turns := 0, 0, 0
	if len(r.Rows) != 10 {
		return fail("complete current rows")
	}
	for j, row := range r.Rows {
		ordinal := w.FirstOrdinal + j/2
		slot := m.Scope.Slots[ordinal-1]
		if row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns || row.QualityEnd != nil || row.FailureEvidence != nil || row.ExcludeCompleteTaskComparison {
			return fail("row identity")
		}
		if j >= 4 {
			want := windowAcquisitionRow{Ordinal: ordinal, Pass: slot.Pass, CaseKey: slot.CaseKey, Arm: slot.ArmOrder[j%2], DefinedTurns: slot.UserTurns, Status: "not_started"}
			if !reflect.DeepEqual(row, want) {
				return fail("unattempted row")
			}
			continue
		}
		if !row.Attempted || row.Finished != (j < 3) || row.Cleanup != (j < 3) || row.Observer != (j < 3) || len(row.TraceID) != 32 || row.RecordingFile != fmt.Sprintf("arm-%06d-%s.json", ordinal, row.Arm) || conversationHash(files[wd+"/"+row.RecordingFile]) != row.RecordingSHA256 {
			return fail("attempted row")
		}
		if j < 3 && row.ObservedTurns != row.DefinedTurns || j == 3 && (row.ObservedTurns != 1 || row.Status != "failed" || row.Disposition != "halt" || row.StructuralPass || row.Success || row.PreviewFailure != nil) {
			return fail("finished/failed boundary")
		}
		record, e := decodeWindowStoppedV2Recording(files[wd+"/"+row.RecordingFile])
		if e != nil {
			return fail("record JSON")
		}
		if record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Disposition != row.Disposition || record.Result.StructuralPass != row.StructuralPass || record.Result.Success != row.Success {
			return fail("record row linkage")
		}
		if j < 3 {
			if validateWindowStoppedV13Recording(record, row, files, wd, used) != nil {
				return fail("original V13 policy record replay")
			}
		} else if validateWindowStoppedV14InterruptedRecording(record) != nil {
			return fail("original V14 interrupted policy replay")
		}
		at++
		if row.Finished {
			fin++
		}
		turns += row.ObservedTurns
		rows[row.CaseKey+"/"+row.Arm] = row
	}
	for _, packet := range payload.Packets {
		row, ok := rows[packet.Identity.CaseKey+"/"+packet.Identity.Arm]
		if !ok || packet.Identity.Pass != row.Pass || packet.Identity.Turn < 1 || packet.Identity.Turn > row.ObservedTurns {
			return fail("native row coverage")
		}
	}
	runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: 8, LogicalJEVCalls: 2, NativeModelIntents: 8, NativeJEVIntents: 2, NativeSettlements: 9, NativePending: 1, Halt: "upstream_failure", Known: windowV8Amounts{a.ModelKnown, a.JEVKnown}, PendingHeld: windowV8Amounts{0, 2688}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{181782, 3811}, TotalPermanent: windowV8Amounts{base.PermanentModel, base.PermanentJEV}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
	if r.Schema != "full-window-acquisition-receipt.v14" || r.EvidenceKind != "recorded_run" || r.Status != "stopped" || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != 1 || r.WindowID != w.ID || r.PlannedCampaignPairs != 3275 || r.PlannedCampaignArms != 6550 || r.PlannedCampaignTurns != 10504 || r.PlannedCurrentPairs != 5 || r.PlannedCurrentArms != 10 || r.PlannedCurrentTurns != 22 || r.AttemptedArms != at || at != 4 || r.FinishedArms != fin || fin != 3 || r.ObservedTurns != turns || turns != 8 || r.UnrunArms != 6 || r.QualityNegativeArms != 1 || r.GlobalUnrunTurns != 14 || r.FirstFailureStage != "arm_proof" || r.FirstFailureOrdinal != 387 || r.FirstFailureArm != "jev_selective" || r.Cleanup || r.Observer || r.Advanced || r.PayloadVerified || !r.EnvironmentCleanup || r.NextClaimSHA256 != "" || r.OperationLimitSeconds != 600 || r.DurationMS != 68673.136 || r.PayloadStatus != "audit_only" || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || r.PayloadPackets != 10 || !reflect.DeepEqual(r.V8, &runtime) || r.Completion != nil || r.QualityEndArms != 0 || r.QualityEndUnrunTurns != 0 || r.PayloadAuditOnlyCompletion != "unavailable_from_payload_capture" {
		return fail("receipt negative boundary")
	}
	if validateWindowStoppedV11Observation(m, r.Observation, false) != nil || validateWindowStoppedV11Observation(m, s.Observation, false) != nil {
		return fail("observation DATA")
	}
	want := FullWindowActualStatus{Observation: s.Observation, V8: &runtime, EvidenceKind: "recorded_run", Closed: true, NextWindowIndex: 1, PlannedPairs: 3275, PlannedArms: 6550, DefinedTurns: 10504, AttemptedArms: 4, FinishedArms: 3, FailedAttemptedArms: 1, UnattemptedArms: 6546, ObservedTurns: 8, GlobalUnrunTurns: 10496, ModelAttempts: 8, JEVAttempts: 2}
	if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != 1 || s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || s.LedgerSHA256 != r.LedgerSHA256 || s.ReportStatus != "stopped" || s.FirstUnrunOrdinal != 388 || s.FirstUnrunArm != "jev_selective" || s.FutureUnrunFromOrdinal != 391 {
		return fail("summary")
	}
	var terminal FullWindowActualStatus
	if decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil || validateWindowStoppedV11Observation(m, terminal.Observation, true) != nil {
		return fail("terminal observation")
	}
	want.Observation = terminal.Observation
	want.LastReceiptSHA256 = conversationHash(files["records/window-000001-summary.json"])
	if !reflect.DeepEqual(terminal, want) {
		return fail("terminal incomplete")
	}
	for n := range files {
		if (strings.Contains(n, "/native-payloads/") || strings.Contains(n, "/preview-failure-")) && !used[n] {
			return fail("unreferenced native/preview original")
		}
	}
	base.ActualModelCalls += 8
	base.ActualJEVCalls += 2
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	return base, nil
}
