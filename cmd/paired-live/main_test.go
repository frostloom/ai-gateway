package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func entryPaths(t *testing.T) (string, string) {
	t.Helper()
	return "../../eval/paired-pilots/2026-10-22-authorized-small-pilot/authorization.json", "../../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json"
}

func TestExecutionRevisionRejectsUnverifiableOrChangedSource(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		raw, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git failed: %s", raw)
		}
		return strings.TrimSpace(string(raw))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "main.go")
	git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	head := git("rev-parse", "HEAD")
	if err := verifyExecutionRevision(root, head, head, false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		want, built string
		dirty       bool
	}{{strings.Repeat("a", 40), head, false}, {head, "", false}, {head, head, true}} {
		if verifyExecutionRevision(root, tc.want, tc.built, tc.dirty) == nil {
			t.Fatal("unverified revision accepted")
		}
	}
	if err := os.WriteFile(filepath.Join(root, "injected.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if verifyExecutionRevision(root, head, head, false) == nil {
		t.Fatal("untracked runtime source accepted")
	}
	if err := os.Remove(filepath.Join(root, "injected.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if verifyExecutionRevision(root, head, head, false) == nil {
		t.Fatal("modified runtime source accepted")
	}
}

func TestCheckDoesNotLoadCredentialsOrCreateOutput(t *testing.T) {
	a, p := entryPaths(t)
	out := filepath.Join(t.TempDir(), "untouched")
	var buf bytes.Buffer
	code := runWithOutput([]string{"-authorization", a, "-plan", p, "-env-file", "does-not-exist-private.env", "-out", out}, &buf)
	var result struct {
		HumanAuthorized bool     `json:"human_authorized"`
		Ready           bool     `json:"ready_for_paid_run"`
		Blockers        []string `json:"blockers"`
	}
	if code != 0 || json.Unmarshal(buf.Bytes(), &result) != nil || !result.HumanAuthorized || result.Ready || len(result.Blockers) != 3 {
		t.Fatalf("check: exit=%d %s", code, buf.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("check created output")
	}
}
func TestNonreadyExecuteRejectsBeforeSecretsAndFixtures(t *testing.T) {
	a, p := entryPaths(t)
	out := filepath.Join(t.TempDir(), "never-created")
	var buf bytes.Buffer
	code := runWithOutput([]string{"-authorization", a, "-plan", p, "-execute", "-env-file", "does-not-exist-private.env", "-out", out}, &buf)
	if code == 0 || !bytes.Contains(buf.Bytes(), []byte("billing evidence is incomplete")) || bytes.Contains(buf.Bytes(), []byte("does-not-exist")) {
		t.Fatalf("wrong rejection: %d %s", code, buf.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("nonready execute created output")
	}
}

func TestReadyExecuteRejectsChangedDefinitionsBeforeSecrets(t *testing.T) {
	a, p := entryPaths(t)
	plan, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	planHash := sha256.Sum256(plan)
	evidence := []byte("synthetic test evidence, never supplier billing evidence")
	evidenceHash := sha256.Sum256(evidence)
	dir := t.TempDir()
	source := filepath.Join(dir, "evidence")
	billing := filepath.Join(dir, "billing.json")
	raw, _ := json.Marshal(map[string]any{"schema": "paired-billing-attestation.v1", "scope_plan_sha256": hex.EncodeToString(planHash[:]), "model_base": "https://maas.qianwenaiapi.com/apps/anthropic", "model": "deepseek-v4.1-flash", "protocol": "anthropic", "jev_model": "jev-1.13.0", "account_tariff_verified": true, "token_envelope_verified": true, "jev_failure_charges_verified": true, "evidence_sha256": hex.EncodeToString(evidenceHash[:])})
	for path, body := range map[string][]byte{source: evidence, billing: raw} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	output := filepath.Join(dir, "not-created")
	code := runWithOutput([]string{"-authorization", a, "-plan", p, "-billing-evidence", billing, "-billing-source", source, "-execute", "-definitions", dir, "-revision", strings.Repeat("a", 40), "-release", "test", "-out", output, "-env-file", "missing.env"}, &buf)
	if code != 2 || !bytes.Contains(buf.Bytes(), []byte("frozen definitions or profiles rejected")) {
		t.Fatalf("wrong rejection: %d %s", code, buf.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("created run output")
	}
}

// Changing output directories must not allocate the same approval again.
func TestAuthorizationClaimCannotBeReusedWithNewOutput(t *testing.T) {
	root := t.TempDir()
	_, planPath := entryPaths(t)
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := claimAuthorization(root, plan, "first-output")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(plan, &fields); err != nil {
		t.Fatal(err)
	}
	fields["unrelated_metadata"] = "changed"
	reformatted, _ := json.MarshalIndent(fields, "", " ")
	if _, err = claimAuthorization(root, reformatted, "different-output"); err == nil {
		t.Fatal("fresh output/metadata reused spending authorization")
	}
	raw, err := os.ReadFile(claim)
	if err != nil || !bytes.Contains(raw, []byte("first-output")) {
		t.Fatal("first intent not durable")
	}
	info, err := os.Stat(claim)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("claim permissions")
	}
}
