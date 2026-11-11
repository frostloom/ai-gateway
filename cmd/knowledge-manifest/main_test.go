package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDoesNotRewriteTamperedManifest(t *testing.T) {
	dir := t.TempDir()
	entries, e := os.ReadDir("../../knowledge/platform")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range entries {
		b, e := os.ReadFile(filepath.Join("../../knowledge/platform", f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, f.Name()), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = run([]string{"-dir", dir, "-check"}); e != nil {
		t.Fatal(e)
	}
	m := filepath.Join(dir, "manifest.json")
	if e = os.WriteFile(m, []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = run([]string{"-dir", dir, "-check"}); e == nil {
		t.Fatal("accepted invalid manifest")
	}
	b, _ := os.ReadFile(m)
	if string(b) != "{}" {
		t.Fatal("check rewrote manifest")
	}
}
