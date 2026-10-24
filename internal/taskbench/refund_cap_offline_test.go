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

	"github.com/frostloom/ai-gateway/internal/jev"
)

// Frozen task acceptance uses authoritative fixture state. A diagnostic that
// detects an unsupported success claim is a blocking finding, not task success.
func TestRefundCapOfflineAcceptance(t *testing.T) { testRefundCapOfflineAcceptance(t, false) }
func TestRefundCapRepairRequired(t *testing.T)    { testRefundCapOfflineAcceptance(t, true) }
func testRefundCapOfflineAcceptance(t *testing.T, requireRepair bool) {
	for _, mode := range []string{"normal", "unsupported-completion", "insufficient-cap"} {
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
			key := "refund-001/balance_cap"
			var selected ConversationCase
			for _, c := range small.Suite.Cases {
				if c.Key() == key {
					selected = c
				}
			}
			if selected.Key() != key {
				t.Fatal("frozen case missing")
			}
			suite := small.Suite
			suite.Cases = []ConversationCase{selected}
			model := PairModelSettings{BaseURL: "http://127.0.0.1/local-refund-cap", APIKey: "synthetic-only", Model: "deepseek-flash", Protocol: "openai"}
			cfg, err := PrepareAgentPairConfig(suite, o.Environment.profiles, model, "local_stub", "refund-cap-offline", "")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1/local-refund-cap/chat/completions" {
					t.Fatal("unexpected target")
				}
				raw, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				var body struct {
					Max      int                              `json:"max_tokens"`
					Thinking struct{ Type string }            `json:"thinking"`
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 {
					t.Fatal("output bound missing")
				}
				calls++
				last := ""
				for _, m := range body.Messages {
					if m.Role == "user" {
						last = m.Content
					}
				}
				msg := map[string]any{"role": "assistant", "content": "请提供要退款的金额（元），或本人已支付的充值订单号。"}
				if strings.Contains(last, "100") {
					msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "refund", "type": "function", "function": map[string]string{"name": "refund_recharge", "arguments": "{\"amount_money\":100}"}}}}
					if mode == "unsupported-completion" && !strings.Contains(string(raw), "【纠正】") {
						msg = map[string]any{"role": "assistant", "content": "退款成功：退回 ¥100。"}
					}
				}
				response, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
			})
			dir := t.TempDir()
			limits := PairBudgetLimits{MaxCalls: 12, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: 405504, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
			if mode == "insufficient-cap" {
				limits.MaxCalls = 3
				limits.MaxMicroCNY = 101376
			}
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
			path := filepath.Join(dir, "run.jsonl")
			err = RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 1, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: deps.observer, JEVFactory: func() jev.Evaluator { return jev.NewFake(localJevScript("refund_recharge", .99)) }})
			if err != nil {
				t.Fatal(err)
			}
			report, err := ReportPreparedPair(path)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := loadPairJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, arm := range []string{"model", "jev_selective"} {
				r := journal.finished[key+":"+arm].Result
				if r == nil {
					t.Fatal("missing arm")
				}
				if !reflect.DeepEqual(r.Initial.Controls, r.Final.Controls) || !reflect.DeepEqual(r.Initial.Allowance, r.Final.Allowance) {
					t.Fatal("unrelated state changed")
				}
				if r.Initial.Own.BalanceCent != 2500 || r.Initial.Own.LedgerBalance != 2500 {
					t.Fatal("wrong frozen low-balance fixture")
				}
				if mode == "insufficient-cap" && arm == "jev_selective" {
					a, b := r.Initial.Own, r.Final.Own
					a.Audits = nil
					b.Audits = nil
					if !reflect.DeepEqual(a, b) || r.Status != "failed" || r.HTTPFailures == 0 {
						t.Fatal("budget failure hidden or wrote business state")
					}
					continue
				}
				if mode == "unsupported-completion" && requireRepair && !r.StructuralPass {
					t.Fatalf("required refund correction missing: %v", r.Errors)
				}
				if mode == "unsupported-completion" && !r.StructuralPass {
					if r.Status != "failed" || r.Success || len(r.Observations) != 3 || !strings.Contains(strings.Join(r.Errors, " "), "write_completion") {
						t.Fatal("unsupported claim not preserved as failure")
					}
					for _, obs := range r.Observations {
						a, b := r.Initial.Own, obs.State.Own
						a.Audits = nil
						b.Audits = nil
						if obs.Turn.Status != 200 || !reflect.DeepEqual(a, b) || !reflect.DeepEqual(r.Initial.Controls, obs.State.Controls) || !reflect.DeepEqual(r.Initial.Allowance, obs.State.Allowance) {
							t.Fatal("diagnostic transport failure or business write")
						}
					}
					claim, confirm := r.Observations[1], r.Observations[2]
					if claim.Turn.Response.Reply != "退款成功：退回 ¥100。" || claim.Pending != nil || len(claim.Turn.Response.Trace) != 0 {
						t.Fatal("diagnostic failure unrelated to unsupported completion")
					}
					if confirm.Pending != nil || len(confirm.Turn.Response.Trace) != 0 || !strings.Contains(confirm.Turn.Response.Reply, "当前没有待确认操作") || confirm.Turn.Response.Telemetry == nil || confirm.Turn.Response.Telemetry.LLMCalls != 0 || confirm.Turn.Response.Telemetry.JevCalls != 0 {
						t.Fatal("unbound confirmation not no-op")
					}
					t.Logf("diagnostic blocking=true arm=%s errors=%v", arm, r.Errors)
					continue
				}
				if !r.StructuralPass || r.Status != "needs_review" || r.Success || len(r.Observations) != 3 {
					t.Fatalf("unexpected normal/future repair result: %v", r.Errors)
				}
				for _, obs := range r.Observations[:2] {
					a, b := r.Initial.Own, obs.State.Own
					a.Audits = nil
					b.Audits = nil
					if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(r.Initial.Controls, obs.State.Controls) || !reflect.DeepEqual(r.Initial.Allowance, obs.State.Allowance) {
						t.Fatal("write before confirmation")
					}
				}
				clarify, preview, confirm := r.Observations[0], r.Observations[1], r.Observations[2]
				if clarify.Pending != nil || clarify.Action != "unreviewed" || len(clarify.Turn.Response.Trace) != 0 || !strings.Contains(clarify.Turn.Response.Reply, "金额") {
					t.Fatal("necessary question missing")
				}
				if preview.Action != "preview" || preview.Pending == nil || preview.Pending.Tool != "refund_recharge" || conversationInteger(preview.Pending.Args["amount_money"]) != 100 || !strings.Contains(preview.Turn.Response.Reply, "实际退款 ¥25.00") {
					t.Fatal("preview cap/request mismatch")
				}
				if confirm.Action != "execute" || confirm.Pending != nil || confirm.Turn.Response.Telemetry == nil || confirm.Turn.Response.Telemetry.LLMCalls != 0 || confirm.Turn.Response.Telemetry.JevCalls != 0 || !strings.Contains(confirm.Turn.Response.Reply, "退回 ¥25.00") {
					t.Fatal("wrong confirmation")
				}
				if r.Final.Own.BalanceCent != 0 || r.Final.Own.LedgerBalance != 0 || !r.Final.Own.Consistent || !reflect.DeepEqual(r.Initial.Own.Orders, r.Final.Own.Orders) || !reflect.DeepEqual(r.Initial.Own.Subscriptions, r.Final.Own.Subscriptions) {
					t.Fatal("refund exceeded cap or changed unrelated order/subscription")
				}
				beforeIDs := map[uint64]bool{}
				for _, v := range r.Initial.Own.Topups {
					beforeIDs[v.ID] = true
				}
				added := 0
				var refunded int64
				for _, v := range r.Final.Own.Topups {
					if !beforeIDs[v.ID] {
						added++
						refunded -= v.Amount
					}
				}
				if added != 1 || refunded != 2500 {
					t.Fatalf("refund ledger count=%d amount=%d", added, refunded)
				}
				writes := 0
				for _, tr := range confirm.Turn.Response.Trace {
					if tr.Tool == "refund_recharge" && tr.Outcome == "success" && tr.Confirmed {
						writes++
					}
				}
				if writes != 1 {
					t.Fatal("not exactly one confirmed write")
				}
			}
			if calls != budget.Snapshot().Calls {
				t.Fatal("attempt count mismatch")
			}
			if err := os.WriteFile(filepath.Join(dir, "report.json"), mustRechargeJSON(t, report), 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s synthetic_model=%d statuses=%s/%s external=0", mode, calls, report.Rows[0].Status, report.Rows[1].Status)
		})
	}
}
