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

// Pins the complete frozen clarification/preview/confirmation flow, including
// real fixture state; dependency scripts never count as semantic quality proof.
func TestRechargeSupplementOfflineAcceptance(t *testing.T) {
	testRechargeSupplementOfflineAcceptance(t, false)
}
func TestRechargeSupplementRepairRequired(t *testing.T) {
	testRechargeSupplementOfflineAcceptance(t, true)
}
func testRechargeSupplementOfflineAcceptance(t *testing.T, requireRepair bool) {
	for _, mode := range []string{"normal", "repair", "insufficient-cap"} {
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
			key := "recharge-001/supplement_then_confirm"
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
			model := PairModelSettings{BaseURL: "http://127.0.0.1/local-supplement", APIKey: "synthetic-only", Model: "deepseek-flash", Protocol: "openai"}
			cfg, err := PrepareAgentPairConfig(suite, o.Environment.profiles, model, "local_stub", "supplement-offline", "")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1/local-supplement/chat/completions" {
					t.Fatal("unexpected target")
				}
				raw, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				var body struct {
					Max      int                              `json:"max_tokens"`
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 {
					t.Fatal("unbounded request")
				}
				last := ""
				for _, m := range body.Messages {
					if m.Role == "user" {
						last = m.Content
					}
				}
				calls++
				msg := map[string]any{"role": "assistant", "content": "请问您要充值多少元？"}
				if strings.Contains(last, "100") {
					msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "recharge", "type": "function", "function": map[string]string{"name": "recharge", "arguments": "{\"amount_money\":100}"}}}}
					if mode == "repair" && !strings.Contains(string(raw), "【纠正】") {
						msg = map[string]any{"role": "assistant", "content": "充值成功：到账 ¥100。"}
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
			maxJEV := 1
			path := filepath.Join(dir, "run.jsonl")
			err = RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: maxJEV, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: deps.observer, JEVFactory: func() jev.Evaluator {
				return jev.NewFake(localJevScript("recharge", .99), localJevScript("recharge", .99))
			}})
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
					t.Fatal("missing result")
				}
				if !reflect.DeepEqual(r.Initial.Controls, r.Final.Controls) || !reflect.DeepEqual(r.Initial.Allowance, r.Final.Allowance) {
					t.Fatal("unrelated state changed")
				}
				if mode == "repair" && requireRepair && !r.StructuralPass {
					t.Fatalf("required correction missing: %v", r.Errors)
				}
				if mode == "repair" && !r.StructuralPass {
					if len(r.Observations) != 3 {
						t.Fatal("diagnostic lost observed rounds")
					}
					for _, obs := range r.Observations {
						initial, current := r.Initial.Own, obs.State.Own
						initial.Audits = nil
						current.Audits = nil
						if obs.Turn.Status != 200 || !reflect.DeepEqual(initial, current) || !reflect.DeepEqual(r.Initial.Controls, obs.State.Controls) || !reflect.DeepEqual(r.Initial.Allowance, obs.State.Allowance) {
							t.Fatal("diagnostic transport failure or intermediate write")
						}
					}
					claim, confirm := r.Observations[1], r.Observations[2]
					if claim.Turn.Response.Reply != "充值成功：到账 ¥100。" || claim.Pending != nil || len(claim.Turn.Response.Trace) != 0 || !strings.Contains(strings.Join(r.Errors, " "), "write_completion") {
						t.Fatal("diagnostic no longer demonstrates unsupported completion")
					}
					if confirm.Pending != nil || len(confirm.Turn.Response.Trace) != 0 || !strings.Contains(confirm.Turn.Response.Reply, "当前没有待确认操作") || confirm.Turn.Response.Telemetry == nil || confirm.Turn.Response.Telemetry.LLMCalls != 0 || confirm.Turn.Response.Telemetry.JevCalls != 0 {
						t.Fatal("unbound confirmation was not a no-op")
					}

					initial, final := r.Initial.Own, r.Final.Own
					initial.Audits = nil
					final.Audits = nil
					if !reflect.DeepEqual(initial, final) || r.Status != "failed" || r.Success {
						t.Fatal("false completion probe produced unsafe writes or hidden failure")
					}
					t.Logf("diagnostic blocking=true arm=%s errors=%v", arm, r.Errors)
					continue
				}
				failed := mode == "insufficient-cap" && arm == "jev_selective"
				if failed {
					initial, final := r.Initial.Own, r.Final.Own
					initial.Audits = nil
					final.Audits = nil
					if !reflect.DeepEqual(initial, final) {
						t.Fatal("exhausted budget changed business state")
					}
					if r.Status != "failed" || r.HTTPFailures == 0 {
						t.Fatal("old budget failure hidden")
					}
					continue
				}
				if !r.StructuralPass || r.Status != "needs_review" || r.Success || len(r.Observations) != 3 {
					t.Fatalf("unexpected result %+v", r.Errors)
				}
				for _, obs := range r.Observations[:2] {
					initial, current := r.Initial.Own, obs.State.Own
					initial.Audits = nil
					current.Audits = nil
					if !reflect.DeepEqual(initial, current) || !reflect.DeepEqual(r.Initial.Controls, obs.State.Controls) || !reflect.DeepEqual(r.Initial.Allowance, obs.State.Allowance) {
						t.Fatal("business write before confirmation")
					}
				}
				clarify, preview, confirm := r.Observations[0], r.Observations[1], r.Observations[2]
				if clarify.Action != "unreviewed" || clarify.Pending != nil || len(clarify.Turn.Response.Trace) != 0 || !strings.Contains(clarify.Turn.Response.Reply, "多少元") {
					t.Fatal("missing necessary amount question")
				}
				if preview.Action != "preview" || preview.Pending == nil || preview.Pending.Tool != "recharge" || conversationInteger(preview.Pending.Args["amount_money"]) != 100 {
					t.Fatal("preview not bound to explicit amount")
				}
				if confirm.Action != "execute" || confirm.Pending != nil || confirm.Turn.Response.Telemetry == nil || confirm.Turn.Response.Telemetry.LLMCalls != 0 {
					t.Fatal("confirmation not deterministic")
				}
				if r.Final.Own.BalanceCent-r.Initial.Own.BalanceCent != 10000 || r.Final.Own.LedgerBalance-r.Initial.Own.LedgerBalance != 10000 || !r.Final.Own.Consistent || len(r.Final.Own.Orders) != len(r.Initial.Own.Orders)+1 || len(r.Final.Own.Topups) != len(r.Initial.Own.Topups)+1 || !reflect.DeepEqual(r.Initial.Own.Subscriptions, r.Final.Own.Subscriptions) {
					t.Fatal("wrong business mutation")
				}
				writes := 0
				for _, tr := range confirm.Turn.Response.Trace {
					if tr.Tool == "recharge" && tr.Outcome == "success" {
						writes++
					}
				}
				if writes != 1 {
					t.Fatal("confirmation did not write exactly once")
				}
			}
			if calls != budget.Snapshot().Calls {
				t.Fatal("budget attempts mismatch")
			}
			if err := os.WriteFile(filepath.Join(dir, "report.json"), mustRechargeJSON(t, report), 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s synthetic_model=%d jev_cap=%d statuses=%s/%s external=0", mode, calls, maxJEV, report.Rows[0].Status, report.Rows[1].Status)
		})
	}
}
