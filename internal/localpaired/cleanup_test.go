package localpaired

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCleanRejectsProtectedRoots(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	ledger := filepath.Join(root, "billing", "ledger")
	frozen := filepath.Join(root, "eval", "task-definitions", "2026-10-12-full-dev-v1")
	langfuse := filepath.Join(root, "deploy", "langfuse")
	allowed := filepath.Join(root, "old-output")
	nested := filepath.Join(allowed, "nested")
	for _, dir := range []string{ledger, frozen, langfuse, nested} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	allow := []string{ledger, frozen, langfuse, allowed}

	if err := Clean(allow, allowed); err == nil {
		t.Fatal("expected error when the new question bank file is absent")
	}
	assertKept(t, nested)

	bank := filepath.Join("eval", "campaigns", "2026-11-10-local-paired", "cases.json")
	if err := os.MkdirAll(filepath.Dir(bank), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bank, []byte("local-read-history-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clean(allow, allowed); err == nil {
		t.Fatal("expected error when the four task 2 cases are absent")
	}
	assertKept(t, nested)

	const cases = "local-read-history-001\nlocal-offtopic-history-001\nlocal-read-pending-001\nlocal-offtopic-pending-001\n"
	if err := os.WriteFile(bank, []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{ledger, frozen, langfuse} {
		if err := Clean(allow, dir); err == nil {
			t.Fatalf("Clean(%q) succeeded, want error", dir)
		}
		assertKept(t, dir)
	}
	if err := Clean(allow, nested); err == nil {
		t.Fatal("expected error for a path that is not an exact allowlisted entry")
	}
	assertKept(t, nested)

	if err := Clean(allow, allowed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(allowed); !os.IsNotExist(err) {
		t.Fatalf("allowlisted path = %v, want removed", err)
	}
	for _, dir := range []string{ledger, frozen, langfuse} {
		assertKept(t, dir)
	}
}

func assertKept(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("path %q was deleted: %v", dir, err)
	}
}

func TestCleanRejectsProtectedVariants(t *testing.T) {
	variants := []struct {
		name   string
		allow  string
		target string
	}{
		{name: "dotdot cleans away langfuse", target: "deploy/langfuse/.."},
		{name: "allow list cleans to langfuse parent", allow: "deploy", target: "deploy/langfuse/.."},
		{name: "dotdot cleans away frozen definitions", target: "eval/task-definitions/2026-10-12-full-dev-v1/.."},
		{name: "dotdot cleans to current directory", target: "deploy/langfuse/../.."},
		{name: "dotdot cleans allowlisted output to dot", target: "old-output/.."},
		{name: "dotdot returns to allowlisted output", target: "old-output/../old-output"},
		{name: "parent of langfuse", target: "deploy"},
		{name: "parent of frozen definitions", target: "eval/task-definitions"},
		{name: "ancestor of frozen definitions", target: "eval"},
		{name: "langfuse case variant", target: "deploy/Langfuse"},
		{name: "frozen definitions case variant", target: "Eval/Task-Definitions/2026-10-12-full-dev-v1"},
		{name: "billing ledger case variant", target: "Billing/Ledger"},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			prepareCleanRoot(t)
			before := keepFiles(t)
			allow := variant.allow
			if allow == "" {
				allow = variant.target
			}
			err := Clean([]string{allow}, variant.target)
			if err == nil || !strings.Contains(err.Error(), "protected") {
				t.Fatalf("err = %v, want a protected-path refusal", err)
			}
			after := keepFiles(t)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("keep files = %v, want %v", after, before)
			}
		})
	}
}

func prepareCleanRoot(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	for _, dir := range []string{
		filepath.Join("billing", "ledger"),
		filepath.Join("eval", "task-definitions", "2026-10-12-full-dev-v1"),
		filepath.Join("deploy", "langfuse"),
		"old-output",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bank := filepath.Join("eval", "campaigns", "2026-11-10-local-paired", "cases.json")
	if err := os.MkdirAll(filepath.Dir(bank), 0o755); err != nil {
		t.Fatal(err)
	}
	const cases = "local-read-history-001\nlocal-offtopic-history-001\nlocal-read-pending-001\nlocal-offtopic-pending-001\n"
	if err := os.WriteFile(bank, []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keepFiles(t *testing.T) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "keep.txt" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(path)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
