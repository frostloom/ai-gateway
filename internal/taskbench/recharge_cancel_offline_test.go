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

// Catches cancellation that leaves pending state, writes money/orders, or asks
// the provider to interpret an explicit cancel instead of clearing the action.
func TestRechargeCancelOfflineAcceptance(t *testing.T) {
	for _, mode := range []string{"normal", "repair", "repair-old-cap"} {
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
			key := "recharge-002/cancel"
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
			model := PairModelSettings{BaseURL: "http://127.0.0.1/local-cancel", APIKey: "synthetic-only", Model: "deepseek-flash", Protocol: "openai"}
			cfg, err := PrepareAgentPairConfig(suite, o.Environment.profiles, model, "local_stub", "cancel-offline", "")
			if err != nil {
				t.Fatal(err)
			}
			calls, maxBytes := 0, 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1/local-cancel/chat/completions" {
					t.Fatal("unexpected provider target")
				}
				raw, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if len(raw) > maxBytes {
					maxBytes = len(raw)
				}
				var body struct {
					Max      int                   `json:"max_tokens"`
					Thinking struct{ Type string } `json:"thinking"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 || body.Thinking.Type != "" {
					t.Fatal("protocol/output/thinking bound missing")
				}
				calls++
				msg := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "recharge", "type": "function", "function": map[string]string{"name": "recharge", "arguments": "{\"amount_money\":100}"}}}}
				if strings.HasPrefix(mode, "repair") && !strings.Contains(string(raw), "【纠正】") {
					msg = map[string]any{"role": "assistant", "content": "充值成功：到账 ¥100。"}
				}
				response, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
			})
			dir := t.TempDir()
			limits := PairBudgetLimits{MaxCalls: 10, MaxOutputTokens: 128, MaxRequestBytes: 16384, InputTokenCeiling: 16384, MaxMicroCNY: 337920, InputMicroCNYPerMillion: 2000000, OutputMicroCNYPerMillion: 8000000}
			if mode == "repair-old-cap" {
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
			if err != nil {
				t.Fatalf("local pair rejected: %v", err)
			}
			report, reportErr := ReportPreparedPair(path)
			if reportErr != nil {
				t.Fatal(reportErr)
			}
			if mode == "repair-old-cap" {
				if calls != 3 || len(report.Rows) != 2 || report.Rows[1].Status != "failed" || report.Rows[1].HTTPFailures == 0 {
					t.Fatal("old cap failure hidden")
				}
			} else {
				if err != nil || report.FinishedArms != 2 {
					t.Fatalf("pair incomplete: %v", err)
				}
			}
			journal, err := loadPairJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, arm := range []string{"model", "jev_selective"} {
				r := journal.finished[key+":"+arm].Result
				if r == nil {
					t.Fatal("result missing")
				}
				initial, final := r.Initial.Own, r.Final.Own
				initial.Audits = nil
				final.Audits = nil
				if !reflect.DeepEqual(initial, final) || !reflect.DeepEqual(r.Initial.Controls, r.Final.Controls) || !reflect.DeepEqual(r.Initial.Allowance, r.Final.Allowance) {
					t.Fatal("cancellation/budget failure changed business state")
				}
				if mode == "repair-old-cap" && arm == "jev_selective" {
					continue
				}
				if !r.StructuralPass || r.Status != "needs_review" || r.Success || len(r.Observations) != 2 {
					t.Fatal("cancel task incorrectly scored")
				}
				preview, cancel := r.Observations[0], r.Observations[1]
				if preview.Action != "preview" || preview.Pending == nil || preview.Pending.Tool != "recharge" || conversationInteger(preview.Pending.Args["amount_money"]) != 100 {
					t.Fatal("preview not bound")
				}
				if cancel.Action != "unreviewed" || cancel.Pending != nil || len(cancel.Turn.Response.Trace) != 0 || (cancel.Turn.Response.Telemetry == nil || cancel.Turn.Response.Telemetry.LLMCalls != 0) {
					t.Fatalf("cancel evidence: action=%s pending=%+v trace=%+v telemetry=%+v", cancel.Action, cancel.Pending, cancel.Turn.Response.Trace, cancel.Turn.Response.Telemetry)
				}
				count := 0
				for _, a := range r.Final.Own.Audits {
					if a.Tool == "cancel_confirmation" && a.Result == "cancelled" && !a.Confirmed {
						count++
					}
				}
				if count != 1 {
					t.Fatal("missing authoritative cancellation audit")
				}

			}
			want := 2
			if mode == "repair" {
				want = 4
			}
			if mode == "repair-old-cap" {
				want = 3
			}
			if calls != want || budget.Snapshot().Calls != calls {
				t.Fatal("attempt cap mismatch")
			}
			if err := os.WriteFile(filepath.Join(dir, "report.json"), mustRechargeJSON(t, report), 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s synthetic_model=%d max_bytes=%d external=0 state_unchanged=true", mode, calls, maxBytes)
		})
	}
}
