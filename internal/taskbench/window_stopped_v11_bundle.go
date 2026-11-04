package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func readWindowStoppedV11Native(dir string, payload windowPayloadManifest, ledger []byte, files map[string][]byte, used map[string]bool, pending bool) (windowAccountingAudit, error) {
	reject := func(reason string) (windowAccountingAudit, error) {
		return windowAccountingAudit{}, fmt.Errorf("settled native data %s", reason)
	}
	audit, err := readWindowAccountingBytes(ledger)
	a := audit.State
	if err != nil || audit.Contract.Schema != windowAccountingV3 || !reflect.DeepEqual(audit.Contract, payload.Contract) || !a.Closed || a.Halt != map[bool]string{false: "", true: "upstream_failure"}[pending] || a.EnvelopeViolation || pending && (len(payload.Packets) != 18 || a.Pending == nil || a.Pending.Sequence != 18 || a.ModelHeld != 74752) || a.RetryRequired != nil || len(a.UnknownIntents) != 0 || a.JEVHeld != 0 || a.ModelUnknownHeld != 0 || a.JEVUnknownHeld != 0 || a.ModelExtraFunded != 0 || a.JEVExtraFunded != 0 || payload.Schema != "full-window-payload.v2" || payload.EvidenceKind != "recorded_run" || payload.CompletionProof != "unavailable_from_payload_capture" || len(payload.Packets) < 1 || len(payload.Packets) > 260 {
		return reject("contract/state")
	}
	get := func(name string) []byte { used[dir+"/"+name] = true; return files[dir+"/"+name] }
	var store windowPayloadManifest
	if decodeWindowActualJSON(get("store.json"), &store) != nil || store.Schema != payload.Schema || store.EvidenceKind != payload.EvidenceKind || store.CompletionProof != payload.CompletionProof || !reflect.DeepEqual(store.Contract, payload.Contract) || len(store.Packets) != 0 {
		return reject("store")
	}
	intents, settles := map[int]windowAccountingEvent{}, map[int]windowAccountingEvent{}
	for _, line := range bytes.Split(bytes.TrimSuffix(ledger, []byte("\n")), []byte("\n")) {
		var event windowAccountingEvent
		if json.Unmarshal(line, &event) != nil {
			return reject("event")
		}
		switch event.Kind {
		case "intent":
			intents[event.Sequence] = event
		case "settle":
			settles[event.Sequence] = event
		case "config", "close":
		case "halt":
			if !pending || event.Code != "upstream_failure" {
				return reject("unrecognized halt")
			}
		default:
			return reject("retry/unknown/extra event")
		}
	}
	if len(intents) != len(payload.Packets) || len(settles) != len(payload.Packets)-map[bool]int{false: 0, true: 1}[pending] || a.ModelAttempts+a.JEVAttempts != len(payload.Packets) {
		return reject("complete native coverage")
	}
	for i, packet := range payload.Packets {
		seq := i + 1
		id := packet.Identity
		c := payload.Contract
		intent, ok := intents[seq]
		settle, settled := settles[seq]
		if !ok || settled != !(pending && seq == 18) || intent.Request == nil || *intent.Request != id || packet.Sequence != seq || packet.ScopeSHA256 != c.ScopeSHA256 || packet.ClaimSHA256 != c.ClaimSHA256 || packet.WindowID != c.WindowID || c.validateRequest(id) != nil || id.NativeAttempt != 1 || id.Attempt > 5 || id.Provider == "jev" && id.Attempt != 1 || packet.FailureDescriptorSHA256 != "" && !(pending && seq == 18) {
			return reject("native identity")
		}
		requestName, responseName := fmt.Sprintf("request-%06d", seq), fmt.Sprintf("response-%06d", seq)
		if packet.RequestDescriptorFile != requestName+".json" || packet.Request.File != requestName+".body" || !packet.Request.Complete || packet.Request.PrefixSHA256 != "" || packet.Request.FullSHA256 != id.BodySHA256 || packet.Request.Bytes != id.Bytes {
			return reject("request descriptor")
		}
		raw := get(requestName + ".json")
		var request windowPayloadPacket
		want := packet
		want.Response = nil
		want.FailureDescriptorSHA256 = ""
		if decodeWindowActualJSON(raw, &request) != nil || !reflect.DeepEqual(request, want) {
			return reject("request packet")
		}
		limits := c.Model
		if id.Provider == "jev" {
			limits = c.JEV
		}
		endpoint, _, err := windowProviderEndpoint(id.Provider)
		body := get(requestName + ".body")
		if err != nil || validateWindowPOST(id.Provider, "POST", endpoint, body, limits, id) != nil {
			return reject("request body")
		}
		if pending && seq == 18 {
			r := packet.Response
			var response windowPayloadPacket
			want := packet
			want.Request = windowPayloadBody{}
			want.RequestDescriptorFile = ""
			want.FailureDescriptorSHA256 = ""
			raw := get(responseName + ".json")
			if a.Pending == nil || *a.Pending != (windowPendingIntent{Sequence: 18, Request: id}) || a.ModelHeld != 74752 || a.JEVHeld != 0 || a.ModelAttempts != 14 || a.JEVAttempts != 4 || settled || id.Provider != "model" || decodeWindowActualJSON(raw, &response) != nil || !reflect.DeepEqual(response, want) || conversationHash(raw) != packet.FailureDescriptorSHA256 || r == nil || r.DescriptorFile != responseName+".json" || r.Status != 0 || r.Code != "round_trip" || r.Complete || r.Available || r.Body != nil || r.TransportFailure == nil || *r.TransportFailure != (windowTransportFailure{Cause: "connection_reset", Phase: "await_response", ElapsedMS: 1877, TraceObserved: true, ConnectionReused: true, RequestWritten: true, FirstByte: false}) {
				return reject("original pending written/no-response boundary")
			}
			if _, exists := files[dir+"/"+responseName+".body"]; exists {
				return reject("pending fabricated body")
			}
			continue
		}
		r := packet.Response
		if r == nil || r.DescriptorFile != responseName+".json" || r.TransportFailure != nil || !r.Available || !r.Complete || r.Status != 200 || r.Code != "complete" || r.Body == nil || r.Body.File != responseName+".body" || !r.Body.Complete || r.Body.PrefixSHA256 != "" || !comparisonDigest(r.Body.FullSHA256) {
			return reject("complete response")
		}
		var response windowPayloadPacket
		want = packet
		want.Request = windowPayloadBody{}
		want.RequestDescriptorFile = ""
		if decodeWindowActualJSON(get(responseName+".json"), &response) != nil || !reflect.DeepEqual(response, want) {
			return reject("response packet")
		}
		responseBody := get(responseName + ".body")
		if len(responseBody) != r.Body.Bytes || conversationHash(responseBody) != r.Body.FullSHA256 || settle.ResponseSHA256 != r.Body.FullSHA256 {
			return reject("response bytes/hash")
		}
		usage, err := parseWindowResponseUsage(id.Provider, responseBody, limits)
		if err != nil || !reflect.DeepEqual(usage, settle.Usage) {
			return reject("fresh usage/cache")
		}
	}
	if !pending && (a.Pending != nil || a.ModelHeld != 0 || a.JEVHeld != 0) {
		return reject("unexpected pending hold")
	}
	return audit, nil
}

// Validate stopped source DATA without replaying Step or minting normal proofs.
// Fixed inventory hashes authenticate all ancillary originals separately.
func readWindowStoppedV11Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	fail := func(s string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v11 bundle: %s", s)
	}
	if !reflect.DeepEqual(first, expectedStoppedV10WindowBaseline()) || validateWindowV11ExecutionShape(m.Scope) != nil || len(files) != 13125 {
		return fail("fixed104/full inventory")
	}
	for _, name := range []string{"campaign/claim-000051.json", "campaign/checkpoint-000050.json"} {
		if _, ok := files[name]; ok {
			return fail("later claim/checkpoint")
		}
	}
	var cleanup map[string]any
	var owner map[string]any
	if json.Unmarshal(files["actual-owner-group-cleanup.json"], &cleanup) != nil || cleanup["group_empty"] != true || cleanup["leader_reaped"] != true || cleanup["confirmed"] != true || cleanup["owned_pgid"] != float64(80632) || cleanup["error_type"] != "PermissionError" || !reflect.DeepEqual(cleanup["errors"], []any{[]any{"probe", "PermissionError"}, []any{"SIGTERM", "PermissionError"}, []any{"probe", "PermissionError"}}) || json.Unmarshal(files["actual-owner-exit.json"], &owner) != nil || owner["exit_code"] != float64(1) || owner["relaunch_allowed"] != false {
		return fail("stopped/reaped owner")
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	priorClaim, priorCheckpoint, priorSummary := "", "", ""
	used := map[string]bool{}
	models, jevs, attempted, finished, turns := 0, 0, 0, 0, 0
	var cumulative *windowV8RuntimeSummary
	var last FullWindowActualStatus
	for i := 1; i <= 50; i++ {
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
		if c.Schema != "full-window-campaign-claim.v11" || c.Root != root+"/campaign" || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.Window != w || c.BaselineEvidenceSHA256 != windowV11BaselineSHA || c.BudgetApprovalSHA256 != windowStoppedV11AuthorizationSHA || c.ParentModel != 195000000 || c.ParentJEV != 3000000 || c.PreviousClaimSHA256 != priorClaim || c.PreviousCheckpointSHA256 != priorCheckpoint || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return fail("claim chain")
		}
		claimSHA := conversationHash(files[cn])
		base.ClaimSHA256[fmt.Sprintf("full1220-v11-20261101/window-%06d", i)] = claimSHA
		halt := ""
		if i == 50 {
			halt = "upstream_failure"
		}
		audit, e := readWindowStoppedV11Native(wd+"/native-payloads", payload, files[ln], files, used, i == 50)
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
			if row.Ordinal != ordinal || row.Pass != slot.Pass || row.CaseKey != slot.CaseKey || row.Arm != slot.ArmOrder[j%2] || row.DefinedTurns != slot.UserTurns || row.QualityEnd != nil || row.FailureEvidence != nil {
				return fail("row identity")
			}
			if !row.Attempted {
				if i != 50 || j != 7 || row.Finished || row.ObservedTurns != 0 || row.Cleanup || row.Observer || row.Status != "not_started" || row.RecordingFile != "" {
					return fail("unattempted partition")
				}
				continue
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
			if e != nil || windowV8DeclarationSHA(record.Result) != record.ResultSHA256 || record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Disposition != row.Disposition {
				return fail("original result DATA")
			}
			if i == 50 && j == 6 {
				if row.Finished || row.Cleanup || row.Observer || row.Status != "failed" || row.Disposition != "halt" || !strings.Contains(strings.Join(record.Result.Errors, "\n"), "turn 2: owned Handler arm stopped") {
					return fail("original stopped Step must remain")
				}
			} else if !row.Finished || !row.Cleanup || !row.Observer || row.ObservedTurns != row.DefinedTurns {
				return fail("prior completed row")
			}
			evidence = append(evidence, windowCampaignArmEvidence{Ordinal: ordinal, Pass: row.Pass, ObservedTurns: row.ObservedTurns, Key: row.CaseKey, Arm: row.Arm, RecordingSHA256: row.RecordingSHA256, Cleanup: row.Cleanup, Observer: row.Observer})
			rowMap[row.CaseKey+"/"+row.Arm] = row
		}
		for _, packet := range payload.Packets {
			row, ok := rowMap[packet.Identity.CaseKey+"/"+packet.Identity.Arm]
			if !ok || packet.Identity.Pass != row.Pass || packet.Identity.Turn < 1 || packet.Identity.Turn > row.ObservedTurns {
				return fail("native row coverage")
			}
		}
		runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: a.ModelAttempts, LogicalJEVCalls: a.JEVAttempts, NativeModelIntents: a.ModelAttempts, NativeJEVIntents: a.JEVAttempts, NativeSettlements: a.ModelAttempts + a.JEVAttempts - map[bool]int{false: 0, true: 1}[i == 50], NativePending: map[bool]int{false: 0, true: 1}[i == 50], PendingHeld: windowV8Amounts{a.ModelHeld, a.JEVHeld}, Halt: halt, Known: windowV8Amounts{a.ModelKnown, a.JEVKnown}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{base.PermanentModel - first.PermanentModel, base.PermanentJEV - first.PermanentJEV}, TotalPermanent: windowV8Amounts{base.PermanentModel, base.PermanentJEV}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
		if i < 50 {
			var cp windowCampaignCheckpoint
			if decodeWindowActualJSON(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i)], &cp) != nil || cp.Schema != "full-window-campaign-owned-checkpoint.v11" || cp.Index != i || cp.ClaimSHA256 != claimSHA || cp.ScopeSHA256 != m.ScopeSHA256 || cp.ExecutionSHA256 != m.ExecutionSHA256 || cp.LedgerSHA256 != audit.JournalSHA256 || !reflect.DeepEqual(cp.Evidence, evidence) || !reflect.DeepEqual(cp.V8, &runtime) {
				return fail("original checkpoint DATA")
			}
			priorCheckpoint = conversationHash(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i)])
			nw := m.Scope.Windows[i]
			runtime.BasePermanent.Model += nw.ModelCap
			runtime.BasePermanent.JEV += nw.JEVCap
			runtime.TotalPermanent.Model += nw.ModelCap
			runtime.TotalPermanent.JEV += nw.JEVCap
			if r.Status != "collected_window" || !r.Cleanup || !r.Observer || !r.Advanced || !r.PayloadVerified || r.NextClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) || at != len(r.Rows) || fin != at {
				return fail("original completed window")
			}
		} else if r.Status != "stopped" || r.Cleanup || r.Observer || r.Advanced || r.PayloadVerified || r.PayloadStatus != "audit_only" || r.FirstFailureStage != "arm_proof" || r.FirstFailureOrdinal != 362 || r.FirstFailureArm != "jev_selective" || r.NextClaimSHA256 != "" || at != 7 || fin != 6 || obs != 20 || r.ObserverCountersCumulative.Accepted != 6426 || r.ObserverCountersCumulative.Exported != 0 || r.ObserverCountersCumulative.Failed != 0 {
			return fail("stopped50 original negative boundary")
		}
		if r.Schema != "full-window-acquisition-receipt.v11" || r.EvidenceKind != "recorded_run" || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != i || r.WindowID != w.ID || r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || len(r.Rows) != 2*(w.LastOrdinal-w.FirstOrdinal+1) || r.AttemptedArms != at || r.FinishedArms != fin || r.ObservedTurns != obs || r.OperationLimitSeconds != 300 || !r.EnvironmentCleanup || !reflect.DeepEqual(r.Completion, m.Scope.Completion) || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || r.PayloadPackets != len(payload.Packets) || !reflect.DeepEqual(r.V8, &runtime) {
			return fail("receipt DATA linkage")
		}
		models += a.ModelAttempts
		jevs += a.JEVAttempts
		attempted += at
		finished += fin
		turns += obs
		cumulative = mergeWindowV8Runtime(cumulative, &runtime)
		want := FullWindowActualStatus{Completion: m.Scope.Completion, V8: cumulative, EvidenceKind: "recorded_run", Observation: s.Observation, BoundariesExcluded: 1851, Closed: i == 50, NextWindowIndex: min(i+1, 50), LastReceiptSHA256: priorSummary, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, AttemptedArms: attempted, FinishedArms: finished, FailedAttemptedArms: attempted - finished, UnattemptedArms: 7320 - attempted, ObservedTurns: turns, GlobalUnrunTurns: 11772 - turns, ModelAttempts: models, JEVAttempts: jevs}
		if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != i || s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || s.LedgerSHA256 != r.LedgerSHA256 || s.ReportStatus != r.Status {
			return fail("summary chain")
		}
		last = want
		priorClaim = claimSHA
		priorSummary = conversationHash(files[sn])
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

// Snapshot identity only. A recorded durable-local status cannot grant remote or
// normal completion authority; the stopped terminal is deliberately non-normal.
func validateWindowStoppedV11Observation(m windowActualManifest, o *windowObservationReceiptV11, closed bool) error {
	if o == nil || o.Boundary != "durable_local" || o.RemoteDelivery != "pending" || o.NormalClosure || o.Spool.Closed != closed || o.Spool.RunID != m.Observer.SpoolRunID || o.Spool.RootSHA256 != m.Observer.SpoolRootSHA256 || o.Spool.MaxBytes != 4<<30 || o.Spool.Poisoned || o.Spool.DataOnly || o.Spool.DurabilityReturnUnproven || o.Spool.OwnerCompletionUnproven {
		return fmt.Errorf("stopped observation boundary")
	}
	return nil
}
