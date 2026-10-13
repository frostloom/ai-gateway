package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func TestPairedCLIPrepareOfflineAndNoOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plan.json")
	args := []string{"-mode", "prepare", "-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-out", p}
	if n := run(args); n != 0 {
		t.Fatalf("exit=%d", n)
	}
	raw, _ := os.ReadFile(p)
	var plan taskbench.SmallPairPlan
	if json.Unmarshal(raw, &plan) != nil || len(plan.Suite.Cases) != 12 || plan.ReadyForPaidRun {
		t.Fatal("not an offline frozen preparation")
	}
	if n := run(args); n == 0 {
		t.Fatal("overwrites preparation")
	}
}
func TestPairedCLIDoesNotAcceptLiveFlags(t *testing.T) {
	for _, args := range [][]string{{"-mode", "run", "-out", "x"}, {"-allow-paid"}, {"-model-endpoint", "https://provider.invalid"}, {"-mode", "report", "unexpected"}} {
		if n := run(args); n == 0 {
			t.Fatalf("accepted live/unexpected args: %v", args)
		}
	}
}

func TestPairedCLIReportsFailedArmsWithoutDroppingThem(t *testing.T) {
	s, e := taskbench.LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if e != nil {
		t.Fatal(e)
	}
	p, e := taskbench.PrepareSmallPair(s)
	if e != nil {
		t.Fatal(e)
	}
	h := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	profiles := map[string]string{}
	for _, c := range p.Suite.Cases {
		profiles[c.Scenario.Profile] = h
	}
	a := taskbench.ConversationComparisonConfig{RoutingMode: "model", EvidenceKind: "local_stub", DefinitionSHA256: s.DefinitionSHA256, ManifestSHA256: s.ManifestSHA256, ProfileSHA256: profiles, Model: "local-test", ProviderProtocol: "openai", PromptSHA256: h, ToolSchemaSHA256: h, OutputBudget: 128, ExecutionRevision: "local-test"}
	b := a
	b.RoutingMode = "jev_selective"
	dir := t.TempDir()
	journal := filepath.Join(dir, "pair")
	out := filepath.Join(dir, "report.json")
	if e = taskbench.RunPreparedPair(context.Background(), journal, taskbench.PairRunConfig{Schema: "paired-run.v1", Suite: p.Suite, Model: a, Selective: b}, nil, func(context.Context, string, taskbench.ConversationCase) (taskbench.ConversationDriver, error) {
		return nil, fmt.Errorf("local fixture intentionally unavailable")
	}); e != nil {
		t.Fatal(e)
	}
	if n := run([]string{"-mode", "report", "-journal", journal, "-out", out}); n != 0 {
		t.Fatalf("exit=%d", n)
	}
	raw, _ := os.ReadFile(out)
	var r taskbench.SmallPairReport
	if json.Unmarshal(raw, &r) != nil || len(r.Rows) != 24 || r.FinishedArms != 24 || r.CompletionRate != nil {
		t.Fatal("failed arms dropped or fabricated quality")
	}
	if _, e = os.Stat(out + ".md"); e != nil {
		t.Fatal(e)
	}
}
