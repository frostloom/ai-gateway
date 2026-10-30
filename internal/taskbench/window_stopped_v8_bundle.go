package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
)

// Pure historical data validation. Only the anchored loader can construct the
// concrete stopped46 type; no acquisition or quality capability is produced.
func readWindowStoppedV8Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func(reason string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v8 bundle rejected: %s", reason)
	}
	if !reflect.DeepEqual(first, expectedStoppedV7WindowBaseline()) || m.Scope.Schema != "full-window-scope.v8" || len(m.Scope.Windows) != 131 || m.CampaignRoot != filepath.Join(root, "campaign") {
		return reject("predecessor/declaration")
	}
	// Claim2 was acquired after the sole completed inner acquisition, then the
	// outer controller stopped. Its reservation survives despite no ledger2.
	for n := range files {
		if strings.HasPrefix(n, "campaign/claim-") && n != "campaign/claim-000001.json" && n != "campaign/claim-000002.json" || strings.HasPrefix(n, "campaign/checkpoint-") && n != "campaign/checkpoint-000001.json" || strings.HasPrefix(n, "campaign/ledger-") && n != "campaign/ledger-000001/accounting.jsonl" {
			return reject("later campaign path")
		}
	}
	var owner struct {
		ExitCode           *int     `json:"exit_code"`
		EndedEpoch         *float64 `json:"ended_epoch"`
		RelaunchAllowed    *bool    `json:"relaunch_allowed"`
		SupervisionFailure string   `json:"supervision_failure,omitempty"`
		SupervisionSignal  *int     `json:"supervision_signal,omitempty"`
		SupervisorExitCode *int     `json:"supervisor_exit_code,omitempty"`
	}
	_, ownerJSONErr := windowJSONObject(files["actual-owner-exit.json"])
	ownerDecoder := json.NewDecoder(bytes.NewReader(files["actual-owner-exit.json"]))
	ownerDecoder.DisallowUnknownFields()
	if ownerJSONErr != nil || ownerDecoder.Decode(&owner) != nil || owner.ExitCode == nil || *owner.ExitCode != 1 || owner.EndedEpoch == nil || *owner.EndedEpoch <= 0 || math.IsNaN(*owner.EndedEpoch) || math.IsInf(*owner.EndedEpoch, 0) || owner.RelaunchAllowed == nil || *owner.RelaunchAllowed {
		return reject("owner stop")
	}
	var cp windowCampaignCheckpoint
	if decodeWindowActualJSON(files["campaign/checkpoint-000001.json"], &cp) != nil {
		return reject("checkpoint decode")
	}
	base := first
	base.TotalModel, base.TotalJEV = 200000000, 4000000
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	claimSHA := ""
	var authSHA string
	for i := 1; i <= 2; i++ {
		var c windowCampaignClaim
		n := fmt.Sprintf("campaign/claim-%06d.json", i)
		w := m.Scope.Windows[i-1]
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if decodeWindowActualJSON(files[n], &c) != nil {
			return reject("claim decode")
		}
		if i == 1 {
			authSHA = c.BudgetApprovalSHA256
		}
		previousCP := ""
		if i == 2 {
			previousCP = conversationHash(files["campaign/checkpoint-000001.json"])
		}
		if !comparisonDigest(authSHA) || c.Schema != "full-window-campaign-claim.v8" || c.BaselineEvidenceSHA256 != windowV8BaselineSHA || c.BudgetApprovalSHA256 != authSHA || c.ParentModel != 140000000 || c.ParentJEV != 1100000 || c.Root != m.CampaignRoot || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.PreviousClaimSHA256 != claimSHA || c.PreviousCheckpointSHA256 != previousCP || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return reject("claim chain")
		}
		claimSHA = conversationHash(files[n])
		base.ClaimSHA256[fmt.Sprintf("full1220-v8-20261030/window-%06d", i)] = claimSHA
	}
	if base.PermanentModel != 52654504 || base.PermanentJEV != 408701 {
		return reject("permanent46")
	}
	ledger := "campaign/ledger-000001/accounting.jsonl"
	audit, err := readWindowAccountingBytes(files[ledger])
	if err != nil {
		return reject("journal replay")
	}
	contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, m.Scope.Windows[0], m.Limits)
	contract.ClaimSHA256 = conversationHash(files["campaign/claim-000001.json"])
	a := audit.State
	if !reflect.DeepEqual(audit.Contract, contract) || !a.Closed || a.Halt != "" || a.EnvelopeViolation || a.Pending != nil || a.RetryRequired != nil || len(a.UnknownIntents) != 0 || a.ModelAttempts != 68 || a.JEVAttempts != 28 || a.ModelHeld != 0 || a.JEVHeld != 0 || a.ModelUnknownHeld != 0 || a.JEVUnknownHeld != 0 {
		return reject("fixed settled journal")
	}
	var receipt windowAcquisitionReceipt
	var summary FullWindowActualReceipt
	var terminal FullWindowActualStatus
	var payload windowPayloadManifest
	if decodeWindowActualJSON(files["records/window-000001/receipt.json"], &receipt) != nil || decodeWindowActualJSON(files["records/window-000001-summary.json"], &summary) != nil || decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil || decodeWindowActualJSON(files["records/window-000001/payload-manifest.json"], &payload) != nil {
		return reject("receipt decode")
	}
	runtime := windowV8RuntimeSummary{Schema: "full-window-runtime-evidence.v8", LogicalModelCalls: 68, LogicalJEVCalls: 28, NativeModelIntents: 68, NativeJEVIntents: 28, NativeSettlements: 96, Known: windowV8Amounts{a.ModelKnown, a.JEVKnown}, HistoricalPermanent: windowV8Amounts{first.PermanentModel, first.PermanentJEV}, BasePermanent: windowV8Amounts{base.PermanentModel - first.PermanentModel, base.PermanentJEV - first.PermanentJEV}, TotalPermanent: windowV8Amounts{base.PermanentModel, base.PermanentJEV}, UnknownIntents: []windowV8UnknownRecord{}, Extras: []windowV8ExtraClaim{}}
	beforeNext := runtime
	beforeNext.BasePermanent = windowV8Amounts{contract.ModelCap, contract.JEVCap}
	beforeNext.TotalPermanent = windowV8Amounts{first.PermanentModel + contract.ModelCap, first.PermanentJEV + contract.JEVCap}
	if cp.Schema != "full-window-campaign-owned-checkpoint.v8" || cp.Index != 1 || cp.ScopeSHA256 != m.ScopeSHA256 || cp.ExecutionSHA256 != m.ExecutionSHA256 || cp.DefinitionSHA256 != m.Scope.DefinitionSHA256 || cp.ScheduleSHA256 != m.Scope.ScheduleSHA256 || cp.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || cp.ClaimSHA256 != contract.ClaimSHA256 || cp.LedgerSHA256 != audit.JournalSHA256 || !reflect.DeepEqual(cp.V8, &beforeNext) {
		return reject("checkpoint linkage")
	}
	if receipt.Schema != "full-window-acquisition-receipt.v8" || receipt.EvidenceKind != "recorded_run" || receipt.Status != "collected_window" || receipt.ScopeSHA256 != m.ScopeSHA256 || receipt.ExecutionSHA256 != m.ExecutionSHA256 || receipt.WindowIndex != 1 || receipt.WindowID != contract.WindowID || receipt.AttemptedArms != 56 || receipt.FinishedArms != 56 || receipt.ObservedTurns != 60 || !receipt.Cleanup || !receipt.EnvironmentCleanup || !receipt.Observer || !receipt.Advanced || !receipt.PayloadVerified || receipt.PayloadStatus != "verified_ledger_linkage" || receipt.NextClaimSHA256 != claimSHA || receipt.LedgerSHA256 != audit.JournalSHA256 || receipt.LedgerCompletionProof != "unavailable_from_journal" || receipt.AccountingSource != "live_owner_snapshot" || !reflect.DeepEqual(receipt.Accounting, a) || !reflect.DeepEqual(receipt.V8, &runtime) || receipt.OperationLimitSeconds != 300 || receipt.DurationMS != 331970.619 || !reflect.DeepEqual(payload.Contract, contract) || receipt.PayloadManifestSHA256 != conversationHash(files["records/window-000001/payload-manifest.json"]) {
		return reject("inner collected linkage")
	}
	status := FullWindowActualStatus{V8: &runtime, EvidenceKind: "recorded_run", Closed: true, NextWindowIndex: 1, PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851, AttemptedArms: 56, FinishedArms: 56, UnattemptedArms: 7264, ObservedTurns: 60, GlobalUnrunTurns: 11712, ModelAttempts: 68, JEVAttempts: 28}
	if !reflect.DeepEqual(summary.FullWindowActualStatus, status) || summary.WindowIndex != 1 || summary.ReportSHA256 != conversationHash(files["records/window-000001/receipt.json"]) || summary.LedgerSHA256 != audit.JournalSHA256 || summary.PayloadManifestSHA256 != receipt.PayloadManifestSHA256 || summary.ReportStatus != "collected_window" || summary.FirstUnrunOrdinal != 29 || summary.FirstUnrunArm != "model" || summary.FutureUnrunFromOrdinal != 29 {
		return reject("outer stopped summary")
	}
	status.LastReceiptSHA256 = conversationHash(files["records/window-000001-summary.json"])
	if !reflect.DeepEqual(terminal, status) {
		return reject("terminal")
	}
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	base.ActualModelCalls += a.ModelAttempts
	base.ActualJEVCalls += a.JEVAttempts
	return base, nil
}
