package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

// Preparation describes local artifacts and planned settings. It does not
// attest to the model, JEV or runtime settings of a running remote service.
type Preparation struct {
	Schema      string            `json:"schema"`
	Kind        string            `json:"kind"`
	CreatedAt   time.Time         `json:"created_at"`
	Version     string            `json:"version"`
	Model       string            `json:"model"`
	JevMode     string            `json:"jev_mode"`
	RoutingMode string            `json:"routing_mode"`
	SourceGit   string            `json:"source_git"`
	TaskCount   int               `json:"task_count"`
	MaxTasks    int               `json:"max_tasks"`
	ModelCalls  int               `json:"model_calls"`
	JevCalls    int               `json:"jev_calls"`
	Hashes      map[string]string `json:"hashes"`
	Limitations []string          `json:"limitations"`
}

func plannedRouting(jevMode string) string {
	if jevMode == "on" {
		return "jev_selective"
	}
	return "model"
}

func buildPreparation(c runConfig, cases []taskbench.Case, evaluatorPath string) (Preparation, error) {
	p := Preparation{Schema: "task-preparation.v1", Kind: "offline_preparation", CreatedAt: time.Now().UTC(), Version: c.Version, Model: c.Model, JevMode: c.JevMode, RoutingMode: plannedRouting(c.JevMode), TaskCount: len(cases), MaxTasks: c.MaxTasks, Hashes: map[string]string{}, Limitations: []string{
		"Offline only: no model/JEV requests, database connections, fixture creation or accuracy measurements.",
		"Planned routing/model settings require verification from actual runtime telemetry before comparison.",
		"Source revision is a declared build origin, not proof that the binary was built from that commit; local source and binary hashes are frozen separately.",
		"Development regression cases and synthetic fixtures are not an independent blind test. No price or future fee is inferred.",
		"Task cap limits task count, not model calls or money; one task can include multiple turns, tool loops and retries.",
	}}
	if c.MaxTasks <= 0 || len(cases) > c.MaxTasks {
		return p, fmt.Errorf("max-tasks must be positive and cover the %d selected tasks", len(cases))
	}
	revision, err := exec.Command("git", "rev-parse", "--verify", "--end-of-options", c.Source+"^{commit}").Output()
	if err != nil {
		return p, fmt.Errorf("source must resolve to an existing commit")
	}
	p.SourceGit = strings.TrimSpace(string(revision))
	paths := map[string]string{"evaluation_contract": "docs/customer-service-evaluation-contract-2026-10-09.md", "fixture_definition": "internal/taskbench/live.go", "agent_binary": c.AgentBinary, "evaluator_binary": evaluatorPath}
	if c.CasesFile != "" {
		paths["cases"] = c.CasesFile
	} else {
		raw, _ := json.Marshal(cases)
		p.Hashes["cases"] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	// Freeze source inputs as well as binaries: stale preparations are rejected
	// even if the operator forgets to rebuild after editing a prompt or oracle.
	for _, dir := range []string{"internal/agent", "internal/taskbench", "internal/jev", "cmd/agent", "cmd/task-eval"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return p, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
				path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
				paths["source:"+path] = path
			}
		}
	}
	for name, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return p, fmt.Errorf("cannot freeze %s: %w", name, err)
		}
		p.Hashes[name] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return p, nil
}

func verifyPreparation(path string, current Preparation) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read prepared manifest: %w", err)
	}
	var frozen Preparation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frozen); err != nil {
		return fmt.Errorf("invalid prepared manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid prepared manifest trailing content")
	}
	// Timestamp is intentionally excluded; each invocation recomputes identity.
	frozen.CreatedAt, current.CreatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(frozen, current) {
		return fmt.Errorf("prepared manifest does not match current cases, source, binaries or planned settings; prepare again")
	}
	return nil
}

func writePreparation(path string, p Preparation) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(raw, '\n'))
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
