package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func stoppedV8BundleFixture(t *testing.T) (string, windowActualManifest, map[string][]byte) {
	t.Helper()
	root := stoppedEvidenceRoot(t)
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV8(s, p, 28, windowV7TestCaps(), b, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{Scope: scope, ScopeSHA256: strings.Repeat("a", 64), ExecutionSHA256: strings.Repeat("b", 64), CampaignRoot: filepath.Join(root, "campaign"), Limits: windowV4ExpectedLimits()}
	files := map[string][]byte{}
	put := func(n string, v any) {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		files[n] = raw
	}
	previous := expectedStoppedV7WindowBaseline()
	model, jev := previous.PermanentModel, previous.PermanentJEV
	claim := func(i int, prev, cp string) {
		w := scope.Windows[i-1]
		model += w.ModelCap
		jev += w.JEVCap
		put(fmt.Sprintf("campaign/claim-%06d.json", i), windowCampaignClaim{Schema: "full-window-campaign-claim.v8", BaselineEvidenceSHA256: windowV8BaselineSHA, BudgetApprovalSHA256: strings.Repeat("c", 64), ParentModel: 140000000, ParentJEV: 1100000, Root: m.CampaignRoot, Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: scope.DefinitionSHA256, ManifestSHA256: scope.ManifestSHA256, SelectionSHA256: scope.SelectionSHA256, ProfileSHA256: scope.ProfileSHA256, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: scope.Bindings.PolicySHA256, PreviousClaimSHA256: prev, PreviousCheckpointSHA256: cp, Window: w, HistoricalClaims: previous.ClaimSHA256, ModelPermanent: model, JEVPermanent: jev})
	}
	claim(1, "", "")
	c1 := conversationHash(files["campaign/claim-000001.json"])
	contract := contractWindowCampaign(scope, m.ScopeSHA256, scope.Windows[0], m.Limits)
	contract.ClaimSHA256 = c1
	dir := filepath.Join(root, "fixture-ledger")
	l, err := newWindowAccounting(dir, contract)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 96; i++ {
		provider, mod, arm := "model", contract.Model.Model, "model"
		if i >= 68 {
			provider, mod, arm = "jev", contract.JEV.Model, "jev_selective"
		}
		ticket, e := l.Begin(windowRequestIdentity{Provider: provider, Model: mod, CaseKey: fmt.Sprintf("balance-%03d/answer_complete", i+1), Pass: 1, Arm: arm, Turn: 1, Attempt: 1, NativeAttempt: 1, BodySHA256: strings.Repeat("d", 64), Bytes: 64})
		if e != nil {
			t.Fatal(e)
		}
		u := windowUsage{Input: windowInt(1), Output: windowInt(1)}
		if provider == "model" {
			u.CacheHit = windowInt(0)
			u.CacheMiss = windowInt(1)
		}
		if e = l.Settle(ticket, strings.Repeat("f", 64), &u); e != nil {
			t.Fatal(e)
		}
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "accounting.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	files["campaign/ledger-000001/accounting.jsonl"] = raw
	audit, err := readWindowAccountingBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: 68, LogicalJEVCalls: 28, NativeModelIntents: 68, NativeJEVIntents: 28, NativeSettlements: 96, Known: windowV8Amounts{audit.State.ModelKnown, audit.State.JEVKnown}, HistoricalPermanent: windowV8Amounts{previous.PermanentModel, previous.PermanentJEV}, BasePermanent: windowV8Amounts{scope.Windows[0].ModelCap, scope.Windows[0].JEVCap}, TotalPermanent: windowV8Amounts{model, jev}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
	cp := windowCampaignCheckpoint{V8: &runtime, Schema: "full-window-campaign-owned-checkpoint.v8", Index: 1, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: scope.DefinitionSHA256, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: scope.Bindings.PolicySHA256, ClaimSHA256: c1, LedgerSHA256: conversationHash(raw), Evidence: []windowCampaignArmEvidence{}}
	put("campaign/checkpoint-000001.json", cp)
	claim(2, c1, conversationHash(files["campaign/checkpoint-000001.json"]))
	runtime.BasePermanent = windowV8Amounts{model - previous.PermanentModel, jev - previous.PermanentJEV}
	runtime.TotalPermanent = windowV8Amounts{model, jev}
	// Remarshal checkpoint before mutation: its runtime retains claim1-only totals.
	payload := windowPayloadManifest{Contract: contract, Packets: []windowPayloadPacket{}}
	put("records/window-000001/payload-manifest.json", payload)
	receipt := windowAcquisitionReceipt{Rows: []windowAcquisitionRow{}, V8: &runtime, Schema: "full-window-acquisition-receipt.v8", EvidenceKind: "recorded_run", Status: "collected_window", ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, WindowID: scope.Windows[0].ID, WindowIndex: 1, AttemptedArms: 56, FinishedArms: 56, ObservedTurns: 60, Advanced: true, Cleanup: true, EnvironmentCleanup: true, Observer: true, NextClaimSHA256: conversationHash(files["campaign/claim-000002.json"]), Accounting: audit.State, AccountingSource: "live_owner_snapshot", LedgerCompletionProof: "unavailable_from_journal", LedgerSHA256: conversationHash(raw), PayloadManifestSHA256: conversationHash(files["records/window-000001/payload-manifest.json"]), PayloadVerified: true, PayloadStatus: "verified_ledger_linkage", OperationLimitSeconds: 300, DurationMS: 331970.619}
	put("records/window-000001/receipt.json", receipt)
	status := FullWindowActualStatus{V8: &runtime, EvidenceKind: "recorded_run", Closed: true, NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: 56, FinishedArms: 56, UnattemptedArms: 7264, ObservedTurns: 60, GlobalUnrunTurns: 11712, ModelAttempts: 68, JEVAttempts: 28}
	summary := FullWindowActualReceipt{FullWindowActualStatus: status, WindowIndex: 1, ReportSHA256: conversationHash(files["records/window-000001/receipt.json"]), LedgerSHA256: conversationHash(raw), PayloadManifestSHA256: receipt.PayloadManifestSHA256, ReportStatus: "collected_window", FirstUnrunOrdinal: 29, FirstUnrunArm: "model", FutureUnrunFromOrdinal: 29}
	put("records/window-000001-summary.json", summary)
	status.LastReceiptSHA256 = conversationHash(files["records/window-000001-summary.json"])
	put("records/terminal.json", status)
	put("actual-owner-exit.json", map[string]any{"exit_code": 1, "ended_epoch": 1793328527.7, "relaunch_allowed": false})
	return root, m, files
}

func TestWindowStoppedV8RetainsInnerCollectedOuterStoppedClaim2(t *testing.T) {
	root, m, files := stoppedV8BundleFixture(t)
	got, err := readWindowStoppedV8Bundle(root, m, expectedStoppedV7WindowBaseline(), files)
	if err != nil {
		t.Fatalf("stopped46 rejected: %v", err)
	}
	if len(got.ClaimSHA256) != 46 || got.PermanentModel != 52654504 || got.PermanentJEV != 408701 || got.ActualModelCalls != 3557 || got.ActualJEVCalls != 918 {
		t.Fatalf("baseline46 lost permanent liability: %+v", got)
	}
	if !reflect.DeepEqual(got.ClaimSHA256["full1220-v8-20261030/window-000002"], conversationHash(files["campaign/claim-000002.json"])) {
		t.Fatal("claim2 omitted")
	}
}
func TestWindowStoppedV8RejectsForgedStopAndLaterPaths(t *testing.T) {
	root, m, original := stoppedV8BundleFixture(t)
	changes := map[string]func(map[string][]byte){
		"ledger2":     func(f map[string][]byte) { f["campaign/ledger-000002/accounting.jsonl"] = []byte("x") },
		"claim3":      func(f map[string][]byte) { f["campaign/claim-000003.json"] = []byte("{}") },
		"checkpoint2": func(f map[string][]byte) { f["campaign/checkpoint-000002.json"] = []byte("{}") },
		"terminal": func(f map[string][]byte) {
			var v FullWindowActualStatus
			json.Unmarshal(f["records/terminal.json"], &v)
			v.Closed = false
			f["records/terminal.json"], _ = json.Marshal(v)
		},
		"outercomplete": func(f map[string][]byte) {
			var v FullWindowActualReceipt
			json.Unmarshal(f["records/window-000001-summary.json"], &v)
			v.AcquisitionDone = true
			f["records/window-000001-summary.json"], _ = json.Marshal(v)
		},
		"innerfalse": func(f map[string][]byte) {
			var v windowAcquisitionReceipt
			json.Unmarshal(f["records/window-000001/receipt.json"], &v)
			v.Advanced = false
			f["records/window-000001/receipt.json"], _ = json.Marshal(v)
		},
		"claim2release": func(f map[string][]byte) {
			var v windowCampaignClaim
			json.Unmarshal(f["campaign/claim-000002.json"], &v)
			v.ModelPermanent--
			f["campaign/claim-000002.json"], _ = json.Marshal(v)
		},
		"nativeunknown": func(f map[string][]byte) {
			var v windowAcquisitionReceipt
			json.Unmarshal(f["records/window-000001/receipt.json"], &v)
			v.V8.NativeUnknown = 1
			f["records/window-000001/receipt.json"], _ = json.Marshal(v)
		},
		"ownerrelaunch": func(f map[string][]byte) {
			f["actual-owner-exit.json"] = []byte(`{"exit_code":1,"ended_epoch":1,"relaunch_allowed":true}`)
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			f := map[string][]byte{}
			for k, v := range original {
				f[k] = append([]byte(nil), v...)
			}
			change(f)
			if _, err := readWindowStoppedV8Bundle(root, m, expectedStoppedV7WindowBaseline(), f); err == nil {
				t.Fatal("invalid stop accepted")
			}
		})
	}
}

// Rehash every dependent reference so the rejection is caused by the wrong
// checkpoint schema, not by a stale claim/receipt/terminal hash earlier on.
func TestWindowStoppedV8RejectsCoherentLegacyCheckpointSchema(t *testing.T) {
	root, m, files := stoppedV8BundleFixture(t)
	put := func(name string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = raw
	}
	var cp windowCampaignCheckpoint
	json.Unmarshal(files["campaign/checkpoint-000001.json"], &cp)
	cp.Schema = "full-window-campaign-owned-checkpoint.v1"
	put("campaign/checkpoint-000001.json", cp)
	var claim windowCampaignClaim
	json.Unmarshal(files["campaign/claim-000002.json"], &claim)
	claim.PreviousCheckpointSHA256 = conversationHash(files["campaign/checkpoint-000001.json"])
	put("campaign/claim-000002.json", claim)
	var receipt windowAcquisitionReceipt
	json.Unmarshal(files["records/window-000001/receipt.json"], &receipt)
	receipt.NextClaimSHA256 = conversationHash(files["campaign/claim-000002.json"])
	put("records/window-000001/receipt.json", receipt)
	var summary FullWindowActualReceipt
	json.Unmarshal(files["records/window-000001-summary.json"], &summary)
	summary.ReportSHA256 = conversationHash(files["records/window-000001/receipt.json"])
	put("records/window-000001-summary.json", summary)
	var terminal FullWindowActualStatus
	json.Unmarshal(files["records/terminal.json"], &terminal)
	terminal.LastReceiptSHA256 = conversationHash(files["records/window-000001-summary.json"])
	put("records/terminal.json", terminal)
	if _, err := readWindowStoppedV8Bundle(root, m, expectedStoppedV7WindowBaseline(), files); err == nil {
		t.Fatal("coherently rehashed legacy .v1 checkpoint accepted in stopped V8 bundle")
	}
}
