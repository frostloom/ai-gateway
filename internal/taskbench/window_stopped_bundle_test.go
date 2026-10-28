package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func stoppedBundleFixture(t *testing.T) (fullWindowBaseline, windowStoppedBundle) {
	t.Helper()
	base := fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29061120, PermanentJEV: 223104,
		RemainingModel: 70938880, RemainingJEV: 776896, ActualModelCalls: 143, ActualJEVCalls: 37, ClaimSHA256: fullWindowCanonicalClaims()}
	a, packet, response := stoppedLiabilityFixture(t)
	a.Contract.JEV = windowProviderLimits{Model: "jev-1.13.0", MaxAttempts: 30, MaxRequestBytes: 4096, MaxInput: 64000, MaxOutput: 1000000, InputRate: 42000}
	request := []byte(windowProtocolModelBody)
	packet.Identity.BodySHA256, packet.Identity.Bytes = conversationHash(request), len(request)
	packet.Identity.CaseKey = "adv-001/answer_complete"
	packet.Request.FullSHA256, packet.Request.Bytes = packet.Identity.BodySHA256, len(request)
	a.State.Pending.Request = packet.Identity
	w := fullWindowDeclaration{ID: "window-001", FirstOrdinal: 1, LastOrdinal: 28, MaxModelCalls: 300, MaxJEVCalls: 30, fullWindowCaps: fullWindowCaps{414586, 4079}}
	claim := windowCampaignClaim{Schema: "full-window-campaign-claim.v1", Root: "/old/campaign", Index: 1,
		ScopeSHA256: a.Contract.ScopeSHA256, ExecutionSHA256: strings.Repeat("f", 64), DefinitionSHA256: strings.Repeat("1", 64), ManifestSHA256: strings.Repeat("2", 64), SelectionSHA256: strings.Repeat("3", 64), ProfileSHA256: strings.Repeat("4", 64), ScheduleSHA256: a.Contract.ScheduleSHA256, PolicySHA256: a.Contract.PolicySHA256,
		Window: w, HistoricalClaims: fullWindowCanonicalClaims(), ModelPermanent: 29475706, JEVPermanent: 227183}
	marshal := func(v any) []byte {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	claimRaw := marshal(claim)
	a.Contract.ClaimSHA256 = conversationHash(claimRaw)
	packet.ClaimSHA256 = a.Contract.ClaimSHA256
	// Generate an actual canonical stopped journal using the existing primitive.
	dir := filepath.Join(t.TempDir(), "ledger")
	l, err := newWindowAccounting(dir, a.Contract)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := l.Begin(packet.Identity)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Fail(ticket, "protocol_failure")
	_ = l.Close()
	a, err = readWindowAccounting(dir)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(dir, "accounting.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := windowActualManifest{Schema: "full-window-actual-ready.v1", EvidenceKind: "actual", ArchiveCommit: "c1c446e991eedcb5c5e1a544e5e762981d8d2571", ClaimsDir: "/claims", SessionRoot: "/old/records", CampaignRoot: claim.Root, KnowledgeModel: "off", KnowledgeJEV: "off", ScopeSHA256: claim.ScopeSHA256, ExecutionSHA256: claim.ExecutionSHA256}
	manifest.Scope = fullWindowScope{Schema: "full-window-scope.v1", DefinitionSHA256: claim.DefinitionSHA256, ManifestSHA256: claim.ManifestSHA256, SelectionSHA256: claim.SelectionSHA256, ProfileSHA256: claim.ProfileSHA256, ScheduleSHA256: claim.ScheduleSHA256,
		Bindings: fullWindowBindings{PolicySHA256: claim.PolicySHA256}, Windows: []fullWindowDeclaration{w}}
	convert := func(p windowProviderLimits) windowCampaignProviderBounds {
		return windowCampaignProviderBounds{Model: p.Model, MaxRequestBytes: p.MaxRequestBytes, MaxInput: p.MaxInput, MaxOutput: p.MaxOutput, InputRate: p.InputRate, OutputRate: p.OutputRate}
	}
	manifest.Limits = windowCampaignLimits{Model: convert(a.Contract.Model), JEV: convert(a.Contract.JEV)}
	payload := windowPayloadManifest{Schema: "full-window-payload.v1", EvidenceKind: "recorded_run", CompletionProof: "unavailable_from_payload_capture", Contract: a.Contract, Packets: []windowPayloadPacket{packet}}
	payloadRaw := marshal(payload)
	recording := []byte(`{"synthetic_recording":true}`)
	report := windowAcquisitionReceipt{Schema: "full-window-acquisition-receipt.v1", EvidenceKind: "recorded_run", Status: "stopped", ScopeSHA256: claim.ScopeSHA256, ExecutionSHA256: claim.ExecutionSHA256, WindowID: w.ID, WindowIndex: 1,
		PlannedCampaignPairs: 3660, PlannedCampaignArms: 7320, PlannedCampaignTurns: 11772, PlannedCurrentPairs: 28, PlannedCurrentArms: 56, PlannedCurrentTurns: 60,
		AttemptedArms: 1, UnrunArms: 55, ObservedTurns: 1, GlobalUnrunTurns: 59, FirstFailureStage: "arm_proof", FirstFailureOrdinal: 1, FirstFailureArm: "model", EnvironmentCleanup: true,
		LedgerSHA256: conversationHash(journal), LedgerCompletionProof: "unavailable_from_journal", Accounting: a.State, AccountingSource: "live_owner_snapshot", PayloadManifestSHA256: conversationHash(payloadRaw), PayloadAuditOnlyCompletion: "unavailable_from_payload_capture", PayloadPackets: 1, PayloadStatus: "audit_only", OperationLimitSeconds: 300}
	report.Rows = make([]windowAcquisitionRow, 56)
	report.Rows[0] = windowAcquisitionRow{Ordinal: 1, Pass: 1, DefinedTurns: 1, ObservedTurns: 1, CaseKey: packet.Identity.CaseKey, Arm: "model", RecordingSHA256: conversationHash(recording), RecordingFile: "arm-000001-model.json", Status: "failed", Disposition: "halt", Attempted: true}
	reportRaw := marshal(report)
	status := FullWindowActualStatus{EvidenceKind: "recorded_run", Closed: true, NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: 1, FailedAttemptedArms: 1, UnattemptedArms: 7319, ObservedTurns: 1, GlobalUnrunTurns: 11771, ModelAttempts: 1}
	summary := FullWindowActualReceipt{FullWindowActualStatus: status, WindowIndex: 1, ReportSHA256: conversationHash(reportRaw), PayloadManifestSHA256: conversationHash(payloadRaw), LedgerSHA256: conversationHash(journal), FirstUnrunOrdinal: 1, FirstUnrunArm: "jev_selective", FutureUnrunFromOrdinal: 29, ReportStatus: "stopped"}
	summaryRaw := marshal(summary)
	status.LastReceiptSHA256 = conversationHash(summaryRaw)
	return base, windowStoppedBundle{manifest: manifest, claim: claim, terminal: status, summary: summary, report: report, payload: payload, audit: a,
		raw: map[string][]byte{"actual-manifest.json": marshal(manifest), "campaign/claim-000001.json": claimRaw, "campaign/ledger-000001/accounting.jsonl": journal, "records/terminal.json": marshal(status), "records/window-000001-summary.json": summaryRaw,
			"records/window-000001/receipt.json": reportRaw, "records/window-000001/payload-manifest.json": payloadRaw, "records/window-000001/arm-000001-model.json": recording,
			"records/window-000001/native-payloads/request-000001.body": request, "records/window-000001/native-payloads/response-000001.body": response}}
}

func TestWindowStoppedBundleDerivesWholePermanentReservation(t *testing.T) {
	base, b := stoppedBundleFixture(t)
	before, _ := json.Marshal(b.audit.State)
	next, liability, err := verifyWindowStoppedBundle(base, b)
	if err != nil {
		t.Fatal(err)
	}
	if next.PermanentModel != 29475706 || next.PermanentJEV != 227183 || next.RemainingModel != 70524294 || next.RemainingJEV != 772817 || next.ActualModelCalls != 144 || next.ActualJEVCalls != 37 || len(next.ClaimSHA256) != 10 || liability != 3832 {
		t.Fatal(next, liability)
	}
	if len(base.ClaimSHA256) != 9 {
		t.Fatal("original baseline map changed")
	}
	after, _ := json.Marshal(b.audit.State)
	if !reflect.DeepEqual(before, after) || b.report.Cleanup || b.report.Observer {
		t.Fatal("original negative state rewritten")
	}
}

func TestWindowStoppedBundleRejectsBudgetOrStopRewrite(t *testing.T) {
	mutations := map[string]func(*fullWindowBaseline, *windowStoppedBundle){
		"double count":             func(base *fullWindowBaseline, _ *windowStoppedBundle) { base.PermanentModel += 414586 },
		"discount old reservation": func(base *fullWindowBaseline, _ *windowStoppedBundle) { base.RemainingModel++ },
		"claim known only":         func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.claim.ModelPermanent = 29061120 + 3832 },
		"claim new scope":          func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.claim.ScopeSHA256 = strings.Repeat("9", 64) },
		"missing predecessor": func(_ *fullWindowBaseline, b *windowStoppedBundle) {
			delete(b.claim.HistoricalClaims, V5ReceiverRepeatedID)
		},
		"claim two windows":   func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.claim.Index = 2 },
		"not terminal":        func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.terminal.Closed = false },
		"claimed complete":    func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.terminal.AcquisitionDone = true },
		"reclassify success":  func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.report.Status = "completed" },
		"cleanup rewritten":   func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.report.Cleanup = true },
		"observer rewritten":  func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.report.Observer = true },
		"settlement invented": func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.report.Accounting.ModelKnown = 3832 },
		"missing packet":      func(_ *fullWindowBaseline, b *windowStoppedBundle) { b.payload.Packets = nil },
		"missing raw receipt": func(_ *fullWindowBaseline, b *windowStoppedBundle) {
			delete(b.raw, "records/window-000001/receipt.json")
		},
		"wrong response": func(_ *fullWindowBaseline, b *windowStoppedBundle) {
			b.raw["records/window-000001/native-payloads/response-000001.body"] = []byte(`{}`)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			base, b := stoppedBundleFixture(t)
			mutate(&base, &b)
			if _, _, err := verifyWindowStoppedBundle(base, b); err == nil {
				t.Fatal("rewritten predecessor accepted")
			}
		})
	}
}

func TestWindowStoppedJournalBytesUseOriginalReplayRules(t *testing.T) {
	_, b := stoppedBundleFixture(t)
	raw := b.raw["campaign/ledger-000001/accounting.jsonl"]
	a, err := readWindowAccountingBytes(raw)
	if err != nil || !reflect.DeepEqual(a, b.audit) {
		t.Fatal("byte replay differs from original file replay", err)
	}
	for _, bad := range [][]byte{nil, raw[:len(raw)-1], append(append([]byte{}, raw...), []byte("{}\n")...)} {
		if _, err = readWindowAccountingBytes(bad); err == nil {
			t.Fatal("invalid/truncated journal accepted")
		}
	}
}
