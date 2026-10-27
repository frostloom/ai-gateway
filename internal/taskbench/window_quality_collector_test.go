package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowQualityCollectorRetainsNegativeAndAdvancesOwned(t *testing.T) {
	testWindowQualityCollectorNegatives(t, 1)
}
func TestWindowQualityCollectorOriginalMultiTurnSuffixUnrun(t *testing.T) {
	testWindowQualityCollectorNegatives(t, 14)
}
func testWindowQualityCollectorNegatives(t *testing.T, width int) {
	auth, model, judge := testWindowCollectorAuthorizationPolicy(t, windowOwnedQualityPolicySHA(), width)
	auth.modelUpstream = windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		*model++
		body := `{"model":"deepseek-flash","content":[{"type":"tool_use","id":"owned-wrong-preview","name":"recharge","input":{"amount_money":100}}],"stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":8}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	receipt, err := runWindowAcquisition(context.Background(), auth)
	if err != nil || receipt.Status != "collected_window" || receipt.FinishedArms != 2*width || !receipt.Advanced || receipt.QualityNegativeArms != 2*width || *model != 2*width || *judge != width {
		t.Fatalf("safe negative not acquired: err=%v receipt=%+v model=%d judge=%d", err, receipt, *model, *judge)
	}
	expectedSuffix := 0
	if width == 14 {
		expectedSuffix = 4
	}
	if receipt.QualityEndArms != 2*width || receipt.QualityEndUnrunTurns != expectedSuffix || receipt.GlobalUnrunTurns != 0 || receipt.PlannedCurrentTurns != receipt.ObservedTurns+receipt.QualityEndUnrunTurns || receipt.UnrunArms != 0 {
		t.Fatal("denominator or explicit suffix changed", receipt.PlannedCurrentTurns, receipt.ObservedTurns, receipt.QualityEndUnrunTurns)
	}
	for _, row := range receipt.Rows {
		if row.Status != "failed" || row.Success || row.StructuralPass || row.Disposition != "halt" {
			t.Fatal("original failure promoted", row)
		}
		if row.QualityEnd == nil || !row.ExcludeCompleteTaskComparison || row.QualityEnd.UnrunTurns != row.DefinedTurns-row.ObservedTurns {
			t.Fatal("missing certified quality end", row)
		}
		q := row.QualityEnd
		for file, digest := range map[string]string{q.CandidateFile: q.CandidateSHA256, q.CleanupFile: q.CleanupSHA256} {
			raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, file))
			if e != nil || conversationHash(raw) != digest {
				t.Fatal("durable evidence linkage", file, e)
			}
		}
		if width == 14 && row.Ordinal == 14 && (row.CaseKey != "adv-014/supplement_then_confirm" || row.ObservedTurns != 1 || row.DefinedTurns != 3 || q.UnrunTurns != 2) {
			t.Fatal("authentic multi-turn source not stopped before next turn", row)
		}
		var record collectionRecording
		raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, row.RecordingFile))
		if e != nil || json.Unmarshal(raw, &record) != nil || verifyCollectionRecording(record) != nil || record.Result.Success {
			t.Fatal("original raw v1 failed recording not replayable", e)
		}
		raw, e = os.ReadFile(filepath.Join(auth.privateReportRoot, q.CleanupFile))
		var cleanup struct {
			Deleted int64 `json:"deleted"`
			Absent  bool  `json:"session_absent"`
		}
		if e != nil || json.Unmarshal(raw, &cleanup) != nil || cleanup.Deleted != 1 || !cleanup.Absent {
			t.Fatal("cleanup audit data incomplete", e)
		}
	}
	if _, err = os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); err != nil {
		t.Fatal("next synthetic claim missing", err)
	}
}

// Data validation is separate from the actual live proof tests above. These
// synthetic checkpoint shapes cannot mint a collector or provider capability.
func TestWindowQualityCheckpointRequiresExplicitTypedSuffix(t *testing.T) {
	slot := fullWindowSlot{Ordinal: 1, Pass: 1, CaseKey: "synthetic/confirm", UserTurns: 3, ArmOrder: [2]string{"model", "jev_selective"}}
	base := windowQualityEndEvidence{PolicySHA256: windowOwnedQualityPolicySHA(), SourceSHA256: strings.Repeat("a", 64), RuleSHA256: strings.Repeat("b", 64), CandidateFile: "quality-candidate.json", CandidateSHA256: strings.Repeat("c", 64), CleanupFile: "quality-cleanup.json", CleanupSHA256: strings.Repeat("d", 64), EnvelopeSHA256: strings.Repeat("e", 64), ObservedTurns: 1, UnrunTurns: 2}
	for _, mode := range []string{"valid", "nil", "old_policy", "observed_zero", "observed_over", "suffix_missing", "suffix_negative", "source_missing", "rule_missing", "candidate_missing", "cleanup_missing", "envelope_missing", "file_escape", "same_file"} {
		t.Run(mode, func(t *testing.T) {
			q := base
			policy := windowOwnedQualityPolicySHA()
			observed := 1
			p := &q
			switch mode {
			case "nil":
				p = nil
			case "old_policy":
				policy = collectionPolicySHA()
			case "observed_zero":
				observed = 0
			case "observed_over":
				observed = 4
			case "suffix_missing":
				q.UnrunTurns = 0
			case "suffix_negative":
				q.UnrunTurns = -1
			case "source_missing":
				q.SourceSHA256 = ""
			case "rule_missing":
				q.RuleSHA256 = ""
			case "candidate_missing":
				q.CandidateSHA256 = ""
			case "cleanup_missing":
				q.CleanupSHA256 = ""
			case "envelope_missing":
				q.EnvelopeSHA256 = ""
			case "file_escape":
				q.CandidateFile = "../candidate.json"
			case "same_file":
				q.CleanupFile = q.CandidateFile
			}
			err := validateWindowQualityEndEvidence(policy, slot, "model", observed, p)
			if (err == nil) != (mode == "valid") {
				t.Fatal("typed suffix validation", mode, err)
			}
		})
	}
	ledger := &windowAccounting{}
	campaign := &windowCampaign{index: 1, ledger: ledger, prepared: windowCampaignPrepared{scope: fullWindowScope{Bindings: fullWindowBindings{PolicySHA256: windowOwnedQualityPolicySHA()}, Windows: []fullWindowDeclaration{{FirstOrdinal: 1, LastOrdinal: 1}}, Slots: []fullWindowSlot{slot}}}}
	cp := &windowCampaignOwnedCheckpoint{owner: campaign, ledger: ledger, index: 1, evidence: []windowCampaignArmEvidence{{Ordinal: 1, Pass: 1, ObservedTurns: 1, Key: slot.CaseKey, Arm: "model", RecordingSHA256: strings.Repeat("f", 64), Cleanup: true, Observer: true, QualityEnd: &base}, {Ordinal: 1, Pass: 1, ObservedTurns: 3, Key: slot.CaseKey, Arm: "jev_selective", RecordingSHA256: strings.Repeat("f", 64), Cleanup: true, Observer: true}}}
	if err := campaign.validateEvidenceWindowCampaign(cp); err != nil {
		t.Fatal("explicit typed suffix rejected", err)
	}
	cp.evidence[0].QualityEnd = nil
	if err := campaign.validateEvidenceWindowCampaign(cp); err == nil {
		t.Fatal("generic shortened arm accepted without owned quality evidence")
	}
}
