package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalCLIRejectsBeforeFixture(t *testing.T) {
	for _, args := range [][]string{
		{}, {"-execute-local"}, {"-execute-local", "-mysql-dsn", "root:SECRET@tcp(example.com:3306)/"},
		{"-execute-local", "-mysql-dsn", "root:SECRET@tcp(127.0.0.1:3307)/existing"},
		{"-execute-local", "-mysql-dsn", "root:SECRET@unix(/tmp/mysql.sock)/"},
	} {
		if run(args) == 0 {
			t.Fatal("invalid input accepted")
		}
	}
	dir := filepath.Join(t.TempDir(), "run")
	keys := filepath.Join(t.TempDir(), "keys.json")
	os.WriteFile(keys, []byte(`["models-001/first_turn"]`), 0600)
	args := []string{"-execute-local", "-mysql-dsn", "root:SECRET@tcp(127.0.0.1:1)/", "-keys", keys, "-revision", strings.Repeat("a", 40), "-out", dir, "-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-profiles", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"}
	if run(args) != 2 {
		t.Fatal("selection should reject before connection")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid selection created output")
	}
}

func TestLocalCLIValidatesLiteralLoopbackDSN(t *testing.T) {
	for _, v := range []string{"root@tcp(127.0.0.1:3307)/", "root@tcp([::1]:3307)/"} {
		if !localAdminDSN(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", "root@tcp(localhost:3307)/", "root@tcp(127.0.0.1:3307)/db", "root@tcp(10.0.0.1:3307)/", "root@unix(/tmp/mysql.sock)/"} {
		if localAdminDSN(v) {
			t.Fatal(v)
		}
	}
}
