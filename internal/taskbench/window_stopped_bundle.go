package taskbench

import (
	"fmt"
	"reflect"
)

const windowStoppedClaimID = "full1220-first-actual-20261028"

type windowStoppedEvidence struct {
	Schema              string `json:"schema"`
	OriginalRoot        string `json:"original_root"`
	HistoricalClaimsDir string `json:"historical_claims_dir"`
}

type windowStoppedBaseline struct {
	base              fullWindowBaseline
	evidencePath      string
	evidenceSHA256    string
	originalRoot      string
	historicalDir     string
	liabilityMicroCNY int64
}

type windowStoppedBundle struct {
	manifest windowActualManifest
	claim    windowCampaignClaim
	terminal FullWindowActualStatus
	summary  FullWindowActualReceipt
	report   windowAcquisitionReceipt
	payload  windowPayloadManifest
	audit    windowAccountingAudit
	raw      map[string][]byte
}

// This is content validation, not a new ready token or execution capability.
// The production loader additionally pins every original raw artifact.
func verifyWindowStoppedBundle(base fullWindowBaseline, b windowStoppedBundle) (fullWindowBaseline, int64, error) {
	reject := func() (fullWindowBaseline, int64, error) {
		return fullWindowBaseline{}, 0, fmt.Errorf("stopped predecessor bundle rejected")
	}
	wantBase := fullWindowBaseline{TotalModel: 100000000, TotalJEV: 1000000, PermanentModel: 29061120, PermanentJEV: 223104, RemainingModel: 70938880, RemainingJEV: 776896, ActualModelCalls: 143, ActualJEVCalls: 37, ClaimSHA256: fullWindowCanonicalClaims()}
	if !reflect.DeepEqual(base, wantBase) {
		return reject()
	}
	for _, name := range []string{"actual-manifest.json", "campaign/claim-000001.json", "campaign/ledger-000001/accounting.jsonl", "records/terminal.json", "records/window-000001-summary.json", "records/window-000001/receipt.json", "records/window-000001/payload-manifest.json", "records/window-000001/arm-000001-model.json", "records/window-000001/native-payloads/request-000001.body", "records/window-000001/native-payloads/response-000001.body"} {
		if len(b.raw[name]) == 0 {
			return reject()
		}
	}
	m, c, r, s := b.manifest, b.claim, b.report, b.summary
	if m.Schema != "full-window-actual-ready.v1" || m.EvidenceKind != "actual" || m.ArchiveCommit != "c1c446e991eedcb5c5e1a544e5e762981d8d2571" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || len(m.Scope.Windows) == 0 {
		return reject()
	}
	if c.Schema != "full-window-campaign-claim.v1" || c.Index != 1 || c.PreviousClaimSHA256 != "" || c.PreviousCheckpointSHA256 != "" || c.Root != m.CampaignRoot || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.Window != m.Scope.Windows[0] || !reflect.DeepEqual(c.HistoricalClaims, base.ClaimSHA256) || c.ModelPermanent != base.PermanentModel+c.Window.ModelCap || c.JEVPermanent != base.PermanentJEV+c.Window.JEVCap {
		return reject()
	}
	if c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.Window.ID != "window-001" || c.Window.FirstOrdinal != 1 || c.Window.LastOrdinal != 28 || c.Window.MaxModelCalls != 300 || c.Window.MaxJEVCalls != 30 || c.Window.ModelCap != 414586 || c.Window.JEVCap != 4079 {
		return reject()
	}
	claimSHA := conversationHash(b.raw["campaign/claim-000001.json"])
	wantContract := contractWindowCampaign(m.Scope, m.ScopeSHA256, c.Window, m.Limits)
	wantContract.ClaimSHA256 = claimSHA
	if !reflect.DeepEqual(wantContract, b.audit.Contract) || b.audit.Events != 4 || b.audit.JournalSHA256 != conversationHash(b.raw["campaign/ledger-000001/accounting.jsonl"]) || b.audit.CompletionProof != "unavailable_from_journal" || !reflect.DeepEqual(b.payload.Contract, b.audit.Contract) || b.payload.Schema != "full-window-payload.v1" || b.payload.EvidenceKind != "recorded_run" || b.payload.CompletionProof != "unavailable_from_payload_capture" || len(b.payload.Packets) != 1 {
		return reject()
	}
	packet := b.payload.Packets[0]
	if packet.Identity.CaseKey != "adv-001/answer_complete" || packet.Identity.Pass != 1 || packet.Identity.Arm != "model" || packet.Identity.Turn != 1 || packet.Identity.Attempt != 1 || packet.Identity.BodySHA256 != conversationHash(b.raw["records/window-000001/native-payloads/request-000001.body"]) || validateWindowPOST("model", "POST", "https://api.deepseek.com/anthropic/v1/messages", b.raw["records/window-000001/native-payloads/request-000001.body"], b.audit.Contract.Model, packet.Identity) != nil {
		return reject()
	}
	liability, err := verifyWindowStoppedLiability(b.audit, packet, b.raw["records/window-000001/native-payloads/response-000001.body"])
	if err != nil || liability != 3832 {
		return reject()
	}
	if r.Schema != "full-window-acquisition-receipt.v1" || r.EvidenceKind != "recorded_run" || r.Status != "stopped" || r.ScopeSHA256 != c.ScopeSHA256 || r.ExecutionSHA256 != c.ExecutionSHA256 || r.WindowID != c.Window.ID || r.WindowIndex != 1 || r.AttemptedArms != 1 || r.FinishedArms != 0 || r.UnrunArms != 55 || r.ObservedTurns != 1 || r.QualityNegativeArms != 0 || r.GlobalUnrunTurns != 59 || r.QualityEndArms != 0 || r.QualityEndUnrunTurns != 0 || r.Cleanup || !r.EnvironmentCleanup || r.Observer || r.Advanced || r.PayloadVerified || r.PayloadStatus != "audit_only" || r.PayloadPackets != 1 || r.NextClaimSHA256 != "" || r.AccountingSource != "live_owner_snapshot" || !reflect.DeepEqual(r.Accounting, b.audit.State) || r.LedgerSHA256 != b.audit.JournalSHA256 || r.LedgerCompletionProof != b.audit.CompletionProof || r.PayloadManifestSHA256 != conversationHash(b.raw["records/window-000001/payload-manifest.json"]) || r.PayloadAuditOnlyCompletion != b.payload.CompletionProof || r.FirstFailureStage != "arm_proof" || r.FirstFailureOrdinal != 1 || r.FirstFailureArm != "model" || r.OperationLimitSeconds != 300 || len(r.Rows) != 56 {
		return reject()
	}
	if r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || r.PlannedCurrentPairs != 28 || r.PlannedCurrentArms != 56 || r.PlannedCurrentTurns != 60 {
		return reject()
	}
	row := r.Rows[0]
	if row.Ordinal != 1 || row.Pass != 1 || row.CaseKey != packet.Identity.CaseKey || row.Arm != "model" || row.Status != "failed" || row.Disposition != "halt" || !row.Attempted || row.Finished || row.Cleanup || row.Observer || row.Success || row.StructuralPass || row.RecordingFile != "arm-000001-model.json" || row.RecordingSHA256 != conversationHash(b.raw["records/window-000001/arm-000001-model.json"]) {
		return reject()
	}
	wantStatus := FullWindowActualStatus{EvidenceKind: "recorded_run", Closed: true, NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: 1, FailedAttemptedArms: 1, UnattemptedArms: 7319, ObservedTurns: 1, GlobalUnrunTurns: 11771, ModelAttempts: 1}
	if !reflect.DeepEqual(s.FullWindowActualStatus, wantStatus) || s.WindowIndex != 1 || s.ReportStatus != "stopped" || s.ReportSHA256 != conversationHash(b.raw["records/window-000001/receipt.json"]) || s.PayloadManifestSHA256 != r.PayloadManifestSHA256 || s.LedgerSHA256 != r.LedgerSHA256 || s.FirstUnrunOrdinal != 1 || s.FirstUnrunArm != "jev_selective" || s.FutureUnrunFromOrdinal != 29 {
		return reject()
	}
	wantStatus.LastReceiptSHA256 = conversationHash(b.raw["records/window-000001-summary.json"])
	if !reflect.DeepEqual(b.terminal, wantStatus) {
		return reject()
	}
	next := base
	next.ClaimSHA256 = fullWindowCanonicalClaims()
	next.ClaimSHA256[windowStoppedClaimID] = claimSHA
	next.PermanentModel, next.PermanentJEV = c.ModelPermanent, c.JEVPermanent
	next.RemainingModel, next.RemainingJEV = next.TotalModel-next.PermanentModel, next.TotalJEV-next.PermanentJEV
	next.ActualModelCalls += s.ModelAttempts
	next.ActualJEVCalls += s.JEVAttempts
	return next, liability, nil
}
