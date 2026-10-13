package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/redis/go-redis/v9"
)

type agentConversationOracle struct{ *storeConversationDouble }

func (o *agentConversationOracle) State(ctx context.Context, _ Fixture, _ string) (ConversationState, error) {
	return o.snapshot(ctx)
}
func (o *agentConversationOracle) Pending(ctx context.Context, _ Fixture, session string) (*Pending, error) {
	if session == "" {
		return nil, nil
	}
	raw, err := o.rdb.Get(ctx, "agent:session:"+session).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("server session missing")
	}
	if err != nil {
		return nil, err
	}
	var stored struct {
		Pending *Pending `json:"pending_action"`
	}
	err = json.Unmarshal(raw, &stored)
	return stored.Pending, err
}
func localToolMessage(tool string, args map[string]any) agent.Message {
	raw, _ := json.Marshal(args)
	call := agent.ToolCall{ID: "local-tool", Type: "function"}
	call.Function.Name = tool
	call.Function.Arguments = string(raw)
	return agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{call}}
}
func localJevScript(intent string, confidence float64) jev.FakeScript {
	return jev.FakeScript{Answers: map[string]jev.Answer{"intent": jev.AnswerChoice(intent, confidence), "legit": jev.AnswerNoul(.99), "risk": jev.AnswerScore(.1, .99, []string{"normal", "caution", "high"}), "route_shape": jev.AnswerChoice("single_read", .99)}}
}

func TestConversationRealAgentLocalDependencies(t *testing.T) {
	suite, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	db, rdb, st := runtimeProfileTestDatabase(t)
	profiles := loadRuntimeProfileArchive(t)
	plans := seedRuntimeCatalogue(t, db, profiles)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	token := "local-conversation-control"
	t.Setenv(controlplane.Env, token)
	billingServer := httptest.NewServer(admin.Handler(st, log, token))
	defer billingServer.Close()
	type selection struct {
		name, mode, tool, branch string
		confidence               float64
	}
	selections := []selection{{"read_model", "model", "get_balance", "", 0}, {"read_high", "jev_selective", "get_balance", "", .99}, {"read_low", "jev_selective", "get_balance", "", .5}}
	for _, mode := range []string{"model", "jev_selective"} {
		for _, branch := range []string{"confirm", "cancel", "duplicate_confirm", "cancel_then_confirm", "supplement_then_confirm"} {
			selections = append(selections, selection{"recharge_" + branch + "_" + mode, mode, "recharge", branch, .99})
		}
		selections = append(selections, selection{"refund_cap_" + mode, mode, "refund_recharge", "balance_cap", .99})
	}
	for _, tc := range selections {
		t.Run(tc.name, func(t *testing.T) {
			var c ConversationCase
			if tc.tool == "get_balance" {
				c = ConversationCase{SourceID: "local-agent-balance", Scenario: ConversationScenario{ID: "read", Profile: "no_subscription", MeasurementScope: "complete_task", FullTaskMetricEligible: true, Turns: []ConversationRule{{User: "查一下余额", ExpectedActions: []string{"read"}, AllowedTools: []string{"get_balance"}, BusinessStateUnchanged: true, NoWrite: true}}, Terminal: map[string]any{"business_state_unchanged": true, "authorized_write_count": float64(0)}}}
			} else {
				family := "recharge-"
				if tc.tool == "refund_recharge" {
					family = "refund-"
				}
				for _, candidate := range suite.Cases {
					if strings.HasPrefix(candidate.SourceID, family) && candidate.Scenario.Operation != nil && candidate.Scenario.Operation.Tool == tc.tool && candidate.Scenario.ID == tc.branch {
						c = candidate
						break
					}
				}
			}
			if c.SourceID == "" {
				t.Fatal("frozen branch absent")
			}
			oracle := &agentConversationOracle{&storeConversationDouble{t: t, db: db, rdb: rdb, st: st, profiles: profiles, plans: plans}}
			// The script is a dependency double, never the expected behavior score.
			var messages []agent.Message
			if tc.tool == "get_balance" {
				messages = []agent.Message{localToolMessage("get_balance", map[string]any{}), {Role: "assistant", Content: "当前可用余额 ¥5000.00；账本口径 ¥5000.00，两者一致。"}}
			} else {
				args := map[string]any{"amount_money": c.Scenario.Operation.Arguments["amount_money"]}
				if c.Scenario.Turns[0].NecessaryClarification {
					messages = append(messages, agent.Message{Role: "assistant", Content: "请提供办理金额（元）。"})
				}
				messages = append(messages, localToolMessage(tc.tool, args))
				if tc.branch == "duplicate_confirm" || tc.branch == "cancel_then_confirm" {
					messages = append(messages, agent.Message{Role: "assistant", Content: "当前没有待确认操作，未执行任何操作。"})
				}
			}
			var calls atomic.Int64
			llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Error("unexpected local model protocol")
				}
				n := int(calls.Add(1)) - 1
				if n >= len(messages) {
					http.Error(w, "local script exhausted", 500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": messages[n]}}})
			}))
			defer llmServer.Close()
			h := agent.NewHandler(nil, agent.NewToolbox(agent.NewAdminClient(billingServer.URL)), agent.NewLLMClient(llmServer.URL, "local-only-not-a-provider-key", "local-script"), st, rdb, log)
			if err := h.SetRoutingMode(tc.mode); err != nil {
				t.Fatal(err)
			}
			if tc.mode == "jev_selective" {
				scripts := []jev.FakeScript{}
				for range c.Scenario.Turns {
					scripts = append(scripts, localJevScript(tc.tool, tc.confidence))
				}
				h.SetJev(jev.NewFake(scripts...))
			}
			h.SetAdminAccess(token, 0)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("X-Agent-Internal", token)
				r.Header.Set("X-Agent-Tenant", fmt.Sprint(oracle.f.TenantID))
				h.ServeHTTP(w, r)
			})
			driver, err := NewHandlerConversationDriver(handler, oracle)
			if err != nil {
				t.Fatal(err)
			}
			result, facts := RunFactCheckedConversation(ctx, c, driver)
			if !result.StructuralPass || result.Status != "needs_review" || result.Success || factMismatch(facts) {
				t.Fatalf("agent chain failed: errors=%v facts=%+v", result.Errors, facts)
			}
			var observedCalls int64
			for _, obs := range result.Observations {
				if !json.Valid(obs.Turn.TelemetryRaw) || !strings.Contains(string(obs.Turn.TelemetryRaw), `"runtime"`) {
					t.Fatal("missing actual runtime telemetry")
				}
				var tele agent.TurnTelemetry
				if err := json.Unmarshal(obs.Turn.TelemetryRaw, &tele); err != nil {
					t.Fatal(err)
				}
				observedCalls += int64(tele.LLMCalls)
				if tc.mode == "model" && (tele.JevEnabled || tele.JevCalls != 0) {
					t.Fatal("V1 called JEV")
				}
			}
			if observedCalls != calls.Load() {
				t.Fatalf("telemetry=%d actual local attempts=%d", observedCalls, calls.Load())
			}
			if tc.name == "read_high" && calls.Load() != 0 {
				t.Fatal("high-confidence read called model")
			}
			if tc.name == "read_low" && calls.Load() != 2 {
				t.Fatal("low-confidence read did not fall back")
			}
			if tc.branch == "supplement_then_confirm" && result.Observations[0].Action != "unreviewed" {
				t.Fatal("clarification label invented")
			}
			t.Logf("local real-agent structural pass; pending answer/action review; local model requests=%d; no external provider/JEV request", calls.Load())
		})
	}
}
