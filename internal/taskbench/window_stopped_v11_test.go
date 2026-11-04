package taskbench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowStoppedV11Exact154DataOnly(t *testing.T) {
	b := expectedStoppedV11WindowBaseline()
	if len(b.ClaimSHA256) != 154 || b.PermanentModel != 79620965 || b.PermanentJEV != 838363 || b.ActualModelCalls != 6659 || b.ActualJEVCalls != 1677 {
		t.Fatalf("missing concrete154 DATA: claims=%d permanent=%d/%d calls=%d/%d", len(b.ClaimSHA256), b.PermanentModel, b.PermanentJEV, b.ActualModelCalls, b.ActualJEVCalls)
	}
}

func TestWindowStoppedV11DetachedAndNoAuthority(t *testing.T) {
	b := expectedStoppedV11WindowBaseline()
	old := expectedStoppedV10WindowBaseline()
	for k, v := range old.ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old claim changed")
		}
	}
	for i := 1; i <= 50; i++ {
		if !comparisonDigest(b.ClaimSHA256[fmt.Sprintf("full1220-v11-20261101/window-%06d", i)]) {
			t.Fatal("missing original claim")
		}
	}
	if _, ok := b.ClaimSHA256["full1220-v11-20261101/window-000051"]; ok {
		t.Fatal("invented claim51")
	}
	delete(b.ClaimSHA256, "full1220-v11-20261101/window-000001")
	if len(expectedStoppedV11WindowBaseline().ClaimSHA256) != 154 || len(old.ClaimSHA256) != 104 {
		t.Fatal("shared maps")
	}
	if _, ok := any(windowStoppedV11Baseline{}).(windowStoppedHistory); ok {
		t.Fatal("stopped data reentry authority")
	}
	s := windowStoppedV10Baseline{base: old}
	s.previous.base = old
	s.previous.previous.base = old
	frozen := freezeWindowStoppedV11Predecessor(s)
	s.base.ClaimSHA256["change"] = "changed"
	if reflect.DeepEqual(frozen, s) || frozen.base.ClaimSHA256["change"] != "" || frozen.previous.base.ClaimSHA256["change"] != "" {
		t.Fatal("predecessor snapshot aliases")
	}
}
func TestWindowStoppedV11RejectUnanchoredAndCanceled(t *testing.T) {
	for _, v := range []struct{ path, sha string }{{"/not-read", windowStoppedV11InventorySHA}, {windowStoppedV11InventoryPath, windowV11BaselineSHA}, {windowStoppedV11InventoryPath, "UNFROZEN"}} {
		if _, e := loadWindowStoppedV11BaselineContext(context.Background(), v.path, v.sha); e == nil {
			t.Fatal("unanchored accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := loadWindowStoppedV11BaselineContext(ctx, windowStoppedV11InventoryPath, windowStoppedV11InventorySHA); e == nil {
		t.Fatal("canceled accepted")
	}
}
func TestWindowStoppedV11InventoryFreshPhysicalBoundary(t *testing.T) {
	for _, mode := range []string{"normal", "changed", "extra", "missing", "hardlink", "identity", "cancel", "spool-extra", "end-spool", "end-bytes", "end-namespace", "end-lock"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			os.Mkdir(filepath.Join(root, "campaign"), 0700)
			os.Mkdir(filepath.Join(root, "records"), 0700)
			os.Mkdir(filepath.Join(root, "spool"), 0700)
			os.WriteFile(filepath.Join(root, "spool/batch"), []byte("span"), 0600)
			lock := filepath.Join(root, "campaign/.lock")
			record := filepath.Join(root, "records/data")
			os.WriteFile(lock, nil, 0600)
			os.WriteFile(record, []byte("data"), 0600)
			inv := windowStoppedV11Inventory{Count: 3, Bytes: 8}
			for _, name := range []string{"campaign/.lock", "records/data", "spool/batch"} {
				path := filepath.Join(root, name)
				id, e := historyPathIdentity(path, false)
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := os.ReadFile(path)
				inv.Files = append(inv.Files, windowStoppedV11InventoryEntry{Path: name, SHA256: conversationHash(raw), Bytes: int64(len(raw)), Identity: [7]int64{int64(id.Dev), int64(id.Ino), int64(id.Mode.Perm()) | 0100000, int64(id.Links), id.Size, id.Mtime, id.Ctime}})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, e := newHistoryReadPass(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			switch mode {
			case "changed":
				os.WriteFile(record, []byte("edit"), 0600)
			case "extra":
				os.WriteFile(filepath.Join(root, "records/extra"), nil, 0600)
			case "missing":
				os.Remove(record)
			case "hardlink":
				os.Link(record, filepath.Join(root, "link"))
			case "identity":
				inv.Files[1].Identity[1]++
			case "cancel":
				cancel()
			case "spool-extra":
				os.WriteFile(filepath.Join(root, "spool/extra"), nil, 0600)
			}
			files, e := readWindowStoppedV11Inventory(p, root, inv)
			if mode != "normal" && !strings.HasPrefix(mode, "end-") {
				if e == nil {
					t.Fatal("unsafe original accepted")
				}
				return
			}
			if e != nil || len(files) != 3 {
				t.Fatal(e)
			}
			if mode == "end-lock" {
				if e = historyStoppedLock(p, 11, lock, func() error { return nil }); e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "end-bytes":
				os.WriteFile(record, []byte("edit"), 0600)
			case "end-spool":
				os.WriteFile(filepath.Join(root, "spool/extra"), nil, 0600)
			case "end-namespace":
				os.WriteFile(filepath.Join(root, "records/extra"), nil, 0600)
			case "end-lock":
				os.Remove(lock)
				os.WriteFile(lock, nil, 0600)
			}
			e = p.Finish()
			if mode == "normal" && e != nil {
				t.Fatal(e)
			}
			if mode != "normal" && e == nil {
				t.Fatal("End accepted changed dependency")
			}
		})
	}
}

func TestWindowStoppedV11LockBounds(t *testing.T) {
	for _, epoch := range []int{0, 12, 99} {
		t.Run(fmt.Sprint(epoch), func(t *testing.T) {
			p, e := newHistoryReadPass(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			called := false
			if e = p.WithStoppedLock(epoch, "/not-read", func() error { called = true; return nil }); e == nil || called {
				t.Fatal("unsupported epoch accepted")
			}
		})
	}
}

func TestWindowStoppedV11SourceStreamFresh(t *testing.T) {
	for _, mode := range []string{"normal", "source-changed", "source-End"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			path := filepath.Join(root, "source.go")
			raw := []byte("package original\n")
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			p, e := newHistoryReadPass(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			if e = historyStoppedRoot(p, root); e != nil {
				t.Fatal(e)
			}
			if mode == "source-changed" {
				os.WriteFile(path, []byte("package changed\n"), 0600)
			}
			_, e = p.Stream(historyReadInventoryStream, path, conversationHash(raw), windowActualSourceMaxBytes)
			if mode == "source-changed" {
				if e == nil {
					t.Fatal("changed source accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "source-End" {
				os.WriteFile(path, []byte("package changed\n"), 0600)
			}
			e = p.Finish()
			if (e == nil) != (mode == "normal") {
				t.Fatalf("fresh source End %v", e)
			}
		})
	}
}

func TestWindowStoppedV11PureRejections(t *testing.T) {
	for _, pending := range []bool{false, true} {
		if _, e := readWindowStoppedV11Native("native", windowPayloadManifest{}, []byte("{}"), map[string][]byte{}, map[string]bool{}, pending); e == nil {
			t.Fatal("invalid ledger accepted")
		}
	}
	if _, e := readWindowStoppedV11Bundle("/unanchored", windowActualManifest{}, expectedStoppedV10WindowBaseline(), map[string][]byte{}); e == nil {
		t.Fatal("unbound bundle accepted")
	}
	for _, o := range []*windowObservationReceiptV11{nil, {Boundary: "durable_local", RemoteDelivery: "delivered"}, {Boundary: "durable_local", RemoteDelivery: "pending", NormalClosure: true}} {
		if validateWindowStoppedV11Observation(windowActualManifest{}, o, false) == nil {
			t.Fatal("normal/remote marker admitted as stopped data")
		}
	}
}
