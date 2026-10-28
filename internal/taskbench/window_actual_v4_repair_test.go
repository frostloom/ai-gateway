package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// One original width28 local_stub window exercises the actual Handler repair
// loop and accounting2. This never uses actual readiness or a real provider.
func TestActualV4CollectorReplyRepairSettlesEveryClosedRequest(t *testing.T) {
	auth, model, jev := testActualV4CollectorLocalStub(t)
	original := auth.modelUpstream
	injected := false
	const injectRepair = true
	auth.modelUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		response, err := original.RoundTrip(req)
		if err != nil {
			return response, err
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		var body map[string]any
		if err = json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		// Every real closed transport call carries valid, reported usage: 100 total
		// input (20miss+80hit),8output => ceil(40+3.2)+64 =108microCNY.
		usage := body["usage"].(map[string]any)
		usage["input_tokens"], usage["cache_read_input_tokens"] = 20, 80
		if injectRepair && !injected {
			// Before any successful balance read in the first original slot, an
			// unsupported terminal cash assertion must enter the existing one-repair
			// path. Subsequent responses use the untouched closed model behavior.
			body["content"] = []any{map[string]any{"type": "text", "text": "当前现金余额为 ¥5000.00。"}}
			body["stop_reason"] = "end_turn"
			injected = true
		}
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		response.Body = io.NopCloser(bytes.NewReader(raw))
		return response, nil
	})
	receipt, err := runWindowAcquisition(context.Background(), auth)
	if err != nil || !receipt.Advanced || !receipt.PayloadVerified || !receipt.Cleanup || !receipt.Observer || receipt.EvidenceKind != "local_stub" || receipt.PlannedCampaignPairs != 3660 || receipt.PlannedCampaignArms != 7320 || receipt.PlannedCampaignTurns != 11772 || receipt.PlannedCurrentPairs != 28 || receipt.FinishedArms != 56 {
		t.Fatal("repair changed collection/denominator", receipt, err)
	}
	repairs, llmCalls, negative := 0, 0, 0
	for _, row := range receipt.Rows {
		raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, row.RecordingFile))
		if e != nil || conversationHash(raw) != row.RecordingSHA256 {
			t.Fatal("record binding", e)
		}
		var recording collectionRecording
		if json.Unmarshal(raw, &recording) != nil || verifyCollectionRecording(recording) != nil {
			t.Fatal("original recording")
		}
		if recording.Result.Success || row.Success {
			t.Fatal("repair promoted unreviewed quality")
		}
		if row.Status == "failed" {
			negative++
			if row.StructuralPass || recording.Result.StructuralPass {
				t.Fatal("quality negative upgraded")
			}
		}
		for _, obs := range recording.Result.Observations {
			tm := obs.Turn.Response.Telemetry
			if tm == nil || tm.Runtime == nil || tm.Runtime.ReplyRepairLimit != 1 || tm.ReplyRepairs < 0 || tm.ReplyRepairs > 1 || tm.LLMCalls > 5 {
				t.Fatal("unbounded/absent original Handler telemetry", tm)
			}
			repairs += tm.ReplyRepairs
			llmCalls += tm.LLMCalls
		}
	}
	if !injected || repairs != 1 {
		t.Fatalf("real Handler reply repair missing: injected=%v repairs=%d model=%d FakeJEV=%d", injected, repairs, *model, *jev)
	}
	if negative == 0 || receipt.QualityNegativeArms != negative || llmCalls != *model || *model != 117 || *jev != 28 {
		t.Fatal("repair sends or negative accounting", repairs, llmCalls, *model, *jev, negative, receipt.QualityNegativeArms)
	}
	ledgerDir := filepath.Join(auth.campaign.root, "ledger-000001")
	audit, e := readWindowAccounting(ledgerDir)
	if e != nil {
		t.Fatal(e)
	}
	if audit.Contract.Schema != "full-window-accounting.v2" || audit.Contract.Model.CacheHitInputRate != 40000 || audit.Contract.Model.MaxAttempts != auth.campaign.prepared.scope.Windows[0].MaxModelCalls || audit.State.ModelAttempts != *model || audit.State.JEVAttempts != *jev || audit.State.ModelKnown != int64(*model)*108 || audit.State.JEVKnown != int64(*jev)*5 || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 || audit.State.Pending != nil || audit.State.Halt != "" || !audit.State.Closed {
		t.Fatal("repair calls were not settled", audit)
	}
	raw, e := os.ReadFile(filepath.Join(ledgerDir, "accounting.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	pending := map[int]string{}
	modelSettles, jevSettles := 0, 0
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var event windowAccountingEvent
		if json.Unmarshal(line, &event) != nil {
			t.Fatal("ledger JSON")
		}
		if event.Kind == "intent" {
			if event.Request == nil || event.Request.Attempt < 1 || event.Request.Attempt > 5 {
				t.Fatal("loop attempt exceeded", event)
			}
			pending[event.Sequence] = event.Request.Provider
		}
		if event.Kind == "settle" {
			switch pending[event.Sequence] {
			case "model":
				modelSettles++
			case "jev":
				jevSettles++
			default:
				t.Fatal("settlement without actual closed intent")
			}
			delete(pending, event.Sequence)
		}
	}
	if len(pending) != 0 || modelSettles != *model || jevSettles != *jev {
		t.Fatal("unbilled/double settled repair request", modelSettles, jevSettles, pending)
	}
	if auth.campaign.prepared.scope.Schema != "full-window-scope.v4" {
		t.Fatal("wrong epoch")
	}
	t.Logf("local_stub original28slots: repairs=%d model=%d FakeJEV=%d known=%d/%d negatives=%d;synthetic only", repairs, *model, *jev, audit.State.ModelKnown, audit.State.JEVKnown, negative)
}
