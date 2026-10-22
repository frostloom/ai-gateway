package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPiLiveCheckPreservesReadOnlyAndExecutionGates(t *testing.T) {
	a, p := entryPaths(t)
	authRaw, _ := os.ReadFile(a)
	planRaw, _ := os.ReadFile(p)
	var auth, plan map[string]any
	json.Unmarshal(authRaw, &auth)
	json.Unmarshal(planRaw, &plan)
	plan["schema"] = "campaign-preflight-plan.v2"
	plan["target"] = map[string]string{"base_url": "https://api.deepseek.com/v1", "model": "deepseek-flash", "protocol": "openai", "jev_model": "jev-1.13.0"}
	planRaw, _ = json.Marshal(plan)
	h := sha256.Sum256(planRaw)
	auth["schema"] = "small-pilot-authorization.v2"
	auth["model_base"] = "https://api.deepseek.com/v1"
	auth["model"] = "deepseek-flash"
	auth["protocol"] = "openai"
	auth["scope_plan_sha256"] = hex.EncodeToString(h[:])
	authRaw, _ = json.Marshal(auth)
	dir := t.TempDir()
	a = filepath.Join(dir, "synthetic-approval.json")
	p = filepath.Join(dir, "plan.json")
	for path, data := range map[string][]byte{a: authRaw, p: planRaw} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-authorization", a, "-plan", p, "-env-file", filepath.Join(dir, "private-missing.env"), "-out", filepath.Join(dir, "never-created")}
	var buf bytes.Buffer
	if code := runWithOutput(args, &buf); code != 0 {
		t.Fatalf("Pi check failed %d: %s", code, buf.String())
	}
	var result struct {
		Model    string `json:"model"`
		Protocol string `json:"protocol"`
		Ready    bool   `json:"ready_for_paid_run"`
		Calls    int    `json:"model_calls"`
	}
	if json.Unmarshal(buf.Bytes(), &result) != nil || result.Model != "deepseek-flash" || result.Protocol != "openai" || result.Ready || result.Calls != 0 {
		t.Fatalf("wrong check %s", buf.String())
	}
	buf.Reset()
	if code := runWithOutput(append(args, "-execute"), &buf); code == 0 || !bytes.Contains(buf.Bytes(), []byte("billing evidence is incomplete")) {
		t.Fatalf("execution gate bypassed: %d %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "never-created")); !os.IsNotExist(err) {
		t.Fatal("created output before gates")
	}
	claim, err := claimAuthorization(dir, planRaw, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = claimAuthorization(dir, planRaw, "second"); err == nil {
		t.Fatal("v2 authorization reused via another output")
	}
	if _, err = os.Stat(claim); err != nil {
		t.Fatal(err)
	}
}
