package taskbench

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWindowStoppedRequiredArtifactSet(t *testing.T) {
	// These are the required artifacts from the stopped execution contract,
	// independently of the implementation's pin map.
	names := []string{"actual-manifest.json", "campaign/claim-000001.json", "campaign/ledger-000001/accounting.jsonl", "records/startup.json", "records/terminal.json", "records/window-000001-summary.json", "records/window-000001/receipt.json", "records/window-000001/payload-manifest.json", "records/window-000001/arm-000001-model.json", "records/window-000001/native-payloads/store.json", "records/window-000001/native-payloads/request-000001.json", "records/window-000001/native-payloads/request-000001.body", "records/window-000001/native-payloads/response-000001.json", "records/window-000001/native-payloads/response-000001.body"}
	pins := windowStoppedArtifactPins()
	if len(pins) != len(names) {
		t.Fatal("stopped artifact set changed")
	}
	for _, name := range names {
		if !comparisonDigest(pins[name]) {
			t.Fatal("required stopped evidence omitted", name)
		}
	}
	for id := range fullWindowCanonicalClaims() {
		names = append(names, "legacy/"+id+".json")
	}
	for _, mode := range []string{"missing", "changed"} {
		for _, name := range names {
			t.Run(mode+"/"+name, func(t *testing.T) {
				root := stoppedEvidenceRoot(t)
				expected := map[string]string{}
				for _, file := range names {
					path := filepath.Join(root, file)
					raw := []byte("original:" + file)
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
					expected[file] = conversationHash(raw)
				}
				if _, err := readWindowStoppedFiles(root, expected, 65536); err != nil {
					t.Fatal("complete fixture rejected", err)
				}
				path := filepath.Join(root, name)
				if mode == "missing" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := readWindowStoppedFiles(root, expected, 65536); err == nil {
					t.Fatal("required evidence mutation ignored", name)
				}
			})
		}
	}
}

func stoppedEvidenceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWindowStoppedFilesReadBoundedOriginalBytes(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	raw := []byte(`{"retained":true}`)
	if err := os.WriteFile(filepath.Join(root, "receipt.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readWindowStoppedFiles(root, map[string]string{"receipt.json": conversationHash(raw)}, 65536)
	if err != nil || string(got["receipt.json"]) != string(raw) {
		t.Fatal("bounded read rejected", err)
	}
	for _, mutation := range []string{"missing", "changed", "symlink", "fifo", "oversize", "traversal", "absolute"} {
		t.Run(mutation, func(t *testing.T) {
			dir := stoppedEvidenceRoot(t)
			file := filepath.Join(dir, "receipt.json")
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			pins := map[string]string{"receipt.json": conversationHash(raw)}
			max := int64(65536)
			switch mutation {
			case "missing":
				os.Remove(file)
			case "changed":
				os.WriteFile(file, []byte(`{}`), 0600)
			case "symlink":
				os.Remove(file)
				if err := os.Symlink(filepath.Join(root, "receipt.json"), file); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				os.Remove(file)
				if err := syscall.Mkfifo(file, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				max = 1
			case "traversal":
				pins = map[string]string{"../receipt.json": conversationHash(raw)}
			case "absolute":
				pins = map[string]string{file: conversationHash(raw)}
			}
			if _, err := readWindowStoppedFiles(dir, pins, max); err == nil {
				t.Fatal("invalid stopped file accepted")
			}
		})
	}
}

func TestWindowStoppedLockRejectsLiveOwnerAndNeverCreatesFile(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	file := filepath.Join(root, ".lock")
	called := 0
	verify := func() error { called++; return nil }
	if err := withWindowStoppedLock(file, verify); err == nil || called != 0 {
		t.Fatal("missing lock accepted")
	}
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatal("verifier created missing old lock")
	}
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := withWindowStoppedLock(file, verify); err != nil || called != 1 {
		t.Fatal("closed owner rejected", err)
	}
	fd, err := os.OpenFile(file, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	if err = syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err = withWindowStoppedLock(file, verify); err == nil || called != 1 {
		t.Fatal("live old owner accepted")
	}
	if err = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("verification failure")
	if err = withWindowStoppedLock(file, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("verification failure lost", err)
	}
	if err = withWindowStoppedLock(file, verify); err != nil {
		t.Fatal("failed verification leaked lock", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil || len(raw) != 0 {
		t.Fatal("old lock bytes changed")
	}
}

func TestWindowStoppedInventoryRejectsUncountedClaimsAndPackets(t *testing.T) {
	for _, extra := range []string{"", "campaign/claim-000002.json", "records/window-000002-summary.json", "records/window-000001/native-payloads/request-000002.body"} {
		t.Run(extra, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			for name := range windowStoppedArtifactPins() {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "campaign/.lock"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if extra != "" {
				if err := os.WriteFile(filepath.Join(root, extra), []byte("uncounted"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := validateWindowStoppedInventory(root)
			if extra == "" && err != nil {
				t.Fatal("original inventory rejected", err)
			}
			if extra != "" && err == nil {
				t.Fatal("uncounted paid evidence ignored")
			}
		})
	}
}

func TestWindowStoppedDescriptorCannotSupplyBudgetOrCreateRoots(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	for _, raw := range []string{
		`{}`, `null`, `{"schema":"wrong"}`,
		`{"schema":"full-window-stopped-baseline.v1","original_root":"/missing-original-root","historical_claims_dir":"/claims","permanent_model_micro_cny":0}`,
		`{"schema":"full-window-stopped-baseline.v1","original_root":"../relative","historical_claims_dir":"/claims"}`,
	} {
		file := filepath.Join(root, "descriptor.json")
		if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadWindowStoppedBaseline(file, conversationHash([]byte(raw))); err == nil {
			t.Fatal("untrusted descriptor accepted")
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 {
			t.Fatal("descriptor created evidence or claim", err)
		}
	}
}
