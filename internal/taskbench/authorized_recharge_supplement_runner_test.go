package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// Real Handler/fixture/budget: only the third-round confirmation may recharge,
// failures cannot fall back or retry, and consumed output must remain unusable.
func TestSupplementRechargeLocalRunner(t *testing.T) {
	for _, mode := range []string{"complete", "repair", "provider-failure", "jev-failure"} {
		t.Run(mode, func(t *testing.T) {
			o := localCampaignOptions(t)
			a, p, b, e := supplementRechargeInputs(t)
			pilot, err := CheckAuthorizedRetest(a, p, b, e)
			if err != nil {
				t.Fatal(err)
			}
			deps, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer deps.close()
			var broken *httptest.Server
			if mode == "jev-failure" {
				broken = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				}))
				defer broken.Close()
			}
			calls := 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
					t.Fatal("wrong target")
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
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 || body.Thinking.Type != "disabled" {
					t.Fatal("request envelope not bound")
				}
				if mode == "provider-failure" {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"synthetic"}`))}, nil
				}
				msg := `{"role":"assistant","content":"","tool_calls":[{"id":"recharge","type":"function","function":{"name":"recharge","arguments":"{\"amount_money\":100}"}}]}`
				last := ""
				for _, m := range body.Messages {
					if m.Role == "user" {
						last = m.Content
					}
				}
				if !strings.Contains(last, "100") {
					msg = `{"role":"assistant","content":"请问您要充值多少元？"}`
				} else if mode == "repair" && !strings.Contains(string(raw), "【纠正】") {
					msg = `{"role":"assistant","content":"充值成功：到账 ¥100。"}`
				}
				response := `{"choices":[{"message":` + msg + `}],"usage":{"prompt_tokens":100,"completion_tokens":8}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: deps.observer, Model: pilot.ModelSettings(), Upstream: up, JEVFactory: func() jev.Evaluator {
				if broken != nil {
					return jev.NewClient(broken.URL, "synthetic", "jev-1.13.0")
				}
				return authorizedTestJEV{jev.NewFake(localJevScript("recharge", .99)), true}
			}}
			opts.Model.APIKey = "synthetic-only"
			dir := filepath.Join(t.TempDir(), "run")
			r, err := RunAuthorizedRetest(context.Background(), dir, opts)
			if r.Schema != "authorized-recharge-supplement.v1" || r.OfflineManifestSHA != "6c8a04a705931816c74425effb17a4856c507ba48ce77dcad17d31182119b578" || r.DiagnosticSourceRevision != "5c2f669413c0787ba1bd3a429c9552d628e1a44c" {
				t.Fatal("receipt provenance lost")
			}
			want := 4
			if mode == "repair" {
				want = 6
			}
			if strings.HasSuffix(mode, "failure") {
				failureCalls := 1
				if mode == "jev-failure" {
					failureCalls = 2
				} // completed model arm; JEV arm stops before its first model
				if err == nil || r.Status != "stopped" || calls != failureCalls {
					t.Fatalf("failure did not halt: %v calls=%d status=%s", err, calls, r.Status)
				}
				if mode == "jev-failure" {
					if r.JEVAttempts != 1 {
						t.Fatal("JEV retried")
					}
					raw, e := os.ReadFile(filepath.Join(dir, "campaign", "batch-001", "run.jsonl.jev.jsonl"))
					if e != nil || !strings.Contains(string(raw), `"transport_cause":"eof"`) {
						t.Fatalf("EOF diagnostic absent: %v", e)
					}
				}
			} else {
				if err != nil || r.Status != "checkpoint_pending" || r.Report == nil || r.Report.FinishedArms != 2 || calls != want || r.JEVAttempts != 1 {
					t.Fatalf("incomplete cancel: %v %+v calls=%d", err, r, calls)
				}
				if r.CampaignReservedModelCalls != 12 || r.CampaignReservedMicroCNY != 405504 || r.CampaignReservedMicroUSD != 2688 {
					t.Fatal("reservation changed")
				}
				journal, err := loadPairJournal(filepath.Join(dir, "campaign", "batch-001", "run.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range journal.finished {
					v := event.Result
					if v == nil || len(v.Observations) != 3 {
						t.Fatal("missing rounds")
					}
					if !v.StructuralPass || v.Status != "needs_review" || v.Success {
						t.Fatal("local fixture promoted to semantic success")
					}
					if !reflect.DeepEqual(v.Initial.Controls, v.Final.Controls) || !reflect.DeepEqual(v.Initial.Allowance, v.Final.Allowance) {
						t.Fatal("control state changed")
					}
					for _, obs := range v.Observations[:2] {
						initial, current := v.Initial.Own, obs.State.Own
						initial.Audits = nil
						current.Audits = nil
						if !reflect.DeepEqual(initial, current) {
							t.Fatal("write before confirmation")
						}
					}
					clarify, preview, confirm := v.Observations[0], v.Observations[1], v.Observations[2]
					if clarify.Pending != nil || len(clarify.Turn.Response.Trace) != 0 || !strings.Contains(clarify.Turn.Response.Reply, "多少元") {
						t.Fatal("clarification missing")
					}
					if preview.Action != "preview" || preview.Pending == nil || preview.Pending.Tool != "recharge" || conversationInteger(preview.Pending.Args["amount_money"]) != 100 {
						t.Fatal("unbound preview")
					}
					if confirm.Action != "execute" || confirm.Pending != nil || confirm.Turn.Response.Telemetry == nil || confirm.Turn.Response.Telemetry.LLMCalls != 0 || confirm.Turn.Response.Telemetry.JevCalls != 0 {
						t.Fatal("confirmation called model or left pending")
					}
					if v.Final.Own.BalanceCent-v.Initial.Own.BalanceCent != 10000 || v.Final.Own.LedgerBalance-v.Initial.Own.LedgerBalance != 10000 || !v.Final.Own.Consistent || len(v.Final.Own.Orders) != len(v.Initial.Own.Orders)+1 || len(v.Final.Own.Topups) != len(v.Initial.Own.Topups)+1 || !reflect.DeepEqual(v.Initial.Own.Subscriptions, v.Final.Own.Subscriptions) {
						t.Fatal("wrong confirmed mutation")
					}
					writes := 0
					for _, tr := range confirm.Turn.Response.Trace {
						if tr.Tool == "recharge" && tr.Outcome == "success" {
							writes++
						}
					}
					if writes != 1 {
						t.Fatal("not exactly one write")
					}

				}
			}
			before := calls
			if _, err := RunAuthorizedRetest(context.Background(), dir, opts); err == nil || calls != before {
				t.Fatal("output reuse called provider")
			}
			t.Logf("mode=%s synthetic_model=%d status=%s external=0", mode, calls, r.Status)
		})
	}
}
