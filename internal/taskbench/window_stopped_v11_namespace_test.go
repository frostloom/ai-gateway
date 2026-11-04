package taskbench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func stoppedV11SpoolNamesFixture() map[string]bool {
	names := map[string]bool{"header.json": false, "closed.json": false, "lock": false}
	for i := 1; i <= 2256; i++ {
		names[fmt.Sprintf("%020d.pb", i)] = false
		names[fmt.Sprintf("%020d.commit", i)] = false
	}
	return names
}
func TestWindowStoppedV11SpoolNamespacePolicy(t *testing.T) {
	names := stoppedV11SpoolNamesFixture()
	if len(names) != 4515 || !stoppedV11SpoolNamespaceAllowed(true, windowStoppedV11Root+"/spool", names) {
		t.Fatal("fixed original4515 spool namespace rejected")
	}
	for _, path := range []string{windowStoppedV10Root + "/spool", "/foreign/spool", windowStoppedV11Root + "/spool-other", windowStoppedV11Root + "/spool/../spool"} {
		if stoppedV11SpoolNamespaceAllowed(true, path, names) {
			t.Fatal("foreign/alias quota expansion")
		}
	}
	if stoppedV11SpoolNamespaceAllowed(false, windowStoppedV11Root+"/spool", names) {
		t.Fatal("ordinary spool quota changed")
	}
	for _, mode := range []string{"missing", "extra", "wrongname", "directory"} {
		t.Run(mode, func(t *testing.T) {
			n := stoppedV11SpoolNamesFixture()
			switch mode {
			case "missing":
				delete(n, "header.json")
			case "extra":
				n["extra"] = false
			case "wrongname":
				delete(n, "header.json")
				n["extra"] = false
			case "directory":
				n["header.json"] = true
			}
			if stoppedV11SpoolNamespaceAllowed(true, windowStoppedV11Root+"/spool", n) {
				t.Fatal("incomplete namespace admitted")
			}
		})
	}
}
func TestWindowStoppedV11SpoolDirectoryExactAndOldBound(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	names := stoppedV11SpoolNamesFixture()
	for name := range names {
		if e := os.WriteFile(filepath.Join(root, name), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := validateWindowStoppedV2Dir(root, names); e == nil {
		t.Fatal("old4096 bound widened")
	}
	if e := validateWindowStoppedV11SpoolDirectory(root, names); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(root, "header.json")
	if e := os.Remove(file); e != nil {
		t.Fatal(e)
	}
	if e := validateWindowStoppedV11SpoolDirectory(root, names); e == nil {
		t.Fatal("missing file accepted")
	}
	os.WriteFile(file, nil, 0600)
	os.WriteFile(filepath.Join(root, "extra"), nil, 0600)
	if e := validateWindowStoppedV11SpoolDirectory(root, names); e == nil {
		t.Fatal("extra file accepted")
	}
	os.Remove(filepath.Join(root, "extra"))
	os.Remove(file)
	os.Mkdir(file, 0700)
	if e := validateWindowStoppedV11SpoolDirectory(root, names); e == nil {
		t.Fatal("wrong type accepted")
	}
	os.Remove(file)
	os.Symlink(filepath.Join(root, "closed.json"), file)
	if e := validateWindowStoppedV11SpoolDirectory(root, names); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestWindowStoppedV11NamespaceOldInitialAndEnd(t *testing.T) {
	for _, mode := range []string{"normal", "extra-End", "missing-End"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			file := filepath.Join(root, "data")
			os.WriteFile(file, nil, 0600)
			p, e := newHistoryReadPass(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			p.stoppedV11Metadata = true
			names := map[string]bool{"data": false}
			if e = historyStoppedDir(p, root, names); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "extra-End":
				os.WriteFile(filepath.Join(root, "extra"), nil, 0600)
			case "missing-End":
				os.Remove(file)
			}
			e = p.Finish()
			if (e == nil) != (mode == "normal") {
				t.Fatalf("End mode=%s error=%v", mode, e)
			}
		})
	}
}
