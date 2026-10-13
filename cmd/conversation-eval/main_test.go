package main

import (
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/taskbench"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIRejectsOnlineOptions(t *testing.T) {
	for _, args := range [][]string{{"-model", "paid"}, {"-base", "http://localhost:18083"}, {"-allow-simulated-writes"}, nil} {
		if got := run(args); got != 2 {
			t.Fatalf("accepted online/missing options: %v exit%d", args, got)
		}
	}
}

func TestCLIDiagnosticFailureCannotExitSuccessfully(t *testing.T) {
	const archive = "../../eval/task-definitions/2026-10-12-full-dev-v1"
	suite, err := taskbench.LoadConversations(archive)
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for _, c := range suite.Cases {
		if !c.Scenario.FullTaskMetricEligible {
			key = c.Key()
			break
		}
	}
	input := taskbench.ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: "local_stub", Episodes: []taskbench.ConversationEpisode{{Key: key}}}
	dir := t.TempDir()
	path := filepath.Join(dir, "input.json")
	out := filepath.Join(dir, "report.json")
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if exit := run([]string{"-definitions", archive, "-transcript", path, "-out", out}); exit != 1 {
		t.Fatalf("diagnostic failure exit%d", exit)
	}
	var report taskbench.ConversationReplayReport
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Metrics.FirstTurnDiagnostics != 1 || report.Metrics.DiagnosticSetupFailures != 1 || report.Metrics.FullTasks != 0 || report.ObservedLLMCalls != nil || report.TranscriptSHA256 == "" {
		t.Fatalf("bad diagnostic report: %+v", report)
	}
	if exit := run([]string{"-definitions", archive, "-transcript", path, "-out", out}); exit != 2 {
		t.Fatal("CLI overwrote previous report")
	}
}
