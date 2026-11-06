package taskbench

import (
	"encoding/json"
	"os"
	"testing"
)

func TestWindowStoppedV14InterruptedOriginalData(t *testing.T) {
	if os.Getenv("TASKBENCH_STOPPED_V14_BUNDLE_DATA") != "1" {
		t.Skip("explicit interrupted original DATA")
	}
	raw, e := os.ReadFile(windowStoppedV14Root + "/records/window-000001/arm-000387-jev_selective.json")
	if e != nil {
		t.Fatal(e)
	}
	r, e := decodeWindowStoppedV2Recording(raw)
	if e != nil {
		t.Fatal(e)
	}
	if e := validateWindowStoppedV14InterruptedRecording(r); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"context-error", "step-error", "final-error", "issue-code", "issue-index", "initial-state", "final-state", "partial-tool-result", "success", "policy"} {
		t.Run(name, func(t *testing.T) {
			var x collectionRecording
			if json.Unmarshal(raw, &x) != nil {
				t.Fatal("fixture")
			}
			switch name {
			case "context-error":
				x.Result.Errors[0] = "context canceled"
			case "step-error":
				x.Result.Errors[1] = "turn 1: complete"
			case "final-error":
				x.Result.Errors[2] = "final oracle: complete"
			case "issue-code":
				x.Issues[0].Code = "transport_failure"
			case "issue-index":
				x.Issues[0].ErrorIndex++
			case "initial-state":
				x.Result.Initial.Own.BalanceCent++
			case "final-state":
				x.Result.Final.Own.BalanceCent++
			case "partial-tool-result":
				x.Result.Observations[0].Turn.RawBody = "{}"
			case "success":
				x.Result.Success = true
			case "policy":
				x.PolicyID = collectionPolicyID
			}
			// Coherently rebind the inner result digest; altered issues are outside it.
			x.ResultSHA256 = windowV8DeclarationSHA(x.Result)
			if validateWindowStoppedV14InterruptedRecording(x) == nil {
				t.Fatal("altered interrupted DATA accepted")
			}
		})
	}
}
