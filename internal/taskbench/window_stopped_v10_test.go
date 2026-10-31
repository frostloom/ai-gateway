package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowStoppedV10Exact104DataOnly(t *testing.T) {
	b := expectedStoppedV10WindowBaseline()
	old := expectedStoppedV9WindowBaseline()
	if len(b.ClaimSHA256) != 104 || b.TotalModel != 200000000 || b.TotalJEV != 4000000 || b.PermanentModel != 66825427 || b.PermanentJEV != 638655 || b.ActualModelCalls != 5164 || b.ActualJEVCalls != 1315 {
		t.Fatal("incorrect permanent104")
	}
	for k, v := range old.ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("changed historical claim")
		}
	}
	delete(b.ClaimSHA256, "full1220-v10-20261031/window-000001")
	if len(expectedStoppedV10WindowBaseline().ClaimSHA256) != 104 {
		t.Fatal("shared claim map")
	}
	if _, ok := any(windowStoppedV10Baseline{}).(windowStoppedHistory); ok {
		t.Fatal("DATA baseline implements reentry")
	}
	if reflect.DeepEqual(windowV11CandidateBaseline(old), expectedStoppedV10WindowBaseline()) {
		t.Fatal("candidate parent rewrites history")
	}
}
func TestWindowStoppedV10RejectsUnanchoredAndCanceled(t *testing.T) {
	if _, e := loadWindowStoppedV10BaselineContext(context.Background(), "/not-read", windowV9BaselineSHA); e == nil {
		t.Fatal("old evidence accepted")
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := loadWindowStoppedV10BaselineContext(c, "/not-read", windowV11BaselineSHA); e == nil {
		t.Fatal("canceled constructor")
	}
}

func TestWindowStoppedV10InventoryFreshPhysicalBoundary(t *testing.T) {
	for _, mode := range []string{"normal", "modified", "extra", "missing", "hardlink", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			os.Mkdir(filepath.Join(root, "campaign"), 0700)
			os.Mkdir(filepath.Join(root, "records"), 0700)
			lock := filepath.Join(root, "campaign/.lock")
			record := filepath.Join(root, "records/record.json")
			os.WriteFile(lock, nil, 0600)
			os.WriteFile(record, []byte("data"), 0600)
			var inv windowStoppedV10Inventory
			raw := []byte(`{"files":[{"path":"campaign/.lock","sha256":"` + conversationHash(nil) + `","size":0},{"path":"records/record.json","sha256":"` + conversationHash([]byte("data")) + `","size":4}],"file_count":2,"total_bytes":4}`)
			json.Unmarshal(raw, &inv)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, e := newHistoryReadPass(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			switch mode {
			case "missing":
				os.Remove(record)
			case "hardlink":
				os.Link(record, filepath.Join(root, "linked"))
			case "extra":
				os.WriteFile(filepath.Join(root, "records/extra"), nil, 0600)
			case "cancel":
				cancel()
			}
			files, e := readWindowStoppedV10Inventory(p, root, inv)
			if mode != "normal" && mode != "modified" {
				if e == nil {
					t.Fatal("unsafe inventory accepted")
				}
				return
			}
			if e != nil || len(files) != 2 {
				t.Fatal(e)
			}
			if mode == "modified" {
				os.WriteFile(record, []byte("edit"), 0600)
				if e = p.Finish(); e == nil {
					t.Fatal("End accepted changed bytes")
				}
			} else if e = p.Finish(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
