package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func offlineArgs(out string) []string {
	return []string{"-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-profiles", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", "-revision", strings.Repeat("a", 40), "-out", out}
}

func TestFullCustomerOfflinePreflight(t *testing.T) {
	out := filepath.Join(t.TempDir(), "probe.json")
	if run(offlineArgs(out)) != 0 {
		t.Fatal("offline preparation failed")
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var p preflightReport
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.SourceCases != 1220 || p.PairedArms != 2440 || p.PlannedUserTurns != 3924 || p.BoundaryCount != 1851 || len(p.Rows) != 1220 {
		t.Fatal("full denominator lost")
	}
	if p.AllowPaid || p.ReadyForPaidRun || p.ModelCalls != 0 || p.JEVCalls != 0 {
		t.Fatal("offline probe grants permission or calls providers")
	}
	source, err := taskbench.LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	small, err := taskbench.PrepareSmallPair(source)
	if err != nil {
		t.Fatal(err)
	}
	whitelist := map[string]bool{}
	for _, c := range small.Suite.Cases {
		whitelist[c.Key()] = true
	}
	covered := 0
	for _, row := range p.Rows {
		if len(row.PairRunContract) != 64 || row.PilotWhitelistMember != whitelist[row.Key] || !row.ConfigurationPrepared {
			t.Fatal("configuration versus execution coverage misreported")
		}
		if row.PilotWhitelistMember {
			covered++
		}
	}
	if covered != p.PilotWhitelistPrimaryCount || covered >= 1220 {
		t.Fatal("pilot whitelist gap hidden")
	}
	if run(offlineArgs(out)) == 0 {
		t.Fatal("existing output overwritten")
	}
	after, _ := os.ReadFile(out)
	if string(after) != string(raw) {
		t.Fatal("output changed")
	}
}

func TestFullPreflightRejectsExecutionAndIncompleteInputs(t *testing.T) {
	for _, extra := range [][]string{{"-execute"}, {"-api-key", "secret"}, {"-provider", "https://example.com"}, {"extra"}} {
		out := filepath.Join(t.TempDir(), "probe.json")
		args := append(offlineArgs(out), extra...)
		if run(args) == 0 {
			t.Fatal("execution or unrecognized input accepted")
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatal("invalid inputs created output")
		}
	}
	out := filepath.Join(t.TempDir(), "probe.json")
	args := offlineArgs(out)
	args[5] = "not-a-revision"
	if run(args) == 0 {
		t.Fatal("invalid provenance accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("invalid revision created output")
	}
}
