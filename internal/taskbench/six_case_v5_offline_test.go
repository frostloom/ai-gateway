package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/jev"
)

// A single frozen cohort/journal per path: no historical paid scope is rebound.
// All provider responses and JEV verdicts are local dependency scripts.
func TestV5SixCaseOfflineAcceptance(t *testing.T) {
	for _, mode := range []string{"normal", "repair", "missing-fresh-read", "wrong-refund-amount"} {
		t.Run(mode, func(t *testing.T) {
			o := localCampaignOptions(t)
			source, err := LoadConversations(o.DefinitionsDir)
			if err != nil {
				t.Fatal(err)
			}
			small, err := PrepareSmallPair(source)
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{"balance-001/answer_complete", "balance-020/explicit_scope_followup", "recharge-002/confirm", "recharge-002/cancel", "recharge-001/supplement_then_confirm", "refund-001/balance_cap"}
			suite := small.Suite
			suite.Cases = nil
			for _, key := range keys {
				for _, c := range small.Suite.Cases {
					if c.Key() == key {
						suite.Cases = append(suite.Cases, c)
					}
				}
			}
			if len(suite.Cases) != 6 {
				t.Fatal("original six cases missing")
			}
			for i, c := range suite.Cases {
				if c.Key() != keys[i] {
					t.Fatal("selection changed")
				}
			}
			model := PairModelSettings{BaseURL: "http://127.0.0.1/six-v5", APIKey: "synthetic-only", Model: "deepseek-flash", Protocol: "openai"}
			cfg, err := PrepareAgentPairConfig(suite, o.Environment.profiles, model, "local_stub", "v5-six-offline", "jev-1.13.0")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AllowPaid {
				t.Fatal("local suite enabled paid execution")
			}
			calls := 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1/six-v5/chat/completions" {
					t.Fatal("nonlocal target")
				}
				raw, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				var body struct {
					Messages []agent.Message `json:"messages"`
					Max      int             `json:"max_tokens"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 {
					t.Fatal("unbounded request")
				}
				calls++
				first, last := "", ""
				for _, m := range body.Messages {
					if m.Role == "user" {
						if first == "" {
							first = m.Content
						}
						last = m.Content
					}
				}
				var selected *ConversationCase
				for i := range suite.Cases {
					c := &suite.Cases[i]
					if c.Scenario.Turns[0].User == first {
						selected = c
						break
					}
				}
				if selected == nil {
					t.Fatal("request outside frozen cohort")
				}
				tool := "get_balance"
				if !strings.HasPrefix(selected.Key(), "balance-") {
					if selected.Scenario.Operation == nil {
						t.Fatal("write operation missing")
					}
					tool = selected.Scenario.Operation.Tool
				}
				msg := contentV5("未执行")
				if body.Messages[len(body.Messages)-1].Role == "tool" {
					msg = contentV5(body.Messages[len(body.Messages)-1].Content)
				} else if tool == "get_balance" {
					followup := selected.Key() == "balance-020/explicit_scope_followup" && last != first
					if followup && mode == "missing-fresh-read" {
						msg = contentV5("本轮未查询，请沿用前述结果。")
					} else if followup && mode == "repair" {
						msg = contentV5("当前现金余额为 ¥5000.00。")
					} else {
						msg = localToolMessage("get_balance", map[string]any{})
					}
				} else if !strings.Contains(last, "100") {
					if tool == "refund_recharge" {
						msg = contentV5("请问您要退款多少元，或请提供本人已支付充值订单号。")
					} else {
						msg = contentV5("请问您要充值多少元？")
					}
				} else if mode == "repair" && !strings.Contains(string(raw), "【纠正】") {
					if tool == "refund_recharge" {
						msg = contentV5("退款成功：退回 ¥100。")
					} else {
						msg = contentV5("充值成功：到账 ¥100。")
					}
				} else {
					amount := 100
					if mode == "wrong-refund-amount" && tool == "refund_recharge" {
						amount = 200
					}
					msg = localToolMessage(tool, map[string]any{"amount_money": amount})
				}
				response, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
			})
			dir := t.TempDir()
			// Local dependency ceiling only; this is not an authorization or paid estimate.
			limits := PairBudgetLimits{MaxCalls: 64, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: 2162688, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
			budget, err := OpenPairBudget(filepath.Join(dir, "budget.jsonl"), PairRunContract(cfg), limits, up)
			if err != nil {
				t.Fatal(err)
			}
			defer budget.Close()
			deps, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer deps.close()
			scripts := []jev.FakeScript{localJevScript("get_balance", .99), localJevScript("get_balance", .5), localJevScript("recharge", .99), localJevScript("recharge", .99), localJevScript("recharge", .99), localJevScript("refund_recharge", .99)}
			path := filepath.Join(dir, "run.jsonl")
			err = RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 6, StopOnJEVFailure: true, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: deps.observer, JEVFactory: func() jev.Evaluator { return authorizedTestJEV{jev.NewFake(scripts...), true} }})
			if err != nil {
				t.Fatal(err)
			}
			report, err := ReportPreparedPair(path)
			if err != nil {
				t.Fatal(err)
			}
			if report.FinishedArms != 12 || report.UnrunArms != 0 {
				t.Fatal("incomplete six-case cohort")
			}
			reportBytes, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "report.json"), reportBytes, 0600); err != nil {
				t.Fatal(err)
			}
			journal, err := loadPairJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			tenants := map[uint64]bool{}
			failed := 0
			for _, c := range suite.Cases {
				for _, arm := range []string{"model", "jev_selective"} {
					v := journal.finished[c.Key()+":"+arm].Result
					if v == nil || len(v.Observations) != len(c.Scenario.Turns) {
						t.Fatal("missing case rounds")
					}
					if tenants[v.Fixture.TenantID] {
						t.Fatal("mutated fixture reused across arms")
					}
					tenants[v.Fixture.TenantID] = true
					for _, obs := range v.Observations {
						mt := obs.Turn.Response.Telemetry
						if mt == nil || mt.Runtime.ContractSHA256 != "a95cacd041830012619a0f4d7fd8208c184aa9e8d45b7ee05afaa239a30e1e8f" || mt.Runtime.Model != "deepseek-flash" || mt.Runtime.Protocol != "openai" || mt.Runtime.PromptSHA256 != cfg.Model.PromptSHA256 || mt.Runtime.ToolsSHA256 != cfg.Model.ToolSchemaSHA256 {
							t.Fatal("mixed runtime/prompt/tool/target")
						}
						if !reflect.DeepEqual(v.Initial.Controls, obs.State.Controls) || !reflect.DeepEqual(v.Initial.Allowance, obs.State.Allowance) {
							t.Fatal("control/allowance mutation")
						}
					}
					probeFailed := (mode == "missing-fresh-read" && c.Key() == keys[1]) || (mode == "wrong-refund-amount" && c.Key() == keys[5])
					if probeFailed {
						if v.StructuralPass || v.Status != "failed" || v.Success || v.HTTPFailures != 0 {
							t.Fatalf("negative probe escaped: %s %s errors=%v", mode, arm, v.Errors)
						}
						if mode == "missing-fresh-read" {
							last := v.Observations[len(v.Observations)-1]
							if len(last.Turn.Response.Trace) != 0 || last.Pending != nil || !v5BusinessEqual(v.Initial.Own, v.Final.Own) || !strings.Contains(strings.Join(v.Errors, " "), "read action lacks a successful read trace") {
								t.Fatal("negative fresh read changed state")
							}
						} else {
							preview := v.Observations[len(v.Observations)-2]
							if preview.Pending == nil || conversationInteger(preview.Pending.Args["amount_money"]) != 200 || !strings.Contains(strings.Join(v.Errors, " "), "preview trace parameters mismatch") || !strings.Contains(strings.Join(v.Errors, " "), "pending identity/parameters mismatch") {
								t.Fatalf("wrong requested amount not diagnosed: %v", v.Errors)
							}
						}
						failed++
						continue
					}
					if !v.StructuralPass || v.Status != "needs_review" || v.Success {
						t.Fatalf("case failed %s %s %v", c.Key(), arm, v.Errors)
					}
					assertV5SixCaseState(t, c, arm, mode, v)
				}
			}
			if (mode == "normal" || mode == "repair") && failed != 0 {
				t.Fatal("valid path failed")
			}
			if (mode == "missing-fresh-read" || mode == "wrong-refund-amount") && failed != 2 {
				t.Fatal("negative two-arm coverage missing")
			}
			jevRaw, err := os.ReadFile(path + ".jev.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			jevCalls := 0
			for _, line := range strings.Split(strings.TrimSpace(string(jevRaw)), "\n") {
				var e pairJEVEvent
				if json.Unmarshal([]byte(line), &e) != nil {
					t.Fatal("invalid local JEV journal")
				}
				if e.Kind == "reserve" {
					jevCalls++
				}
			}
			if jevCalls != 6 {
				t.Fatal("bounded six FakeJEV calls missing")
			}
			before := calls
			if err = RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 6, StopOnJEVFailure: true, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: deps.observer, JEVFactory: func() jev.Evaluator { return authorizedTestJEV{jev.NewFake(scripts...), true} }}); err != nil || calls != before {
				t.Fatalf("local journal resume repeated dependency or failed: %v", err)
			}
			t.Logf("mode=%s arms=12 turns=%d model=%d FakeJEV=6 task_failed=%d external=0", mode, v5TurnCount(suite), calls, failed)
		})
	}
}
func contentV5(s string) agent.Message { return agent.Message{Role: "assistant", Content: s} }
func v5BusinessEqual(a, b Snapshot) bool {
	a.Audits = nil
	b.Audits = nil
	return reflect.DeepEqual(a, b)
}
func assertV5SixCaseState(t *testing.T, c ConversationCase, arm, mode string, v *ConversationResult) {
	t.Helper()
	key := c.Key()
	last := v.Observations[len(v.Observations)-1]
	if !reflect.DeepEqual(v.Initial.Own.Subscriptions, v.Final.Own.Subscriptions) {
		t.Fatal("subscription changed")
	}
	if strings.HasPrefix(key, "balance-") {
		if !v5BusinessEqual(v.Initial.Own, v.Final.Own) {
			t.Fatal("balance case wrote business state")
		}
		for _, obs := range v.Observations {
			reads := 0
			for _, tr := range obs.Turn.Response.Trace {
				if tr.Tool == "get_balance" && tr.Phase == "read" && tr.Outcome == "success" {
					reads++
				}
			}
			if reads != 1 || !strings.Contains(obs.Turn.Response.Reply, "5000.00") {
				t.Fatal("missing authoritative cash read/reply")
			}
		}
		if key == "balance-020/explicit_scope_followup" && mode == "repair" {
			found := 0
			for _, a := range v.Final.Own.Audits {
				if strings.Contains(a.Guard, "cash_read_recovery") {
					found++
				}
			}
			if found != 1 || last.Turn.Response.Telemetry.ReplyRepairs != 1 {
				t.Fatal("cash correction did not recover one audited read")
			}
		}
		if key == "balance-001/answer_complete" && arm == "jev_selective" {
			if !last.Turn.Response.Telemetry.JevFastPath || last.Turn.Response.Telemetry.LLMCalls != 0 {
				t.Fatal("first balance fastpath lost")
			}
		}
		return
	}
	for _, obs := range v.Observations[:len(v.Observations)-1] {
		if !v5BusinessEqual(v.Initial.Own, obs.State.Own) {
			t.Fatal("write before confirmation")
		}
	}
	mt := last.Turn.Response.Telemetry
	if last.Pending != nil || mt.LLMCalls != 0 || mt.JevCalls != 0 {
		t.Fatal("deterministic confirmation/cancel changed")
	}
	if key == "recharge-002/cancel" {
		cancelAudits := 0
		for _, a := range v.Final.Own.Audits {
			if a.Tool == "cancel_confirmation" && a.Result == "cancelled" && !a.Confirmed {
				cancelAudits++
			}
		}
		if !v5BusinessEqual(v.Initial.Own, v.Final.Own) || len(last.Turn.Response.Trace) != 0 || cancelAudits != 1 {
			t.Fatal("cancel not cleared safely")
		}
		return
	}
	preview := v.Observations[len(v.Observations)-2]
	if preview.Pending == nil || conversationInteger(preview.Pending.Args["amount_money"]) != 100 || preview.Pending.TenantID != v.Fixture.TenantID {
		t.Fatal("unbound preview amount/owner")
	}
	writes := 0
	for _, tr := range last.Turn.Response.Trace {
		if tr.Outcome == "success" && tr.Confirmed && tr.Phase == "execute" && tr.Tool == c.Scenario.Operation.Tool {
			writes++
		}
	}
	if writes != 1 {
		t.Fatal("not exactly one confirmed write")
	}
	delta := int64(10000)
	if key == "refund-001/balance_cap" {
		delta = -2500
		if !reflect.DeepEqual(v.Initial.Own.Orders, v.Final.Own.Orders) || !strings.Contains(preview.Turn.Response.Reply, "实际退款 ¥25.00") || !strings.Contains(last.Turn.Response.Reply, "退回 ¥25.00") {
			t.Fatal("refund cap/order/reply mismatch")
		}
	}
	if v.Final.Own.BalanceCent-v.Initial.Own.BalanceCent != delta || v.Final.Own.LedgerBalance-v.Initial.Own.LedgerBalance != delta || !v.Final.Own.Consistent || len(v.Final.Own.Topups) != len(v.Initial.Own.Topups)+1 {
		t.Fatal("incorrect business delta")
	}
	for _, old := range v.Initial.Own.Orders {
		found := false
		for _, now := range v.Final.Own.Orders {
			if old.ID == now.ID {
				found = reflect.DeepEqual(old, now)
			}
		}
		if !found {
			t.Fatal("unrelated order changed")
		}
	}
	if delta > 0 && len(v.Final.Own.Orders) != len(v.Initial.Own.Orders)+1 {
		t.Fatal("recharge order missing")
	}
}

func v5TurnCount(s ConversationSuite) int {
	n := 0
	for _, c := range s.Cases {
		n += 2 * len(c.Scenario.Turns)
	}
	return n
}
