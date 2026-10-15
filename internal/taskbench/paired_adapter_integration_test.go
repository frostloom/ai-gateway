package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
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
	observer, capture := pairedTestObserver(t)
	var traceIDs []string
	var reports []SmallPairReport
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
	for _, key := range []string{"balance-001/answer_complete", "balance-020/explicit_scope_followup", "recharge-002/confirm", "recharge-002/cancel"} {
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
					Messages []struct {
						Role    string `json:"role"`
						Content []struct {
							Type    string `json:"type"`
							Text    string `json:"text"`
							Content string `json:"content"`
						} `json:"content"`
					} `json:"messages"`
					Max      int `json:"max_tokens"`
					Thinking struct {
						Type string `json:"type"`
					} `json:"thinking"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.Max != 128 || request.Thinking.Type != "disabled" || r.URL.Path != "/v1/messages" {
					t.Error("wrong Anthropic budget/thinking/protocol")
				}
				content := []any{map[string]any{"type": "tool_use", "id": "local-call", "name": tool, "input": args}}
				stop := "tool_use"
				if len(request.Messages) > 0 {
					for _, block := range request.Messages[len(request.Messages)-1].Content {
						if block.Type == "tool_result" {
							content = []any{map[string]any{"type": "text", "text": block.Content}}
							stop = "end_turn"
						} else if block.Type == "text" && block.Text == "看看我还能用多少" {
							content = []any{map[string]any{"type": "text", "text": "请问您要查询人民币现金余额，还是套餐token额度？"}}
							stop = "end_turn"
						}
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"model": "local", "content": content, "stop_reason": stop, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
			}))
			defer provider.Close()
			model := PairModelSettings{BaseURL: provider.URL, APIKey: "local-only", Model: "local", Protocol: "anthropic"}
			cfg, e := PrepareAgentPairConfig(suite, env.profiles, model, "local_stub", "test-revision", "")
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			limits := pairedLimits()
			limits.MaxCalls = 8
			limits.MaxRequestBytes = 16384
			limits.InputTokenCeiling = 16384
			campaign, e := OpenCampaignBudget(filepath.Join(dir, "campaign"), conversationHash([]byte("local campaign")), CampaignLimits{MaxBatches: 1, MaxModelCalls: 8, MaxModelMicroCNY: 1000000, MaxJEVCalls: 4, MaxJEVMicroUSD: 20000, JEVMicroUSDPerAttempt: 2688})
			if e != nil {
				t.Fatal(e)
			}
			defer campaign.Close()
			lease, e := campaign.ReserveBatch("batch-001", PairRunContract(cfg), limits, 4)
			if e != nil {
				t.Fatal(e)
			}
			dir = lease.Dir
			budget, e := OpenPairBudget(filepath.Join(dir, "budget.jsonl"), PairRunContract(cfg), limits, http.DefaultTransport)
			if e != nil {
				t.Fatal(e)
			}
			defer budget.Close()
			confidence := .99
			if key == "balance-020/explicit_scope_followup" {
				confidence = .5
			}
			opts := AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 4, DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Config: cfg, Environment: env, Budget: budget, Model: model, JEVFactory: func() jev.Evaluator {
				return jev.NewFake(localJevScript(tool, confidence), localJevScript(tool, confidence))
			}}
			path := filepath.Join(dir, "run.jsonl")
			opts.Observer = observer
			e = RunCampaignBatch(ctx, lease, opts)
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
			for _, row := range r.Rows {
				if len(row.LangfuseTraceID) != 32 {
					t.Fatal("missing task trace ID")
				}
				traceIDs = append(traceIDs, row.LangfuseTraceID)
			}
			reports = append(reports, r)
			before := calls.Load()
			if e = RunCampaignBatch(ctx, lease, opts); e == nil || calls.Load() != before {
				t.Fatal("campaign lease reused provider")
			}
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
	checkPairedObservations(t, observer, capture, traceIDs, reports)
}
