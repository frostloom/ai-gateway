package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestWindowStoppedV12Exact156DataOnly(t *testing.T) {
	old := expectedStoppedV11WindowBaseline()
	b := expectedStoppedV12WindowBaseline()
	if len(b.ClaimSHA256) != 156 || b.PermanentModel != 79974331 || b.PermanentJEV != 845904 || b.ActualModelCalls != 6691 || b.ActualJEVCalls != 1685 {
		t.Fatalf("wrong156 DATA: %+v", b)
	}
	for k, v := range old.ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old154 changed")
		}
	}
	for i, sha := range windowStoppedV12ClaimHashes {
		if b.ClaimSHA256[fmt.Sprintf("full3660-v12-r4-20261105/window-%06d", i+1)] != sha {
			t.Fatal("missing Root4 claim")
		}
	}
	delete(b.ClaimSHA256, "full3660-v12-r4-20261105/window-000001")
	if len(expectedStoppedV12WindowBaseline().ClaimSHA256) != 156 || len(old.ClaimSHA256) != 154 {
		t.Fatal("aliased historical map")
	}
	if _, ok := any(windowStoppedV12Baseline{}).(windowStoppedHistory); ok {
		t.Fatal("DATA has execution authority")
	}
}
func TestWindowStoppedV12RejectUnanchoredAndCanceled(t *testing.T) {
	for _, x := range []struct{ path, sha string }{{"/not-read", windowStoppedV12InventorySHA}, {windowStoppedV12InventoryPath, windowStoppedV11InventorySHA}, {windowStoppedV12InventoryPath, "UNFROZEN"}} {
		if _, e := loadWindowStoppedV12BaselineContext(context.Background(), x.path, x.sha); e == nil {
			t.Fatal("unanchored accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := loadWindowStoppedV12BaselineContext(ctx, windowStoppedV12InventoryPath, windowStoppedV12InventorySHA); e == nil {
		t.Fatal("canceled accepted")
	}
}
func TestWindowStoppedV12DetachedPredecessor(t *testing.T) {
	s := windowStoppedV11Baseline{base: expectedStoppedV11WindowBaseline()}
	s.previous.base = expectedStoppedV10WindowBaseline()
	frozen := freezeWindowStoppedV12Predecessor(s)
	s.base.ClaimSHA256["changed"] = "changed"
	s.previous.base.ClaimSHA256["changed"] = "changed"
	if reflect.DeepEqual(s, frozen) || frozen.base.ClaimSHA256["changed"] != "" || frozen.previous.base.ClaimSHA256["changed"] != "" {
		t.Fatal("predecessor alias")
	}
}
func TestWindowStoppedV12PureReject(t *testing.T) {
	if _, e := readWindowStoppedV12Bundle("/foreign", windowActualManifest{}, expectedStoppedV11WindowBaseline(), nil); e == nil {
		t.Fatal("unbound bundle accepted")
	}
	if _, e := readWindowStoppedV12Native("native", windowPayloadManifest{}, nil, nil, map[string]bool{}, false); e == nil {
		t.Fatal("invalid native accepted")
	}
}

func TestWindowStoppedV12V1RecordingReplay(t *testing.T) {
	c := rechargeConversation()
	d := collectionUnmetDriver()
	d.steps[0].Action = "preview"
	d.steps[0].Turn.Response.Trace = []Trace{{Tool: "recharge", Phase: "preview", Outcome: "failure", ResolvedArgs: `{"amount_money":100}`}}
	r := recordCollectionConversation(context.Background(), c, d)
	row := windowAcquisitionRow{CaseKey: c.Key(), ObservedTurns: 1, Status: r.Result.Status, Disposition: r.Disposition}
	if e := validateWindowStoppedV12Recording(r, row, true); e != nil {
		t.Fatal("v1 failed-preview DATA rejected", e)
	}
	for _, name := range []string{"policy", "issue", "hash", "success", "finished", "trace", "disposition"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(r)
			var v collectionRecording
			json.Unmarshal(raw, &v)
			w := row
			switch name {
			case "policy":
				v.PolicyID = "full-customer-recording-policy.v2"
			case "issue":
				v.Issues = nil
			case "hash":
				v.ResultSHA256 = strings.Repeat("f", 64)
			case "success":
				v.Result.Success = true
			case "finished":
				w.Finished = true
			case "trace":
				v.Result.Observations[0].Turn.Response.Trace[0].Outcome = "success"
			case "disposition":
				v.Disposition = "continue_recorded"
				w.Disposition = v.Disposition
			}
			if name != "hash" {
				v.ResultSHA256 = windowV8DeclarationSHA(v.Result)
			}
			if validateWindowStoppedV12Recording(v, w, true) == nil {
				t.Fatal("changed v1 stopped recording admitted")
			}
		})
	}
}
