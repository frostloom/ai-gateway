package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Explicit original DATA replay only: no source constructor, keys, provider,
// owner, execution authority or scoring. Root supervises the full177 constructor.
func TestWindowStoppedV13OriginalBundleData(t *testing.T) {
	if os.Getenv("TASKBENCH_STOPPED_V13_BUNDLE_DATA") != "1" {
		t.Skip("explicit original DATA replay")
	}
	raw, e := os.ReadFile(windowStoppedV13InventoryPath)
	if e != nil {
		t.Fatal(e)
	}
	if conversationHash(raw) != windowStoppedV13InventorySHA {
		t.Fatal("inventory pin")
	}
	var inv windowStoppedV11Inventory
	if json.Unmarshal(raw, &inv) != nil {
		t.Fatal("inventory JSON")
	}
	files := map[string][]byte{}
	for _, entry := range inv.Files {
		b, e := os.ReadFile(filepath.Join(inv.Root, entry.Path))
		if e != nil || conversationHash(b) != entry.SHA256 {
			t.Fatal("original bytes", entry.Path, e)
		}
		files[entry.Path] = b
	}
	var m windowActualManifest
	if decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil {
		t.Fatal("manifest")
	}
	previous := windowStoppedV12Baseline{base: expectedStoppedV12WindowBaseline(), historicalDir: m.ClaimsDir}
	if e := validateWindowStoppedV13Manifest(m, previous); e != nil {
		t.Fatal(e)
	}
	if e := readWindowStoppedV13Signatures(m, files); e != nil {
		t.Fatal(e)
	}
	got, e := readWindowStoppedV13Bundle(inv.Root, m, previous.base, files)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, expectedStoppedV13WindowBaseline()) {
		t.Fatal("derived177")
	}
	// Each mutation remains in memory; originals are never written.
	for _, name := range []string{"stopped-promoted", "checkpoint", "preview", "native", "predecessor"} {
		t.Run(name, func(t *testing.T) {
			fs := map[string][]byte{}
			for k, v := range files {
				fs[k] = v
			}
			first := expectedStoppedV12WindowBaseline()
			switch name {
			case "stopped-promoted":
				var r windowAcquisitionReceipt
				json.Unmarshal(fs["records/window-000021/receipt.json"], &r)
				r.Status = "collected_window"
				r.Advanced = true
				fs["records/window-000021/receipt.json"], _ = json.Marshal(r)
			case "checkpoint":
				delete(fs, "campaign/checkpoint-000021.json")
			case "preview":
				for k := range fs {
					if filepath.Base(k) == "preview-failure-000471-model.json" {
						fs[k] = []byte("{}")
					}
				}
			case "native":
				fs["records/window-000021/native-payloads/response-000001.body"] = []byte("{}")
			case "predecessor":
				first.ClaimSHA256["mutated"] = "bad"
			}
			if _, e := readWindowStoppedV13Bundle(inv.Root, m, first, fs); e == nil {
				t.Fatal("altered DATA accepted")
			}
		})
	}
}
