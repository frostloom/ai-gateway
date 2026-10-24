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

// Real Handler/fixture/budget: cancellation must not execute the recharge,
// failures cannot fall back or retry, and consumed output must remain unusable.
func TestCancelRechargeLocalRunner(t *testing.T) {
	for _, mode := range []string{"complete", "repair", "provider-failure", "jev-failure"} {
		t.Run(mode, func(t *testing.T) {
			o := localCampaignOptions(t)
			a, p, b, e := cancelRechargeInputs(t)
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
					Max      int                   `json:"max_tokens"`
					Thinking struct{ Type string } `json:"thinking"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 || body.Thinking.Type != "disabled" {
					t.Fatal("request envelope not bound")
				}
				if mode == "provider-failure" {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"synthetic"}`))}, nil
				}
				msg := `{"role":"assistant","content":"","tool_calls":[{"id":"recharge","type":"function","function":{"name":"recharge","arguments":"{\"amount_money\":100}"}}]}`
				if mode == "repair" && !strings.Contains(string(raw), "【纠正】") {
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
			if r.Schema != "authorized-recharge-cancel.v1" || r.OfflineManifestSHA != "0458f4ce65f17983185611a8affb236332d48a4b48bd388d803db64673d03f79" || r.DiagnosticSourceRevision != "5c2f669413c0787ba1bd3a429c9552d628e1a44c" {
				t.Fatal("receipt provenance lost")
			}
			want := 2
			if mode == "repair" {
				want = 4
			}
			if strings.HasSuffix(mode, "failure") {
				if err == nil || r.Status != "stopped" || calls != 1 {
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
				if r.CampaignReservedModelCalls != 10 || r.CampaignReservedMicroCNY != 337920 || r.CampaignReservedMicroUSD != 2688 {
					t.Fatal("reservation changed")
				}
				journal, err := loadPairJournal(filepath.Join(dir, "campaign", "batch-001", "run.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range journal.finished {
					v := event.Result
					if v == nil || len(v.Observations) != 2 {
						t.Fatal("missing rounds")
					}
					if !v.StructuralPass || v.Status != "needs_review" || v.Success {
						t.Fatal("local cancellation promoted to semantic success")
					}
					initial, final := v.Initial.Own, v.Final.Own
					initial.Audits = nil
					final.Audits = nil
					if !reflect.DeepEqual(initial, final) || !reflect.DeepEqual(v.Initial.Controls, v.Final.Controls) || !reflect.DeepEqual(v.Initial.Allowance, v.Final.Allowance) {
						t.Fatal("cancel changed business state")
					}
					preview, cancel := v.Observations[0], v.Observations[1]
					if preview.Action != "preview" || preview.Pending == nil || conversationInteger(preview.Pending.Args["amount_money"]) != 100 || preview.Pending.Tool != "recharge" {
						t.Fatal("missing bound preview")
					}
					if cancel.Action != "unreviewed" || cancel.Pending != nil || len(cancel.Turn.Response.Trace) != 0 || cancel.Turn.Response.Telemetry == nil || cancel.Turn.Response.Telemetry.LLMCalls != 0 {
						t.Fatal("cancel called provider or left pending")
					}
					count := 0
					for _, a := range v.Final.Own.Audits {
						if a.Tool == "cancel_confirmation" && a.Result == "cancelled" && !a.Confirmed {
							count++
						}
					}
					if count != 1 {
						t.Fatal("cancel audit missing")
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
