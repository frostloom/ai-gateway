package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/jev"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentPairOwnedEnvironmentLocalReadAndWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	env, e := OpenPairedFixtureEnvironment(ctx, PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := env.Close(); err != nil {
			t.Error(err)
		}
	})
	source, e := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := PrepareSmallPair(source)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"balance-001/answer_complete", "recharge-002/confirm"} {
		t.Run(key, func(t *testing.T) {
			var selected ConversationCase
			for _, c := range plan.Suite.Cases {
				if c.Key() == key {
					selected = c
				}
			}
			suite := plan.Suite
			suite.Cases = []ConversationCase{selected}
			var calls atomic.Int64
			tool := "get_balance"
			args := map[string]any{}
			if selected.Scenario.Operation != nil {
				tool = "recharge"
				args = map[string]any{"amount_money": 100}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					Messages []agent.Message `json:"messages"`
					Max      int             `json:"max_tokens"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.Max != 128 {
					t.Error("unbounded model request")
				}
				msg := localToolMessage(tool, args)
				if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "tool" {
					msg = agent.Message{Role: "assistant", Content: request.Messages[len(request.Messages)-1].Content}
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
			}))
			defer provider.Close()
			model := PairModelSettings{BaseURL: provider.URL, APIKey: "local-only", Model: "local", Protocol: "openai"}
			cfg, e := PrepareAgentPairConfig(suite, env.profiles, model, "local_stub", "test-revision", "")
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			limits := pairedLimits()
			limits.MaxCalls = 6
			limits.MaxRequestBytes = 16384
			limits.InputTokenCeiling = 16384
			budget, e := OpenPairBudget(filepath.Join(dir, "budget"), PairRunContract(cfg), limits, http.DefaultTransport)
			if e != nil {
				t.Fatal(e)
			}
			defer budget.Close()
			opts := AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 4, DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Config: cfg, Environment: env, Budget: budget, Model: model, JEVFactory: func() jev.Evaluator { return jev.NewFake(localJevScript(tool, .99), localJevScript(tool, .99)) }}
			path := filepath.Join(dir, "run")
			e = RunAgentPair(ctx, path, opts)
			if e != nil {
				t.Fatal(e)
			}
			r, e := ReportPreparedPair(path)
			if e != nil {
				t.Fatal(e)
			}
			if r.FinishedArms != 2 || r.Rows[0].Status != "needs_review" || r.Rows[1].Status != "needs_review" {
				t.Fatalf("wrong report: %+v", r)
			}
			before := calls.Load()
			if e = RunAgentPair(ctx, path, opts); e != nil || calls.Load() != before {
				t.Fatal("resume repeated model", e)
			}
			opts.Model.BaseURL = provider.URL + "/changed"
			if e = RunAgentPair(ctx, path, opts); e == nil || calls.Load() != before {
				t.Fatal("changed provider configuration called model")
			}
			j, e := loadPairJournal(path)
			if e != nil {
				t.Fatal(e)
			}
			left := j.finished[key+":model"].Result
			right := j.finished[key+":jev_selective"].Result
			if left.Fixture.TenantID == right.Fixture.TenantID {
				t.Fatal("mutated accounts reused")
			}
			opts.Model = model
			raw, err := os.ReadFile(path + ".jev.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path+".jev.jsonl", append(bytes.Split(raw, []byte{'\n'})[0], '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if e = RunAgentPair(ctx, path, opts); e == nil || calls.Load() != before {
				t.Error("truncated JEV ledger accepted or called provider")
			}
			t.Logf("%s: local model HTTP=%d, external=0; both semantic review pending", key, before)
		})
	}
	if e = env.Close(); e != nil {
		t.Fatal(e)
	}
}
