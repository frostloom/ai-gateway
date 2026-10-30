package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

// All top-level bytes have fixed source pins. Descendant records and native
// bodies must still be read and checked, rather than accepting their manifests
// as proof that those bytes exist. No old accounting state is mutated.
func readWindowStoppedV2Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	return readWindowStoppedV2BundleWithPass(nil, root, m, first, files)
}

func readWindowStoppedV2BundleWithPass(p *historyReadPass, root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func() (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v2 bundle linkage rejected")
	}
	if !reflect.DeepEqual(first, expectedStoppedWindowBaseline()) || len(m.Scope.Windows) != 131 || len(m.Scope.Slots) != 3660 || len(files) != 38 {
		return reject()
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	var prior FullWindowActualReceipt
	var lastAudit windowAccountingAudit
	totalPackets, totalRecords, unknown := 0, 0, 0
	traces := map[string]bool{}
	campaignNames := map[string]bool{".lock": false}
	recordNames := map[string]bool{"startup.json": false, "terminal.json": false}
	for i := 1; i <= 6; i++ {
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
		claimSHA := conversationHash(files[claimName])
		w := m.Scope.Windows[i-1]
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if c.Schema != "full-window-campaign-claim.v2" || c.BaselineEvidenceSHA256 != m.Scope.BaselineEvidenceSHA256 || c.Root != m.CampaignRoot || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return reject()
		}
		if i == 1 {
			if c.PreviousClaimSHA256 != "" || c.PreviousCheckpointSHA256 != "" {
				return reject()
			}
		} else if c.PreviousClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i-1)]) || c.PreviousCheckpointSHA256 != conversationHash(files[fmt.Sprintf("campaign/checkpoint-%06d.json", i-1)]) {
			return reject()
		}
		base.ClaimSHA256[fmt.Sprintf("full1220-v2-20261028/window-%06d", i)] = claimSHA
		audit, err := readWindowAccountingBytes(files[ledgerName])
		if err != nil {
			return reject()
		}
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(payload.Contract, contract) || !reflect.DeepEqual(r.Accounting, audit.State) || !audit.State.Closed || audit.State.EnvelopeViolation || r.AccountingSource != "live_owner_snapshot" || r.LedgerSHA256 != conversationHash(files[ledgerName]) || s.LedgerSHA256 != r.LedgerSHA256 || r.LedgerCompletionProof != "unavailable_from_journal" {
			return reject()
		}
		if s.ReportSHA256 != conversationHash(files[wd+"/receipt.json"]) || r.PayloadManifestSHA256 != conversationHash(files[wd+"/payload-manifest.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || r.PayloadPackets != len(payload.Packets) || r.ScopeSHA256 != m.ScopeSHA256 || r.ExecutionSHA256 != m.ExecutionSHA256 || r.WindowIndex != i || r.WindowID != w.ID || r.EvidenceKind != "recorded_run" || r.OperationLimitSeconds != 300 || len(r.Rows) != 56 {
			return reject()
		}
		if err = readWindowStoppedV2PayloadWithPass(p, filepath.Join(root, wd, "native-payloads"), payload); err != nil {
			return fullWindowBaseline{}, err
		}
		names := map[string]bool{"receipt.json": false, "payload-manifest.json": false, "native-payloads": true}
		attempted, finished, turns, models, jevs := 0, 0, 0, 0, 0
		rows := map[string]windowAcquisitionRow{}
		for start := 0; start < len(r.Rows); start += 4 {
			end := min(start+4, len(r.Rows))
			var specs []windowStoppedReadSpec
			var attemptedRows []windowAcquisitionRow
			for offset, row := range r.Rows[start:end] {
				j := start + offset
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
				specs = append(specs, windowStoppedReadSpec{path: filepath.Join(root, wd, row.RecordingFile), sha256: row.RecordingSHA256, maxBytes: 8 << 20, kind: windowStoppedReadFile})
				attemptedRows = append(attemptedRows, row)
			}
			if len(specs) == 0 {
				continue
			}
			records, err := historyStoppedBatch(p, specs)
			if err != nil {
				return reject()
			}
			for index, row := range attemptedRows {
				raw := records[index]
				record, decodeErr := decodeWindowStoppedV2Recording(raw)
				if decodeErr != nil || record.Result.Case.Key() != row.CaseKey || len(record.Result.Observations) != row.ObservedTurns || record.Result.Status != row.Status || record.Result.Success != row.Success {
					return reject()
				}
				resultRaw, err := json.Marshal(record.Result)
				if err != nil || conversationHash(resultRaw) != record.ResultSHA256 {
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
			if !p.Response.Available {
				unknown++
				if i != 6 || p.Sequence != len(payload.Packets) || audit.State.Pending == nil || audit.State.Pending.Sequence != p.Sequence || audit.State.Pending.Request != p.Identity {
					return reject()
				}
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
		if i == 6 {
			want.Closed = true
			want.NextWindowIndex = 6
		}
		if !reflect.DeepEqual(s.FullWindowActualStatus, want) || s.WindowIndex != i {
			return reject()
		}
		if i < 6 {
			if r.Status != "collected_window" || s.ReportStatus != "collected_window" || !r.Advanced || !r.Cleanup || !r.Observer || !r.PayloadVerified || audit.State.Pending != nil || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.Halt != "" || r.NextClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) {
				return reject()
			}
			campaignNames[fmt.Sprintf("checkpoint-%06d.json", i)] = false
		} else if r.Status != "stopped" || s.ReportStatus != "stopped" || r.Advanced || r.Cleanup || r.Observer || r.PayloadVerified || r.NextClaimSHA256 != "" || audit.State.Halt != "upstream_failure" || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 2688 || audit.State.Pending == nil || audit.State.Pending.Request.Provider != "jev" {
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
	want.LastReceiptSHA256 = conversationHash(files["records/window-000006-summary.json"])
	if !reflect.DeepEqual(terminal, want) || terminal.AttemptedArms != 334 || terminal.FinishedArms != 333 || terminal.ObservedTurns != 396 || terminal.ModelAttempts != 665 || terminal.JEVAttempts != 167 || totalRecords != 334 || totalPackets != 832 || unknown != 1 || lastAudit.State.Pending.Sequence != 140 {
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
	if base.PermanentModel != 33395934 || base.PermanentJEV != 259511 || base.ActualModelCalls != 809 || base.ActualJEVCalls != 204 || len(base.ClaimSHA256) != 16 {
		return reject()
	}
	return base, nil
}

func decodeWindowStoppedV2Recording(raw []byte) (collectionRecording, error) {
	var r collectionRecording
	// The caller has already verified the original recording SHA against its
	// pinned receipt. Records contain legitimate null business state/arrays;
	// keep duplicate-key, depth, trailing-data and unknown-field rejection, but
	// do not apply the readiness manifest's recursive non-null constraint.
	if _, err := windowJSONObject(raw); err != nil {
		return r, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	err := d.Decode(&r)
	return r, err
}
