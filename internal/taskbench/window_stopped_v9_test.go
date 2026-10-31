package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowStoppedV9Exact102DataOnly(t *testing.T) {
	b := expectedStoppedV9WindowBaseline()
	if len(b.ClaimSHA256) != 102 || b.TotalModel != 200000000 || b.TotalJEV != 4000000 || b.PermanentModel != 66472061 || b.PermanentJEV != 631114 || b.RemainingModel != 133527939 || b.RemainingJEV != 3368886 || b.ActualModelCalls != 5137 || b.ActualJEVCalls != 1308 {
		t.Fatalf("historical102 liability/calls: %+v", b)
	}
	for k, v := range expectedStoppedV8WindowBaseline().ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old46 rewritten")
		}
	}
	delete(b.ClaimSHA256, "full1220-v9-r5-20261031/window-000056")
	if len(expectedStoppedV9WindowBaseline().ClaimSHA256) != 102 {
		t.Fatal("shared historical map")
	}
	var data windowStoppedV9Baseline
	if _, ok := any(data).(windowStoppedHistory); ok {
		t.Fatal("data baseline grants execution history capability")
	}
	if _, ok := any(data).(interface {
		revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error)
	}); ok {
		t.Fatal("data baseline grants reentry capability")
	}
}
func stoppedV9DescriptorFixture() windowStoppedV9Evidence {
	return windowStoppedV9Evidence{Schema: "full-window-stopped-baseline.v9", PreviousBaseline: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/v9-stopped46-baseline-20261030/stopped-v8.private.json", SHA256: "2f87853fe1a8df139a2093e1a34d91eb8758cd6ea547755e02d80bb98d3b2a3c"}, OriginalRoot: "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031", BudgetApproval: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031/human-approval-reply.private.json", SHA256: "22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57"}, Manifest: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031/actual-manifest.json", SHA256: "aa9a664d767c080648213e3d98c63c340d2207afea9121d104bbb2775fdc7666"}, StoppedAudit: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/v9-live-score-horizon-20261031/r5-stop-h056-delivery-audit.json", SHA256: "72e296c427039da615518050d96a9144af3ad3db69e1bbaf20e4ed8418386250"}, Inventory: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/v9-live-score-horizon-20261031/r5-stopped102-preparation/original-inventory.json", SHA256: "d94416208084bc50a81ac1aa69bea8c9b646d30e2c2d14239218903e58c95c0f"}, SourceConfirmation: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/v9-live-score-horizon-20261031/r5-stopped102-preparation-v2/source-confirmation.json", SHA256: "0be98c5fb6c13dbe01758a245df6fa755b679d5785338316d1721f09677a5dbf"}, OwnerExit: windowActualFile{Path: "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031/actual-owner-exit.json", SHA256: "2373309ed9886d2ae2f77f9f37532e69630b07c44eab5541e6e06fe2eb04c00e"}}
}
func TestWindowStoppedV9DescriptorExternalFixedBindings(t *testing.T) {
	e := stoppedV9DescriptorFixture()
	if err := validateWindowStoppedV9Descriptor(e); err != nil {
		t.Fatal(err)
	}
	for _, mut := range []func(*windowStoppedV9Evidence){func(v *windowStoppedV9Evidence) { v.Schema = "full-window-stopped-baseline.v8" }, func(v *windowStoppedV9Evidence) {
		v.SourceConfirmation.Path = filepath.Join(v.OriginalRoot, "source-confirmation.json")
	}, func(v *windowStoppedV9Evidence) { v.Inventory.SHA256 = v.Manifest.SHA256 }, func(v *windowStoppedV9Evidence) { v.PreviousBaseline.Path += ".copy" }, func(v *windowStoppedV9Evidence) { v.OriginalRoot = "/synthetic/other-r5" }} {
		v := e
		mut(&v)
		if validateWindowStoppedV9Descriptor(v) == nil {
			t.Fatal("substituted descriptor accepted")
		}
	}
}
func TestWindowStoppedV9UntrustedCancelNoEffects(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	path := filepath.Join(root, "descriptor.json")
	raw := []byte(`{}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{windowStoppedV9DescriptorSHA, conversationHash(raw), windowV9BaselineSHA} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, err := loadWindowStoppedV9BaselineContext(ctx, path, sha)
		if err == nil || len(out.base.ClaimSHA256) != 0 {
			t.Fatal("canceled/untrusted evidence accepted")
		}
	}
	if _, err := loadWindowStoppedV9BaselineWithPass(nil, path, windowStoppedV9DescriptorSHA); err == nil {
		t.Fatal("unowned pass accepted")
	}
	names, _ := os.ReadDir(root)
	if len(names) != 1 {
		t.Fatal("rejection mutated namespace")
	}
}
func TestWindowStoppedV9HistoryEpochBounded(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	lock := filepath.Join(root, ".lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := newHistoryReadPass(context.Background())
	if err := p.WithStoppedLock(9, lock, func() error { return nil }); err != nil {
		t.Fatal("readonly ninth epoch rejected", err)
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	p, _ = newHistoryReadPass(context.Background())
	if err := p.WithStoppedLock(10, lock, func() error { return nil }); err == nil {
		t.Fatal("unbounded epoch accepted")
	}
	_ = p.Finish()
}

func stoppedV9Put(t *testing.T, files map[string][]byte, name string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	files[name] = raw
}
func stoppedV9BundleFixture(t *testing.T) (string, windowActualManifest, map[string][]byte) {
	t.Helper()
	root := "/synthetic/r5"
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{Scope: scope, ScopeSHA256: strings.Repeat("a", 64), ExecutionSHA256: strings.Repeat("b", 64), CampaignRoot: root + "/campaign", Limits: windowV4ExpectedLimits()}
	files := map[string][]byte{"campaign/.lock": {}}
	stoppedV9Put(t, files, "records/startup.json", FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, UnattemptedArms: 7320, GlobalUnrunTurns: 11772})
	first := expectedStoppedV8WindowBaseline()
	model, jev := first.PermanentModel, first.PermanentJEV
	prevClaim, prevCP := "", ""
	extraModel := 294
	skippedLastSelective := 0
	claims := make([]windowCampaignClaim, 56)
	audits := make([]windowAccountingAudit, 56)
	rows := make([][]windowAcquisitionRow, 56)
	runtimes := make([]windowV8RuntimeSummary, 56)
	caseByKey := map[string]ConversationCase{}
	for _, c := range s.Cases {
		caseByKey[c.Key()] = c
	}
	for i := 1; i <= 56; i++ {
		w := scope.Windows[i-1]
		model += w.ModelCap
		jev += w.JEVCap
		claim := windowCampaignClaim{Schema: "full-window-campaign-claim.v9", BaselineEvidenceSHA256: windowV9BaselineSHA, BudgetApprovalSHA256: "bea97cb062f26b8dda1dd1b921b3120ab0404ff8d0662b6392287fec3f6097eb", ParentModel: 180000000, ParentJEV: 2500000, Root: m.CampaignRoot, Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: scope.DefinitionSHA256, ManifestSHA256: scope.ManifestSHA256, SelectionSHA256: scope.SelectionSHA256, ProfileSHA256: scope.ProfileSHA256, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: scope.Bindings.PolicySHA256, PreviousClaimSHA256: prevClaim, PreviousCheckpointSHA256: prevCP, Window: w, HistoricalClaims: first.ClaimSHA256, ModelPermanent: model, JEVPermanent: jev}
		cn := fmt.Sprintf("campaign/claim-%06d.json", i)
		stoppedV9Put(t, files, cn, claim)
		claims[i-1] = claim
		claimSHA := conversationHash(files[cn])
		prevClaim = claimSHA
		contract := contractWindowCampaign(scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		var ledger []byte
		index := 0
		previous := ""
		appendEvent := func(e windowAccountingEvent) {
			e.Index = index
			e.PreviousSHA256 = previous
			raw, _ := json.Marshal(e)
			ledger = append(ledger, raw...)
			ledger = append(ledger, '\n')
			previous = conversationHash(raw)
			index++
		}
		appendEvent(windowAccountingEvent{Kind: "config", Contract: &contract})
		wd := fmt.Sprintf("records/window-%06d", i)
		nd := wd + "/native-payloads"
		payload := windowPayloadManifest{Schema: "full-window-payload.v2", EvidenceKind: "recorded_run", CompletionProof: "unavailable_from_payload_capture", Contract: contract, Packets: []windowPayloadPacket{}}
		stoppedV9Put(t, files, nd+"/store.json", payload)
		sequence := 0
		evidence := []windowCampaignArmEvidence{}
		addPacket := func(provider string, row windowAcquisitionRow, turn, attempt int) {
			sequence++
			request := []byte(`{"model":"deepseek-flash","max_tokens":128,"messages":[{"role":"user","content":[{"type":"text","text":"synthetic"}]}],"thinking":{"type":"disabled"}}`)
			response := []byte(`{"model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`)
			mod := "deepseek-flash"
			if provider == "jev" {
				mod = "jev-1.13.0"
				request = []byte(`{"model":"jev-1.13.0","state":"synthetic","questions":{"q":{"type":"noul"}}}`)
				response = []byte(`{"model":"jev-1.13.0","usage":{"input_tokens":1,"output_tokens":1}}`)
			}
			id := windowRequestIdentity{Provider: provider, Model: mod, CaseKey: row.CaseKey, Pass: row.Pass, Arm: row.Arm, Turn: turn, Attempt: attempt, NativeAttempt: 1, BodySHA256: conversationHash(request), Bytes: len(request)}
			usage := &windowUsage{Input: windowInt(1), Output: windowInt(1)}
			if provider == "model" {
				usage.CacheHit = windowInt(0)
				usage.CacheMiss = windowInt(1)
			}
			appendEvent(windowAccountingEvent{Kind: "intent", Sequence: sequence, Request: &id})
			appendEvent(windowAccountingEvent{Kind: "settle", Sequence: sequence, ResponseSHA256: conversationHash(response), Usage: usage})
			reqName := fmt.Sprintf("request-%06d", sequence)
			resName := fmt.Sprintf("response-%06d", sequence)
			packet := windowPayloadPacket{Sequence: sequence, Identity: id, ScopeSHA256: m.ScopeSHA256, ClaimSHA256: claimSHA, WindowID: w.ID, Request: windowPayloadBody{File: reqName + ".body", Bytes: len(request), Complete: true, FullSHA256: id.BodySHA256}, RequestDescriptorFile: reqName + ".json", Response: &windowPayloadResponse{Status: 200, Code: "complete", Complete: true, Available: true, Body: &windowPayloadBody{File: resName + ".body", Bytes: len(response), Complete: true, FullSHA256: conversationHash(response)}, DescriptorFile: resName + ".json"}}
			rq := packet
			rq.Response = nil
			stoppedV9Put(t, files, nd+"/"+reqName+".json", rq)
			rs := packet
			rs.Request = windowPayloadBody{}
			rs.RequestDescriptorFile = ""
			stoppedV9Put(t, files, nd+"/"+resName+".json", rs)
			files[nd+"/"+reqName+".body"] = request
			files[nd+"/"+resName+".body"] = response
			payload.Packets = append(payload.Packets, packet)
		}
		for ordinal := w.FirstOrdinal; ordinal <= w.LastOrdinal; ordinal++ {
			slot := scope.Slots[ordinal-1]
			for _, arm := range slot.ArmOrder {
				result := ConversationResult{Case: caseByKey[slot.CaseKey], Status: "failed", Observations: make([]ConversationObservation, slot.UserTurns)}
				resultRaw, _ := json.Marshal(result)
				record := collectionRecording{PolicyID: collectionPolicyID, PolicySHA256: collectionPolicySHA(), ResultSHA256: conversationHash(resultRaw), Disposition: "continue_recorded", Issues: []collectionIssue{}, Result: result}
				name := fmt.Sprintf("arm-%06d-%s.json", ordinal, arm)
				stoppedV9Put(t, files, wd+"/"+name, record)
				row := windowAcquisitionRow{Ordinal: ordinal, Pass: slot.Pass, DefinedTurns: slot.UserTurns, ObservedTurns: slot.UserTurns, CaseKey: slot.CaseKey, Arm: arm, TraceID: fmt.Sprintf("%032x", ordinal*2+len(rows[i-1])%2), RecordingSHA256: conversationHash(files[wd+"/"+name]), RecordingFile: name, Status: "failed", Disposition: "continue_recorded", Attempted: true, Finished: true, Cleanup: true, Observer: true}
				rows[i-1] = append(rows[i-1], row)
				evidence = append(evidence, windowCampaignArmEvidence{Ordinal: ordinal, Pass: slot.Pass, ObservedTurns: slot.UserTurns, Key: slot.CaseKey, Arm: arm, RecordingSHA256: row.RecordingSHA256, Cleanup: true, Observer: true})
				for turn := 1; turn <= slot.UserTurns; turn++ {
					if i == 56 && arm == "jev_selective" && skippedLastSelective < 4 {
						skippedLastSelective++
						continue
					}
					addPacket("model", row, turn, 1)
					if extraModel > 0 {
						addPacket("model", row, turn, 2)
						extraModel--
					}
				}
				if arm == "jev_selective" {
					addPacket("jev", row, 1, 1)
				}
			}
		}
		appendEvent(windowAccountingEvent{Kind: "close"})
		ln := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		files[ln] = ledger
		audit, err := readWindowAccountingBytes(ledger)
		if err != nil {
			t.Fatal("synthetic ledger", err)
		}
		audits[i-1] = audit
		stoppedV9Put(t, files, wd+"/payload-manifest.json", payload)
		runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: audit.State.ModelAttempts, LogicalJEVCalls: audit.State.JEVAttempts, NativeModelIntents: audit.State.ModelAttempts, NativeJEVIntents: audit.State.JEVAttempts, NativeSettlements: sequence, Known: windowV8Amounts{audit.State.ModelKnown, audit.State.JEVKnown}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{model - first.PermanentModel, jev - first.PermanentJEV}, TotalPermanent: windowV8Amounts{model, jev}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
		runtimes[i-1] = runtime
		cp := windowCampaignCheckpoint{V8: &runtime, Schema: "full-window-campaign-owned-checkpoint.v8", Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: scope.DefinitionSHA256, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: scope.Bindings.PolicySHA256, ClaimSHA256: claimSHA, LedgerSHA256: audit.JournalSHA256, Evidence: evidence}
		cpn := fmt.Sprintf("campaign/checkpoint-%06d.json", i)
		stoppedV9Put(t, files, cpn, cp)
		prevCP = conversationHash(files[cpn])
	}
	var cumulative *windowV8RuntimeSummary
	prevSummary := ""
	attempted, turns, models, jevs := 0, 0, 0, 0
	for i := 1; i <= 56; i++ {
		w := scope.Windows[i-1]
		wd := fmt.Sprintf("records/window-%06d", i)
		a := audits[i-1]
		runtime := runtimes[i-1]
		next := ""
		if i < 56 {
			runtime.BasePermanent = windowV8Amounts{claims[i].ModelPermanent - first.PermanentModel, claims[i].JEVPermanent - first.PermanentJEV}
			runtime.TotalPermanent = windowV8Amounts{claims[i].ModelPermanent, claims[i].JEVPermanent}
			next = conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)])
		}
		currentTurns := 0
		for _, r := range rows[i-1] {
			currentTurns += r.ObservedTurns
		}
		status := "collected_window"
		if i == 56 {
			status = "stopped"
		}
		receipt := windowAcquisitionReceipt{V8: &runtime, Schema: "full-window-acquisition-receipt.v8", EvidenceKind: "recorded_run", Status: status, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, WindowID: w.ID, WindowIndex: i, PlannedCampaignPairs: 3660, PlannedCampaignArms: 7320, PlannedCampaignTurns: 11772, PlannedCurrentPairs: w.LastOrdinal - w.FirstOrdinal + 1, PlannedCurrentArms: len(rows[i-1]), PlannedCurrentTurns: currentTurns, AttemptedArms: len(rows[i-1]), FinishedArms: len(rows[i-1]), ObservedTurns: currentTurns, QualityNegativeArms: len(rows[i-1]), Rows: rows[i-1], Cleanup: i < 56, EnvironmentCleanup: true, Observer: true, Advanced: i < 56, NextClaimSHA256: next, LedgerSHA256: a.JournalSHA256, LedgerCompletionProof: "unavailable_from_journal", Accounting: a.State, AccountingSource: "live_owner_snapshot", OperationLimitSeconds: 300, PayloadManifestSHA256: conversationHash(files[wd+"/payload-manifest.json"]), PayloadAuditOnlyCompletion: "unavailable_from_payload_capture", PayloadPackets: a.State.ModelAttempts + a.State.JEVAttempts, PayloadVerified: i < 56, PayloadStatus: "verified_ledger_linkage", DurationMS: 100}
		if i == 56 {
			receipt.FirstFailureStage = "campaign_advance"
			receipt.PayloadStatus = "audit_only"
			receipt.DurationMS = 300023.451
		}
		stoppedV9Put(t, files, wd+"/receipt.json", receipt)
		attempted += len(rows[i-1])
		turns += currentTurns
		models += a.State.ModelAttempts
		jevs += a.State.JEVAttempts
		cumulative = mergeWindowV8Runtime(cumulative, &runtime)
		actualStatus := FullWindowActualStatus{V8: cumulative, EvidenceKind: "recorded_run", Closed: i == 56, NextWindowIndex: i + 1, LastReceiptSHA256: prevSummary, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: attempted, FinishedArms: attempted, UnattemptedArms: 7320 - attempted, ObservedTurns: turns, GlobalUnrunTurns: 11772 - turns, ModelAttempts: models, JEVAttempts: jevs}
		if i == 56 {
			actualStatus.NextWindowIndex = 56
		}
		summary := FullWindowActualReceipt{FullWindowActualStatus: actualStatus, WindowIndex: i, ReportSHA256: conversationHash(files[wd+"/receipt.json"]), PayloadManifestSHA256: receipt.PayloadManifestSHA256, LedgerSHA256: a.JournalSHA256, FirstUnrunOrdinal: w.LastOrdinal + 1, FirstUnrunArm: scope.Slots[w.LastOrdinal].ArmOrder[0], FutureUnrunFromOrdinal: w.LastOrdinal + 1, ReportStatus: status}
		sn := fmt.Sprintf("records/window-%06d-summary.json", i)
		stoppedV9Put(t, files, sn, summary)
		prevSummary = conversationHash(files[sn])
		if i == 56 {
			actualStatus.LastReceiptSHA256 = prevSummary
			stoppedV9Put(t, files, "records/terminal.json", actualStatus)
		}
	}
	stoppedV9Put(t, files, "actual-owner-exit.json", map[string]any{"exit_code": 1, "ended_epoch": 1793423715.8, "relaunch_allowed": false, "supervision_failure": "ValueError", "supervision_signal": nil, "supervisor_exit_code": 1})
	return root, m, files
}
func TestWindowStoppedV9BundleKeeps55NormalAnd56Stopped(t *testing.T) {
	root, m, f := stoppedV9BundleFixture(t)
	b, err := readWindowStoppedV9Bundle(root, m, expectedStoppedV8WindowBaseline(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.ClaimSHA256) != 102 || b.PermanentModel != 66472061 || b.PermanentJEV != 631114 || b.ActualModelCalls != 5137 || b.ActualJEVCalls != 1308 {
		t.Fatal("stopped102 accounting", b)
	}
}
func TestWindowStoppedV9BundleRejectsForgedHistory(t *testing.T) {
	root, m, original := stoppedV9BundleFixture(t)
	changes := map[string]func(map[string][]byte){
		"missingStartup": func(f map[string][]byte) { delete(f, "records/startup.json") },
		"promotedStartup": func(f map[string][]byte) {
			var s FullWindowActualStatus
			json.Unmarshal(f["records/startup.json"], &s)
			s.AcquisitionDone = true
			stoppedV9Put(t, f, "records/startup.json", s)
		},
		"claim57": func(f map[string][]byte) { f["campaign/claim-000057.json"] = []byte(`{}`) }, "missingCP56": func(f map[string][]byte) { delete(f, "campaign/checkpoint-000056.json") }, "nativebody": func(f map[string][]byte) {
			f["records/window-000001/native-payloads/response-000001.body"] = []byte(`{}`)
		}, "extraNative": func(f map[string][]byte) { f["records/window-000056/native-payloads/foreign.body"] = []byte(`{}`) }, "missingRecord": func(f map[string][]byte) { delete(f, "records/window-000001/arm-000001-model.json") },
		"releasedHold": func(f map[string][]byte) {
			var c windowCampaignClaim
			json.Unmarshal(f["campaign/claim-000056.json"], &c)
			c.ModelPermanent--
			stoppedV9Put(t, f, "campaign/claim-000056.json", c)
		},
		"promotedStopped": func(f map[string][]byte) {
			var r windowAcquisitionReceipt
			json.Unmarshal(f["records/window-000056/receipt.json"], &r)
			r.Advanced = true
			r.Cleanup = true
			r.PayloadVerified = true
			stoppedV9Put(t, f, "records/window-000056/receipt.json", r)
		},
		"normalNotAdvanced": func(f map[string][]byte) {
			var r windowAcquisitionReceipt
			json.Unmarshal(f["records/window-000055/receipt.json"], &r)
			r.Advanced = false
			stoppedV9Put(t, f, "records/window-000055/receipt.json", r)
		},
		"legacyCheckpoint": func(f map[string][]byte) {
			var c windowCampaignCheckpoint
			json.Unmarshal(f["campaign/checkpoint-000056.json"], &c)
			c.Schema = "full-window-campaign-owned-checkpoint.v1"
			stoppedV9Put(t, f, "campaign/checkpoint-000056.json", c)
		},
		"terminalOpen": func(f map[string][]byte) {
			var v FullWindowActualStatus
			json.Unmarshal(f["records/terminal.json"], &v)
			v.Closed = false
			stoppedV9Put(t, f, "records/terminal.json", v)
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			f := map[string][]byte{}
			for k, v := range original {
				f[k] = v
			}
			change(f)
			if _, err := readWindowStoppedV9Bundle(root, m, expectedStoppedV8WindowBaseline(), f); err == nil {
				t.Fatal("forged stopped history accepted")
			}
		})
	}
}
func TestWindowStoppedV9RootsAreCopiedData(t *testing.T) {
	s := windowStoppedV9Baseline{originalRoot: "/r5"}
	a := s.windowStoppedPredecessorRootsV10()
	if len(a) != 9 || a[8] != "/r5" {
		t.Fatal(a)
	}
	a[8] = "changed"
	if reflect.DeepEqual(a, s.windowStoppedPredecessorRootsV10()) {
		t.Fatal("roots aliased")
	}
}

// Rebind the receipt, summary and terminal so only the removed negative count
// distinguishes this forged data from a coherent stopped56 chain.
func TestWindowStoppedV9CoherentNegativeSuppressionRejected(t *testing.T) {
	root, m, files := stoppedV9BundleFixture(t)
	var r windowAcquisitionReceipt
	json.Unmarshal(files["records/window-000056/receipt.json"], &r)
	r.QualityNegativeArms--
	stoppedV9Put(t, files, "records/window-000056/receipt.json", r)
	var s FullWindowActualReceipt
	json.Unmarshal(files["records/window-000056-summary.json"], &s)
	s.ReportSHA256 = conversationHash(files["records/window-000056/receipt.json"])
	stoppedV9Put(t, files, "records/window-000056-summary.json", s)
	var terminal FullWindowActualStatus
	json.Unmarshal(files["records/terminal.json"], &terminal)
	terminal.LastReceiptSHA256 = conversationHash(files["records/window-000056-summary.json"])
	stoppedV9Put(t, files, "records/terminal.json", terminal)
	if _, err := readWindowStoppedV9Bundle(root, m, expectedStoppedV8WindowBaseline(), files); err == nil {
		t.Fatal("coherently suppressed failed count accepted")
	}
}

func stoppedV9InventoryFixture(t *testing.T) (string, windowStoppedV8Inventory) {
	t.Helper()
	root := stoppedEvidenceRoot(t)
	if err := os.Mkdir(filepath.Join(root, "records"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("original\n")
	path := filepath.Join(root, "records", "raw.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return root, windowStoppedV8Inventory{Root: root, Files: map[string]windowStoppedV8InventoryEntry{"records/raw.json": {SHA256: conversationHash(raw), Bytes: int64(len(raw))}}}
}
func TestWindowStoppedV9InventoryFreshEndAndNamespace(t *testing.T) {
	for _, mode := range []string{"unchanged", "body", "extra", "symlink", "hardlink", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			root, inv := stoppedV9InventoryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, _ := newHistoryReadPass(ctx)
			if err := p.MarkImmutableRoot(root); err != nil {
				t.Fatal(err)
			}
			files, _, err := readWindowStoppedV9InventoryWithPass(p, root, inv)
			if err != nil || string(files["records/raw.json"]) != "original\n" {
				t.Fatal("fresh inventory", err)
			}
			path := filepath.Join(root, "records/raw.json")
			switch mode {
			case "body":
				err = os.WriteFile(path, []byte("tampered\n"), 0600)
			case "extra":
				err = os.WriteFile(filepath.Join(root, "records/extra.json"), nil, 0600)
			case "symlink":
				err = os.Remove(path)
				if err == nil {
					err = os.Symlink(filepath.Join(root, "outside"), path)
				}
			case "hardlink":
				err = os.Link(path, filepath.Join(root, "other-link"))
			case "canceled":
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			err = p.Finish()
			if mode == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
				stats := p.Stats()
				if stats.FirstPhysical == 0 || stats.EndPhysical == 0 {
					t.Fatal("missing physical endpoints", stats)
				}
			} else if err == nil {
				t.Fatal("mutation/cancel survived fresh End")
			}
		})
	}
}
func TestWindowStoppedV9SourceArchiveFreshAndCancellation(t *testing.T) {
	for _, mode := range []string{"unchanged", "changed", "traversal", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			raw := []byte("source\n")
			path := filepath.Join(root, "a.go")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			sources := map[string]string{"a.go": conversationHash(raw)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, _ := newHistoryReadPass(ctx)
			if mode == "traversal" {
				sources["../escape"] = conversationHash(raw)
			}
			if mode == "canceled" {
				cancel()
			}
			err := readWindowStoppedV9SourcesWithPass(p, root, sources)
			if mode == "traversal" || mode == "canceled" {
				if err == nil {
					t.Fatal("unsafe sources accepted")
				}
				_ = p.Abort()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "changed" {
				if err := os.WriteFile(path, []byte("changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err = p.Finish()
			if mode == "changed" && err == nil {
				t.Fatal("source mutation survived Finish")
			}
			if mode == "unchanged" && err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Keep all descriptor/body hashes coherent; the request/response usage decoder
// must still reject a cache-vector lie, rather than relying on stale hashes.
func TestWindowStoppedV9NativeCoherentCacheLieRejected(t *testing.T) {
	_, _, files := stoppedV9BundleFixture(t)
	dir := "records/window-000001/native-payloads"
	var p windowPayloadManifest
	json.Unmarshal(files["records/window-000001/payload-manifest.json"], &p)
	ledgerName := "campaign/ledger-000001/accounting.jsonl"
	if _, err := readWindowStoppedV9Native(dir, p, files[ledgerName], files, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	packet := p.Packets[0]
	body := []byte(`{"model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":0}}`)
	files[dir+"/"+packet.Response.Body.File] = body
	packet.Response.Body.Bytes = len(body)
	packet.Response.Body.FullSHA256 = conversationHash(body)
	p.Packets[0] = packet
	response := packet
	response.Request = windowPayloadBody{}
	response.RequestDescriptorFile = ""
	stoppedV9Put(t, files, dir+"/"+response.Response.DescriptorFile, response)
	// Rebuild the canonical journal's chain after changing settle1's responseSHA.
	lines := strings.Split(strings.TrimSuffix(string(files[ledgerName]), "\n"), "\n")
	previous := ""
	var raw []byte
	for _, line := range lines {
		var e windowAccountingEvent
		json.Unmarshal([]byte(line), &e)
		e.PreviousSHA256 = previous
		if e.Kind == "settle" && e.Sequence == 1 {
			e.ResponseSHA256 = conversationHash(body)
		}
		b, _ := json.Marshal(e)
		previous = conversationHash(b)
		raw = append(raw, b...)
		raw = append(raw, '\n')
	}
	if _, err := readWindowStoppedV9Native(dir, p, raw, files, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "usage/cache") {
		t.Fatal("coherent cache lie not rejected by fresh usage", err)
	}
}
func TestWindowStoppedV9SourceJSONCannotGrantAuthority(t *testing.T) {
	raw := []byte(`{"schema":"full1220-v9-stopped-source-confirmation.v1","classification":"external_persisted_source_replay_DATA_only","noActualAuthority":true,"normal56WindowProof":false,"scoresHold":true,"new_opaque_reconstructed_from_JSON":false}`)
	root := stoppedEvidenceRoot(t)
	p, _ := newHistoryReadPass(context.Background())
	defer p.Abort()
	if err := readWindowStoppedV9SourceData(p, raw); err == nil {
		t.Fatal("JSON impersonated pinned source confirmation")
	}
	names, _ := os.ReadDir(root)
	if len(names) != 0 {
		t.Fatal("untrusted source JSON had effects")
	}
}

func TestWindowStoppedV9PredecessorErrorPreservesExactCause(t *testing.T) {
	previous := windowStoppedV8Baseline{base: expectedStoppedV8WindowBaseline(), originalRoot: "/synthetic/old46"}
	childCause := fmt.Errorf("synthetic nested reader failure")
	passCause := fmt.Errorf("synthetic history logical admission limit")
	for _, mode := range []string{"same", "changed", "child", "retained_pass", "both"} {
		t.Run(mode, func(t *testing.T) {
			p, _ := newHistoryReadPass(context.Background())
			defer p.Abort()
			fresh := previous
			var nested error
			switch mode {
			case "changed":
				fresh.originalRoot = "/synthetic/other46"
			case "child":
				nested = childCause
			case "retained_pass":
				_ = p.fail(passCause)
				nested = fmt.Errorf("generic old loader rejection")
			case "both":
				_ = p.fail(passCause)
				nested = childCause
			}
			err := checkWindowStoppedV9Predecessor(p, previous, fresh, nested)
			if mode == "same" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("failed/changed predecessor accepted")
			}
			if (mode == "child" || mode == "both") && !errors.Is(err, childCause) {
				t.Fatal("nested cause erased", err)
			}
			if (mode == "retained_pass" || mode == "both") && !errors.Is(err, passCause) {
				t.Fatal("owned pass cause erased", err)
			}
		})
	}
}

func stoppedV9PreviousForSnapshot() windowStoppedV8Baseline {
	p := windowStoppedV8Baseline{base: expectedStoppedV8WindowBaseline(), originalRoot: "/synthetic/old46", evidencePath: "/synthetic/old46/descriptor.json", evidenceSHA256: windowV9BaselineSHA}
	// Every ancestor owns a separate map. The synthetic strings below carry no
	// genuine proof; they exercise the typed value's complete mutable surface.
	p.previous.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v7": "7"}}
	p.previous.previous.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v6": "6"}}
	p.previous.previous.previous.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v5": "5"}}
	p.previous.previous.previous.previous.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v4": "4"}}
	p.previous.previous.previous.previous.previous.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v3": "3"}}
	p.previous.previous.previous.previous.previous.previous.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v2": "2"}}
	p.previous.previous.previous.previous.previous.previous.first.base = fullWindowBaseline{ClaimSHA256: map[string]string{"v1": "1"}}
	return p
}
func stoppedV9PreviousMaps(p *windowStoppedV8Baseline) []*fullWindowBaseline {
	return []*fullWindowBaseline{&p.base, &p.previous.base, &p.previous.previous.base, &p.previous.previous.previous.base, &p.previous.previous.previous.previous.base, &p.previous.previous.previous.previous.previous.base, &p.previous.previous.previous.previous.previous.previous.base, &p.previous.previous.previous.previous.previous.previous.first.base}
}
func TestWindowStoppedV9FrozenPredecessorDetectsEveryAncestorMutation(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprintf("ancestor%d", i+1), func(t *testing.T) {
			previous := stoppedV9PreviousForSnapshot()
			frozen := freezeWindowStoppedV9Predecessor(previous)
			maps := stoppedV9PreviousMaps(&previous)
			maps[i].ClaimSHA256["forged"] = "changed"
			p, _ := newHistoryReadPass(context.Background())
			defer p.Abort()
			if err := checkWindowStoppedV9Predecessor(p, frozen, previous, nil); err == nil {
				t.Fatal("mutation aliased frozen predecessor")
			}
		})
	}
	for _, mut := range []func(*windowStoppedV8Baseline){func(p *windowStoppedV8Baseline) { p.base.ActualModelCalls++ }, func(p *windowStoppedV8Baseline) { p.evidencePath += ".copy" }, func(p *windowStoppedV8Baseline) { p.previous.previous.originalRoot += "changed" }} {
		previous := stoppedV9PreviousForSnapshot()
		frozen := freezeWindowStoppedV9Predecessor(previous)
		mut(&previous)
		p, _ := newHistoryReadPass(context.Background())
		if checkWindowStoppedV9Predecessor(p, frozen, previous, nil) == nil {
			t.Fatal("scalar identity changed")
		}
		_ = p.Abort()
	}
}
func TestWindowStoppedV9NoSecondRecursiveReplayAtLogicalLimit(t *testing.T) {
	for _, mode := range []string{"unchanged", "body", "namespace", "lock", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			raw := []byte("original ancestor bytes\n")
			leaf := filepath.Join(root, "raw.json")
			lock := filepath.Join(root, ".lock")
			if err := os.WriteFile(leaf, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, nil, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, _ := newHistoryReadPass(ctx)
			defer p.Abort()
			if err := p.MarkImmutableRoot(root); err != nil {
				t.Fatal(err)
			}
			if _, err := historyStoppedV7File(p, leaf, conversationHash(raw), 65536); err != nil {
				t.Fatal(err)
			}
			if err := historyStoppedDir(p, root, map[string]bool{"raw.json": false, ".lock": false}); err != nil {
				t.Fatal(err)
			}
			if err := p.WithStoppedLock(8, lock, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			previous := stoppedV9PreviousForSnapshot()
			frozen := freezeWindowStoppedV9Predecessor(previous)
			p.mu.Lock()
			p.stats.Logical = historyPassLogicalLimit
			p.mu.Unlock()
			switch mode {
			case "body":
				if err := os.WriteFile(leaf, []byte("changed ancestor bytes!\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "namespace":
				if err := os.WriteFile(filepath.Join(root, "claim-000057.json"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "lock":
				if err := os.Rename(lock, lock+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(lock, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			}
			err := recheckWindowStoppedV9Predecessor(p, frozen, previous)
			if mode == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation was not preserved", err)
				}
				return
			}
			if err != nil {
				t.Fatal("second pure recursive load consumed logical admission", err)
			}
			if got := p.Stats().Logical; got != 1048576 {
				t.Fatal("final semantic check changed fixed admission counter", got)
			}
			err = p.Finish()
			if mode == "unchanged" {
				if err != nil {
					t.Fatal("fresh End failed", err)
				}
				if p.Stats().EndPhysical != 1 {
					t.Fatal("ancestor was not physically re-read", p.Stats())
				}
			} else if err == nil {
				t.Fatal("fresh End missed original mutation", mode)
			}
		})
	}
}
