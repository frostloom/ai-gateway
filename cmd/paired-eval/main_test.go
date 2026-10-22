package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

func campaignCLIArgs(out, plan, base string) []string {
	return []string{"-mode", "campaign-preflight", "-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-profiles", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", "-model-base", base, "-model-name", "deepseek-v4.1-flash", "-protocol", "anthropic", "-revision", "reviewed-revision", "-jev-model", "jev-1.13.0", "-campaign-plan", plan, "-out", out}
}

func TestPairedCLICampaignPreflightOffline(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	t.Setenv("AGENT_LLM_API_KEY", "do-not-read-this-key")
	t.Setenv("JEV_API_KEY", "do-not-read-this-jev-key")
	dir := t.TempDir()
	out := filepath.Join(dir, "proposal.json")
	args := campaignCLIArgs(out, "../../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json", srv.URL)
	if run(args) != 0 {
		t.Fatal("campaign preflight rejected")
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var proposal taskbench.CampaignProposal
	if json.Unmarshal(raw, &proposal) != nil || proposal.PlannedCases != 6 || proposal.PlannedArms != 12 || proposal.PlannedUserTurns != 26 || proposal.ModelReservedMicroCNY != 811008 || proposal.JEVReservedMicroUSD != 16128 || proposal.AllowPaid || proposal.ReadyForPaidRun || proposal.ProviderCalls != 0 || proposal.AuthorizedSpending != nil {
		t.Fatal("incorrect offline proposal")
	}
	if strings.Contains(string(raw), "do-not-read-this") {
		t.Fatal("discovered environment key")
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0600 {
		t.Fatal("proposal is not private")
	}
	if run(args) == 0 {
		t.Fatal("overwrote proposal")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || calls.Load() != 0 {
		t.Fatal("preflight created runtime artifacts or called provider")
	}
}

func TestPairedCLICampaignPreflightRejectsInvalidInputs(t *testing.T) {
	planPath := "../../eval/paired-pilots/2026-10-22-campaign-preflight/plan.json"
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"profiles", "model-base", "model-name", "protocol", "revision", "jev-model", "campaign-plan"} {
		t.Run("missing-"+field, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "proposal.json")
			original := campaignCLIArgs(out, planPath, "https://provider.invalid")
			args := []string{}
			for i := 0; i < len(original); i += 2 {
				if original[i] != "-"+field {
					args = append(args, original[i:i+2]...)
				}
			}
			if run(args) == 0 {
				t.Fatal("missing explicit flag accepted")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("invalid input created output")
			}
		})
	}
	for _, mode := range []string{"prepare", "preflight", "report", "live"} {
		t.Run("wrong-mode-"+mode, func(t *testing.T) {
			args := campaignCLIArgs(filepath.Join(t.TempDir(), "out.json"), planPath, "https://provider.invalid")
			args[1] = mode
			if run(args) == 0 {
				t.Fatal("campaign plan accepted outside campaign mode")
			}
		})
	}
	invalid := map[string]string{
		"unknown":        strings.Replace(string(raw), "\"schema\":", "\"allow_paid\":true,\"schema\":", 1),
		"nested-unknown": strings.Replace(string(raw), "\"max_calls\":", "\"ignore_budget\":true,\"max_calls\":", 1),
		"trailing":       string(raw) + "{}",
		"malformed":      "{",
		"null":           "null",
		"over-budget":    strings.Replace(string(raw), "\"max_model_calls\": 24", "\"max_model_calls\": 23", 1),
	}
	for name, data := range invalid {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "plan.json")
			out := filepath.Join(dir, "out.json")
			if err := os.WriteFile(input, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if run(campaignCLIArgs(out, input, "https://provider.invalid")) == 0 {
				t.Fatal("invalid plan accepted")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("invalid plan created output")
			}
		})
	}
}

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

func TestPairedCLIExplicitPreflightIsOfflineAndNotPaidReady(t *testing.T) {
	p := filepath.Join(t.TempDir(), "preflight.json")
	args := []string{"-mode", "preflight", "-definitions", "../../eval/task-definitions/2026-10-12-full-dev-v1", "-profiles", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", "-model-base", "https://maas.qianwenaiapi.com/apps/anthropic", "-model-name", "deepseek-v4.1-flash", "-protocol", "anthropic", "-revision", "reviewed-revision", "-jev-model", "jev-1.13.0", "-out", p}
	if n := run(args); n != 0 {
		t.Fatalf("preflight exit %d", n)
	}
	raw, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Ready         bool                    `json:"ready_for_paid_run"`
		Calls         int                     `json:"provider_calls"`
		Configuration taskbench.PairRunConfig `json:"configuration"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Ready || v.Calls != 0 || v.Configuration.AllowPaid || len(v.Configuration.Suite.Cases) != 12 || v.Configuration.ProviderEndpointSHA256 == "" {
		t.Fatal("preflight authorized or missing bindings")
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
