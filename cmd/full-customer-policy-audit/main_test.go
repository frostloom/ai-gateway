package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyAuditCLIRejectsInvalidInputsWithoutOutput(t *testing.T) {
	for _, args := range [][]string{nil, {"-model-key", "must-not-exist"}, {"-journals", "missing"}} {
		if run(args) == 0 {
			t.Fatal("invalid input accepted")
		}
	}
	dir := t.TempDir()
	refs := filepath.Join(dir, "refs.json")
	out := filepath.Join(dir, "new.json")
	for _, raw := range []string{"not-json", "[]", `[{"path":"missing","sha256":"bad"}]`} {
		if os.WriteFile(refs, []byte(raw), 0600) != nil {
			t.Fatal("fixture write failed")
		}
		if run([]string{"-journals", refs, "-out", out, "-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-profiles", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"}) == 0 {
			t.Fatal("invalid references accepted")
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatal("invalid audit wrote output")
		}
	}
	os.WriteFile(out, []byte("sentinel"), 0600)
	if run([]string{"-journals", refs, "-out", out}) == 0 {
		t.Fatal("existing file accepted")
	}
	raw, _ := os.ReadFile(out)
	if string(raw) != "sentinel" {
		t.Fatal("old output changed")
	}
}
