package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIRejectsLiveAndMalformedInput(t *testing.T) {
	if e := run([]string{"-execute"}); e == nil {
		t.Fatal("accepted live flag")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "input.json")
	out := filepath.Join(dir, "report.json")
	os.WriteFile(in, []byte(`{"schema":"knowledge-comparison.v1","unexpected":true}`), 0600)
	if e := run([]string{"-input", in, "-output", out}); e == nil {
		t.Fatal("accepted malformed input")
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("wrote report on invalid input")
	}
}
