package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func args() []string { return []string{"-root", "../.."} }
func TestCheckNeverLoadsCredentialsOrOpensNetwork(t *testing.T) {
	var b bytes.Buffer
	out := filepath.Join(t.TempDir(), "no-created-run")
	a := append(args(), "-env-file", "/definitely/missing/private.env", "-out", out)
	if run(a, &b) != 0 {
		t.Fatal(b.String())
	}
	var r struct {
		Ready bool `json:"ready_for_paid_run"`
		Calls int  `json:"model_calls"`
		JEV   int  `json:"jev_calls"`
	}
	if json.Unmarshal(b.Bytes(), &r) != nil || r.Ready || r.Calls != 0 || r.JEV != 0 {
		t.Fatal(b.String())
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("check created output")
	}
	if !strings.Contains(b.String(), "cross_entry_budget_uncoordinated") {
		t.Fatal(b.String())
	}
}
func TestExecuteRequiresNewExactAuthorizationAndProvenance(t *testing.T) {
	var b bytes.Buffer
	a := append(args(), "-execute", "-revision", strings.Repeat("a", 40), "-env-file", "/missing/secret", "-out", filepath.Join(t.TempDir(), "run"))
	if run(a, &b) != 2 || strings.Contains(b.String(), "secret") || !strings.Contains(b.String(), "cross_entry_budget_uncoordinated") {
		t.Fatal(b.String())
	}
}
func TestOldAuthorizationCannotEnableKnowledge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.json")
	os.WriteFile(p, []byte(`{"schema":"recharge-confirm-authorization.v1","human_authorized":true}`), 0600)
	var b bytes.Buffer
	if run(append(args(), "-scope", p), &b) != 2 {
		t.Fatal(b.String())
	}
}
func TestChangedPayloadAndRevisionRejected(t *testing.T) {
	raw, e := os.ReadFile("../../eval/knowledge/control-v1/scope.example.json")
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []string{"payload", "revision"} {
		var m map[string]any
		json.Unmarshal(raw, &m)
		if change == "payload" {
			m["limits"].(map[string]any)["max_output_tokens"] = 1024
		} else {
			m["revision"] = "unbound"
		}
		p := filepath.Join(t.TempDir(), "changed.json")
		raw, _ := json.Marshal(m)
		os.WriteFile(p, raw, 0600)
		var b bytes.Buffer
		if run(append(args(), "-scope", p), &b) != 2 {
			t.Fatal("accepted " + change)
		}
	}
}
func TestLocalModeCannotSelectRemoteOrPaidExecute(t *testing.T) {
	for _, extra := range [][]string{{"-local-stub", "https://api.deepseek.com/v1"}, {"-local-stub", "http://127.0.0.1:9", "-execute"}} {
		var b bytes.Buffer
		if run(append(args(), extra...), &b) != 2 {
			t.Fatal("unsafe local combination accepted")
		}
	}
}
