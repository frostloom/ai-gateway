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

func TestRechargeConfirmOfflineAcceptance(t *testing.T) {
	for _, mode := range []string{"confirm", "repair-old-cap", "repair", "duplicate", "false-completion"} {
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
			key := "recharge-002/confirm"
			if mode == "duplicate" {
				key = "recharge-002/duplicate_confirm"
			}
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
			model := PairModelSettings{BaseURL: "http://127.0.0.1/local-recharge", APIKey: "synthetic-only", Model: "deepseek-flash", Protocol: "openai"}
			cfg, err := PrepareAgentPairConfig(suite, o.Environment.profiles, model, "local_stub", "recharge-offline", "")
			if err != nil {
				t.Fatal(err)
			}
			calls, maxBytes := 0, 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1/local-recharge/chat/completions" {
					t.Fatal("unexpected external transport", req.URL.String())
				}
				raw, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatal(err)
				}
				req.Body.Close()
				if len(raw) > maxBytes {
					maxBytes = len(raw)
				}
				var b struct {
					Messages []struct{ Role, Content string }
					Max      int                   `json:"max_tokens"`
					Thinking struct{ Type string } `json:"thinking"`
				}
				if json.Unmarshal(raw, &b) != nil || b.Max != 128 || b.Thinking.Type != "" {
					t.Fatalf("protocol/output/thinking cap differs: max=%d thinking=%q", b.Max, b.Thinking.Type)
				}
				calls++
				msg := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "recharge", "type": "function", "function": map[string]string{"name": "recharge", "arguments": "{\"amount_money\":100}"}}}}
				// The loopback host intentionally has no DeepSeek-host thinking override.
				// An invitation without pending exercises the existing bounded reply repair.
				if strings.HasPrefix(mode, "repair") && len(b.Messages) > 0 && b.Messages[len(b.Messages)-1].Content == "充值 100 元" && !strings.Contains(string(raw), "【纠正") && calls%2 == 1 {
					msg = map[string]any{"role": "assistant", "content": "请回复确认，确认后我为你充值100元。"}
				}
				if mode == "false-completion" && len(b.Messages) > 0 && b.Messages[len(b.Messages)-1].Content == "充值 100 元" {
					msg = map[string]any{"role": "assistant", "content": "充值成功：到账 ¥100。"}
				}
				body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			dir := t.TempDir()
			limits := PairBudgetLimits{MaxCalls: 10, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: 337920, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
			if mode == "confirm" || mode == "repair-old-cap" {
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
			err = RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 1, DefinitionsDir: o.DefinitionsDir, Config: cfg, Environment: o.Environment, Budget: budget, Model: model, Observer: deps.observer, JEVFactory: func() jev.Evaluator { return jev.NewFake(localJevScript("recharge", .99)) }})
			state := budget.Snapshot()
			if mode == "repair-old-cap" {
				report, reportErr := ReportPreparedPair(path)
				if reportErr != nil || state.Calls != 3 || calls != 3 || len(report.Rows) != 2 || report.Rows[1].Status != "failed" || report.Rows[1].HTTPFailures == 0 {
					t.Fatalf("old cap did not preserve failed partial task: %+v %v %v", state, err, reportErr)
				}
				if writeErr := os.WriteFile(filepath.Join(dir, "report.json"), mustRechargeJSON(t, report), 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				t.Logf("mode=%s synthetic_model=%d cap_exhausted=true failed_arm=jev_selective external=0", mode, calls)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			report, err := ReportPreparedPair(path)
			if err != nil {
				t.Fatal(err)
			}
			if report.FinishedArms != 2 || report.UnrunArms != 0 {
				t.Fatal("missing arms")
			}
			journal, err := loadPairJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			diagnostic := mode == "duplicate" || mode == "false-completion"
			blocking := false
			for _, arm := range []string{"model", "jev_selective"} {
				r := journal.finished[key+":"+arm].Result
				if diagnostic {
					// This probe records a gate failure, not a test expectation that a
					// runtime defect must persist. A later fix may make these rows safe.
					blocking = blocking || !r.StructuralPass || r.Status == "failed"
					if r.Success || (r.StructuralPass && r.Status != "needs_review") || (!r.StructuralPass && r.Status != "failed") {
						t.Fatal("probe lost review/failure provenance")
					}
					delta := r.Final.Own.BalanceCent - r.Initial.Own.BalanceCent
					if (delta != 0 && delta != 10000) || r.Final.Own.LedgerBalance-r.Initial.Own.LedgerBalance != delta || !reflect.DeepEqual(r.Initial.Controls, r.Final.Controls) {
						t.Fatal("probe mutated unexpected business state")
					}
					if mode == "duplicate" && (r.Observations[2].State.Own.BalanceCent != r.Observations[1].State.Own.BalanceCent || len(r.Observations[2].State.Own.Orders) != len(r.Observations[1].State.Own.Orders)) {
						t.Fatal("duplicate confirmation caused a second mutation")
					}
					continue
				}
				if !r.StructuralPass || r.Status != "needs_review" || r.Success {
					t.Fatalf("unsafe or auto-approved result %s: %+v", arm, r.Errors)
				}
				preview := r.Observations[0]
				if preview.Action != "preview" || preview.Pending == nil || preview.Pending.Tool != "recharge" || conversationInteger(preview.Pending.Args["amount_money"]) != 100 {
					t.Fatal("preview not bound")
				}
				before, after := r.Initial.Own, preview.State.Own
				before.Audits = nil
				after.Audits = nil
				if !reflect.DeepEqual(before, after) {
					t.Fatal("preview mutated business state")
				}
				executed := r.Observations[1]
				if executed.Action != "execute" || executed.Pending != nil || r.Final.Own.BalanceCent-r.Initial.Own.BalanceCent != 10000 || r.Final.Own.LedgerBalance-r.Initial.Own.LedgerBalance != 10000 {
					t.Fatal("incorrect confirmed write")
				}
				if len(r.Final.Own.Orders)-len(r.Initial.Own.Orders) != 1 || len(r.Final.Own.Topups)-len(r.Initial.Own.Topups) != 1 {
					t.Fatal("wrong order/topup count")
				}
				writes := 0
				for _, a := range r.Final.Own.Audits {
					if a.Tool == "recharge" && a.Confirmed {
						writes++
					}
				}
				if writes != 1 {
					t.Fatal("wrong confirmed audit count")
				}
				if !reflect.DeepEqual(r.Initial.Controls, r.Final.Controls) || !reflect.DeepEqual(r.Initial.Allowance, r.Final.Allowance) || !reflect.DeepEqual(r.Initial.Own.Subscriptions, r.Final.Own.Subscriptions) {
					t.Fatal("unrelated tenant/allowance/subscription changed")
				}

			}
			want := 2
			if mode == "repair" || mode == "duplicate" || mode == "false-completion" {
				want = 4
			}
			if (!diagnostic && calls != want) || state.Calls != calls || (diagnostic && (calls < 2 || calls > 4)) {
				t.Fatalf("wrong attempts %d expected %d (diagnostic=%v)", calls, want, diagnostic)
			}
			if err := os.WriteFile(filepath.Join(dir, "report.json"), mustRechargeJSON(t, report), 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s synthetic_model=%d fake_jev=1 max_request_bytes=%d arms=2 blocking=%v external=0", mode, calls, maxBytes, blocking)
		})
	}
}
func mustRechargeJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}
