package taskbench

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestWindowStoppedV13Exact177DataOnly(t *testing.T) {
	old := expectedStoppedV12WindowBaseline()
	b := expectedStoppedV13WindowBaseline()
	if len(b.ClaimSHA256) != 177 || b.PermanentModel != 85037976 || b.PermanentJEV != 932373 || b.ActualModelCalls != 7237 || b.ActualJEVCalls != 1869 {
		t.Fatalf("wrong177 DATA: %+v", b)
	}
	for k, v := range old.ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old156 changed")
		}
	}
	for i, sha := range windowStoppedV13ClaimHashes {
		if b.ClaimSHA256[fmt.Sprintf("full3660-v13-r4-20261105/window-%06d", i+1)] != sha {
			t.Fatal("missing Root4 claim")
		}
	}
	delete(b.ClaimSHA256, "full3660-v13-r4-20261105/window-000001")
	if len(expectedStoppedV13WindowBaseline().ClaimSHA256) != 177 || len(old.ClaimSHA256) != 156 {
		t.Fatal("aliased historical map")
	}
	if _, ok := any(windowStoppedV13Baseline{}).(windowStoppedHistory); ok {
		t.Fatal("DATA has execution authority")
	}
}
func TestWindowStoppedV13RejectUnanchoredAndCanceled(t *testing.T) {
	for _, x := range []struct{ path, sha string }{{"/not-read", windowStoppedV13InventorySHA}, {windowStoppedV13InventoryPath, windowStoppedV12InventorySHA}, {windowStoppedV13InventoryPath, "UNFROZEN"}} {
		if _, e := loadWindowStoppedV13BaselineContext(context.Background(), x.path, x.sha); e == nil {
			t.Fatal("unanchored accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := loadWindowStoppedV13BaselineContext(ctx, windowStoppedV13InventoryPath, windowStoppedV13InventorySHA); e == nil {
		t.Fatal("canceled accepted")
	}
}
func TestWindowStoppedV13DetachedPredecessor(t *testing.T) {
	s := windowStoppedV12Baseline{base: expectedStoppedV12WindowBaseline()}
	s.previous.base = expectedStoppedV10WindowBaseline()
	frozen := freezeWindowStoppedV13Predecessor(s)
	s.base.ClaimSHA256["changed"] = "changed"
	s.previous.base.ClaimSHA256["changed"] = "changed"
	if reflect.DeepEqual(s, frozen) || frozen.base.ClaimSHA256["changed"] != "" || frozen.previous.base.ClaimSHA256["changed"] != "" {
		t.Fatal("predecessor alias")
	}
}
func TestWindowStoppedV13PureReject(t *testing.T) {
	if _, e := readWindowStoppedV13Bundle("/foreign", windowActualManifest{}, expectedStoppedV12WindowBaseline(), nil); e == nil {
		t.Fatal("unbound bundle accepted")
	}
	if _, e := readWindowStoppedV12Native("native", windowPayloadManifest{}, nil, nil, map[string]bool{}, false); e == nil {
		t.Fatal("invalid native accepted")
	}
}
func TestWindowStoppedV13PreviewSidecarRetainsOriginalNegatives(t *testing.T) {
	c, d := collectionFailedPreviewExample(t)
	r := recordCollectionPreviewFailureConversation(context.Background(), c, d)
	raw := mustCollectionJSON(t, windowPreviewFailureSidecar{Schema: "full-window-preview-failure-sidecar.v13", Record: r})
	recordRaw := mustCollectionJSON(t, r.collectionRecording)
	evidence := &windowPreviewFailureEvidence{PolicySHA256: windowOwnedPreviewFailurePolicySHA(), RecordingSHA256: conversationHash(recordRaw), SidecarFile: "preview-failure-000001-model.json", SidecarSHA256: conversationHash(raw)}
	row := windowAcquisitionRow{Ordinal: 1, Arm: "model", CaseKey: c.Key(), ObservedTurns: len(r.Result.Observations), Status: r.Result.Status, Disposition: r.Disposition, StructuralPass: r.Result.StructuralPass, Success: r.Result.Success, PreviewFailure: evidence}
	files := map[string][]byte{"window/" + evidence.SidecarFile: raw}
	if e := validateWindowStoppedV13Recording(r.collectionRecording, row, files, "window", map[string]bool{}); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"missing", "changed", "policy", "normal-promoted", "identity", "no-sidecar"} {
		t.Run(mode, func(t *testing.T) {
			x, y := r.collectionRecording, row
			f := map[string][]byte{}
			for k, v := range files {
				f[k] = append([]byte(nil), v...)
			}
			ev := *evidence
			y.PreviewFailure = &ev
			switch mode {
			case "missing":
				delete(f, "window/"+ev.SidecarFile)
			case "changed":
				f["window/"+ev.SidecarFile] = []byte("{}")
			case "policy":
				x.PolicyID = collectionPolicyID
			case "normal-promoted":
				y.Success = true
			case "identity":
				y.Ordinal++
			case "no-sidecar":
				y.PreviewFailure = nil
			}
			if validateWindowStoppedV13Recording(x, y, f, "window", map[string]bool{}) == nil {
				t.Fatal("changed preview DATA accepted")
			}
		})
	}
}
