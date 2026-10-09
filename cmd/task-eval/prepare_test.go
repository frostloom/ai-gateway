package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The subprocess exercises the real CLI without contacting any service. A
// misplaced backend initialization would fail on the deliberately invalid DSN.
func TestPreparationCLIHelper(t *testing.T) {
	if os.Getenv("TASK_PREPARE_TEST_HELPER") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("TASK_PREPARE_TEST_ARGS")), &args); err != nil {
		panic(err)
	}
	os.Args = append([]string{"task-eval"}, args...)
	flag.CommandLine = flag.NewFlagSet("task-eval", flag.ExitOnError)
	os.Exit(run())
}

func preparationCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0], "-test.run=^TestPreparationCLIHelper$")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "TASK_PREPARE_TEST_HELPER=1", "TASK_PREPARE_TEST_ARGS="+string(raw), "MYSQL_DSN=invalid-offline-dsn", "REDIS_ADDR=invalid", "AGENT_LLM_API_KEY=", "JEV_API_KEY=")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func preparationArgs(t *testing.T, count int) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "agent.bin")
	if err := os.WriteFile(binary, []byte("local binary fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	cases := make([]map[string]any, count)
	for i := range cases {
		cases[i] = map[string]any{"id": strings.Repeat("x", i+1), "category": "read", "message": "查余额", "tool": "get_balance", "read": true}
	}
	raw, _ := json.Marshal(cases)
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(casesPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prepare.json")
	return []string{"-prepare-only", "-out", out, "-version", "dev", "-model", "fake-model", "-jev", "off", "-routing-mode", "model", "-source", "HEAD", "-agent-binary", binary, "-cases", casesPath, "-max-tasks", "1"}, out
}

func TestPrepareOnlyRequiresNoCredentialsWriteScopeOrServices(t *testing.T) {
	args, out := preparationArgs(t, 1)
	output, err := preparationCLI(t, args...)
	if err != nil {
		t.Fatalf("offline preparation failed: %v\n%s", err, output)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema     string            `json:"schema"`
		Kind       string            `json:"kind"`
		TaskCount  int               `json:"task_count"`
		ModelCalls int               `json:"model_calls"`
		JevCalls   int               `json:"jev_calls"`
		Hashes     map[string]string `json:"hashes"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "task-preparation.v1" || manifest.Kind != "offline_preparation" || manifest.TaskCount != 1 || manifest.ModelCalls != 0 || manifest.JevCalls != 0 {
		t.Fatalf("wrong preparation: %s", raw)
	}
	for _, name := range []string{"cases", "evaluation_contract", "fixture_definition", "agent_binary", "evaluator_binary"} {
		if len(manifest.Hashes[name]) != 64 {
			t.Fatalf("missing SHA for %s", name)
		}
	}
	if strings.Contains(string(raw), "API_KEY") || strings.Contains(string(raw), "MYSQL_DSN") {
		t.Fatal("preparation leaked environment")
	}
}

func TestPreparationRefusesOversizedSuiteBeforeCreatingOutput(t *testing.T) {
	args, out := preparationArgs(t, 2)
	output, err := preparationCLI(t, args...)
	if err == nil || !strings.Contains(output, "max-tasks") {
		t.Fatalf("task cap not enforced: %v %s", err, output)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("oversized suite created output")
	}
}

func TestPreparationPreservesExistingEvidence(t *testing.T) {
	args, out := preparationArgs(t, 1)
	if err := os.WriteFile(out, []byte("frozen"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := preparationCLI(t, args...); err == nil {
		t.Fatalf("overwrite allowed: %s", output)
	}
	raw, _ := os.ReadFile(out)
	if string(raw) != "frozen" {
		t.Fatal("existing evidence overwritten")
	}
}

func TestPreparationRejectsContradictoryRouting(t *testing.T) {
	args, _ := preparationArgs(t, 1)
	args = append(args, "-routing-mode", "jev_selective")
	output, err := preparationCLI(t, args...)
	if err == nil || !strings.Contains(output, "routing") {
		t.Fatalf("contradictory modes accepted: %v %s", err, output)
	}
}

func TestPreparedManifestRejectsChangedBinaryAndCases(t *testing.T) {
	for _, target := range []string{"-agent-binary", "-cases"} {
		t.Run(target, func(t *testing.T) {
			args, out := preparationArgs(t, 1)
			if output, err := preparationCLI(t, args...); err != nil {
				t.Fatalf("prepare: %v %s", err, output)
			}
			verifyArgs := append([]string{}, args...)
			for i := range verifyArgs {
				if verifyArgs[i] == "-out" {
					verifyArgs[i+1] = out + ".verified"
				}
			}
			verifyArgs = append(verifyArgs, "-prepared", out)
			if output, err := preparationCLI(t, verifyArgs...); err != nil {
				t.Fatalf("unchanged manifest rejected: %v %s", err, output)
			}
			for i := range args {
				if args[i] == target {
					raw, err := os.ReadFile(args[i+1])
					if err != nil {
						t.Fatal(err)
					}
					// Whitespace changes are valid JSON but must invalidate frozen bytes.
					if err := os.WriteFile(args[i+1], append(raw, ' '), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			for i := range verifyArgs {
				if verifyArgs[i] == "-out" {
					verifyArgs[i+1] = out + ".changed"
				}
			}
			output, err := preparationCLI(t, verifyArgs...)
			if err == nil || !strings.Contains(output, "prepared manifest does not match") {
				t.Fatalf("changed artifact accepted: %v %s", err, output)
			}
			if _, err := os.Stat(out + ".changed"); !os.IsNotExist(err) {
				t.Fatal("stale preparation produced output")
			}
		})
	}
}
