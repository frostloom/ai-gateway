package taskbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestRecordedAgentPairRequiresObserverBeforeExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	env, err := OpenPairedFixtureEnvironment(ctx, PairedFixtureOptions{
		AllowFixtureCreation: true,
		AdminDSN:             "root:root@tcp(127.0.0.1:3307)/",
		ProfilesDir:          "../../eval/runtime-profiles/2026-10-13-six-profiles-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Close(); err != nil {
			t.Error(err)
		}
	})
	var modelCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"model": "local-test", "stop_reason": "end_turn",
			"content": []any{map[string]any{"type": "text", "text": "本地测试回复"}},
			"usage":   map[string]int{"input_tokens": 100, "output_tokens": 8},
		})
	}))
	t.Cleanup(provider.Close)
	defs := "../../eval/task-definitions/2026-10-12-full-dev-v1"
	source, err := LoadConversations(defs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareSmallPair(source)
	if err != nil {
		t.Fatal(err)
	}
	suite := plan.Suite
	suite.Cases = suite.Cases[:1]
	model := PairModelSettings{BaseURL: provider.URL, APIKey: "local-only", Model: "local-test", Protocol: "anthropic"}
	// Exercise the recorded-run gate using only loopback model/JEV doubles.
	cfg, err := PrepareAgentPairConfig(suite, env.profiles, model, "recorded_run", "test-revision", "test-judge")
	if err != nil {
		t.Fatal(err)
	}
	cfg.AllowPaid = true
	dir := t.TempDir()
	limits := pairedLimits()
	limits.MaxCalls = 8
	limits.MaxRequestBytes = 16384
	limits.InputTokenCeiling = 16384
	limits.MaxMicroCNY = 1000000
	budget, err := OpenPairBudget(filepath.Join(dir, "budget.jsonl"), PairRunContract(cfg), limits, provider.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := budget.Close(); err != nil {
			t.Error(err)
		}
	})
	judgeConstructions := 0
	path := filepath.Join(dir, "run.jsonl")
	err = RunAgentPair(ctx, path, AgentPairOptions{
		AllowExecution: true, RatesVerified: true, AllowJEVCalls: true, MaxJEVCalls: 2,
		DefinitionsDir: defs, Config: cfg, Environment: env, Budget: budget, Model: model,
		JEVFactory: func() jev.Evaluator {
			judgeConstructions++
			return jev.NewFake()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "explicit Langfuse observer") {
		t.Errorf("missing observer was not rejected: %v", err)
	}
	if modelCalls.Load() != 0 || judgeConstructions != 0 || budget.Snapshot().Calls != 0 {
		t.Errorf("unobserved execution reached providers: model=%d JEV factories=%d budget=%d", modelCalls.Load(), judgeConstructions, budget.Snapshot().Calls)
	}
	for _, name := range []string{path, path + ".lock", path + ".jev.jsonl", path + ".jev.jsonl.lock"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("rejected run created execution evidence %s: %v", filepath.Base(name), err)
		}
	}
}
