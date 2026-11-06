package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowStoppedV14Exact178DataOnly(t *testing.T) {
	old := expectedStoppedV13WindowBaseline()
	b := expectedStoppedV14WindowBaseline()
	if len(b.ClaimSHA256) != 178 || b.PermanentModel != 85219758 || b.PermanentJEV != 936184 || b.ActualModelCalls != 7245 || b.ActualJEVCalls != 1871 {
		t.Fatalf("wrong178: %+v", b)
	}
	for k, v := range old.ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old177 changed")
		}
	}
	if b.ClaimSHA256[windowStoppedV14ClaimKey] != windowStoppedV14ClaimSHA {
		t.Fatal("new claim missing")
	}
	b.ClaimSHA256["mutated"] = "bad"
	if len(expectedStoppedV14WindowBaseline().ClaimSHA256) != 178 || len(old.ClaimSHA256) != 177 {
		t.Fatal("aliased map")
	}
	if _, ok := any(windowStoppedV14Baseline{}).(windowStoppedHistory); ok {
		t.Fatal("DATA implements authority")
	}
	s := windowStoppedV13Baseline{base: expectedStoppedV13WindowBaseline()}
	s.previous.base = expectedStoppedV12WindowBaseline()
	f := freezeWindowStoppedV14Predecessor(s)
	s.base.ClaimSHA256["mutated"] = "bad"
	s.previous.base.ClaimSHA256["mutated"] = "bad"
	if reflect.DeepEqual(s, f) || f.base.ClaimSHA256["mutated"] != "" || f.previous.base.ClaimSHA256["mutated"] != "" {
		t.Fatal("predecessor alias")
	}
}
func TestWindowStoppedV14ContextBeforeIO(t *testing.T) {
	if _, e := loadWindowStoppedV14BaselineContext(nil, "/must-not-read", "bad"); e == nil {
		t.Fatal("nil accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := loadWindowStoppedV14BaselineContext(ctx, "/must-not-read", "bad"); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel identity lost: %v", e)
	}
	if _, e := loadWindowStoppedV14BaselineContext(context.Background(), "/must-not-read", "bad"); e == nil {
		t.Fatal("unanchored accepted")
	}
}

// Explicit DATA parsing only. No history constructor/provider/credentials/role.
// Root separately supervises the full constructor after reviewing capacity.
func TestWindowStoppedV14OriginalBundleData(t *testing.T) {
	if os.Getenv("TASKBENCH_STOPPED_V14_BUNDLE_DATA") != "1" {
		t.Skip("explicit original stopped DATA replay")
	}
	raw, e := os.ReadFile(windowStoppedV14InventoryPath)
	if e != nil {
		t.Fatal(e)
	}
	if conversationHash(raw) != windowStoppedV14InventorySHA {
		t.Fatal("descriptor SHA")
	}
	var inv windowStoppedV11Inventory
	if decodeWindowActualJSON(raw, &inv) != nil {
		t.Fatal("descriptor JSON")
	}
	files := map[string][]byte{}
	for _, v := range inv.Files {
		if strings.HasPrefix(v.Path, "regression/source/") || strings.HasPrefix(v.Path, "source-git/") || v.Path == "full-window-actual-v14" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(inv.Root, v.Path))
		if e != nil || conversationHash(b) != v.SHA256 {
			t.Fatal("original bytes", v.Path, e)
		}
		files[v.Path] = b
	}
	var m windowActualManifest
	if decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil {
		t.Fatal("manifest")
	}
	if e := readWindowStoppedV14Signatures(m, files); e != nil {
		t.Fatal(e)
	}
	got, e := readWindowStoppedV14Bundle(inv.Root, m, expectedStoppedV13WindowBaseline(), files)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, expectedStoppedV14WindowBaseline()) {
		t.Fatal("derived178")
	}
	for _, name := range []string{"pending-cleared", "held-cleared", "stopped-promoted", "finished-promoted", "false-cleanup", "false-observer", "false-advanced", "false-payload", "operation300", "payload-completion", "quality-end-arms", "quality-end-turns", "claim2", "checkpoint1", "native10-body", "native10-transport", "usage", "preview", "unattempted-record", "terminal-open", "predecessor", "approval-missing", "approval-old", "wrong-pub"} {
		t.Run(name, func(t *testing.T) {
			fs := map[string][]byte{}
			for k, v := range files {
				fs[k] = append([]byte(nil), v...)
			}
			first := expectedStoppedV13WindowBaseline()
			mm := m
			var r windowAcquisitionReceipt
			json.Unmarshal(fs["records/window-000001/receipt.json"], &r)
			switch name {
			case "pending-cleared":
				r.Accounting.Pending = nil
			case "held-cleared":
				r.Accounting.JEVHeld = 0
			case "stopped-promoted":
				r.Status = "collected_window"
			case "finished-promoted":
				r.Rows[3].Finished = true
			case "false-cleanup":
				r.Cleanup = true
			case "false-observer":
				r.Observer = true
			case "false-advanced":
				r.Advanced = true
			case "false-payload":
				r.PayloadVerified = true
			case "operation300":
				r.OperationLimitSeconds = 300
			case "payload-completion":
				r.PayloadAuditOnlyCompletion = "complete"
			case "quality-end-arms":
				r.QualityEndArms = 1
			case "quality-end-turns":
				r.QualityEndUnrunTurns = 1
			case "claim2":
				fs["campaign/claim-000002.json"] = []byte("{}")
			case "checkpoint1":
				fs["campaign/checkpoint-000001.json"] = []byte("{}")
			case "native10-body":
				fs["records/window-000001/native-payloads/response-000010.body"] = []byte("{}")
			case "native10-transport":
				fs["records/window-000001/native-payloads/response-000010.json"] = []byte("{}")
			case "usage":
				fs["records/window-000001/native-payloads/response-000001.body"] = []byte("{}")
			case "preview":
				delete(fs, "records/window-000001/preview-failure-000386-model.json")
			case "unattempted-record":
				r.Rows[4].RecordingFile = "forged.json"
			case "terminal-open":
				fs["records/terminal.json"] = []byte("{}")
			case "predecessor":
				first.ClaimSHA256["mutated"] = "bad"
			case "approval-missing":
				delete(fs, "operation-policy-approval.json")
			case "approval-old":
				fs["operation-policy-approval.json"] = fs["execution-authorization.json"]
			case "wrong-pub":
				mm.BuildAttestationPublicKey = "bad"
			}
			fs["records/window-000001/receipt.json"], _ = json.Marshal(r)
			sigerr := readWindowStoppedV14Signatures(mm, fs)
			_, be := readWindowStoppedV14Bundle(inv.Root, mm, first, fs)
			if sigerr == nil && be == nil {
				t.Fatal("altered stopped DATA accepted")
			}
		})
	}
}
