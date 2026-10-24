package taskbench

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepeatedLocalRunnerStopsAndAccounts(t *testing.T) {
	for _, mode := range []string{"normal", "repair", "provider-failure", "jev-failure", "structural-failure", "usage-failure", "receipt-failure", "reverse-provider-failure", "reverse-jev-failure", "cancelled", "observer-failure"} {
		t.Run(mode, func(t *testing.T) {
			o := localCampaignOptions(t)
			a, p, b, e := repeatedInputs(t)
			pilot, err := CheckAuthorizedRepeated(a, p, b, e)
			if err != nil {
				t.Fatal(err)
			}
			source, err := LoadConversations(o.DefinitionsDir)
			if err != nil {
				t.Fatal(err)
			}
			small, err := PrepareSmallPair(source)
			if err != nil {
				t.Fatal(err)
			}
			deps, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer deps.close()
			runCtx, cancelRun := context.WithCancel(context.Background())
			defer cancelRun()
			var observerBroken atomic.Bool
			observer := deps.observer
			if mode == "observer-failure" {
				bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if observerBroken.Load() {
						w.WriteHeader(503)
					} else {
						deps.capture.ServeHTTP(w, r)
					}
				}))
				defer bad.Close()
				observer, err = observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: bad.URL + "/api/public/otel/v1/traces", PublicKey: "pk-synthetic", SecretKey: "sk-synthetic", HashKey: strings.Repeat("h", 32), Environment: "evaluation", SampleRate: 1, QueueSize: 256, ExportTimeout: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Shutdown(context.Background())
			}
			broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				c.Close()
			}))
			defer broken.Close()
			calls := 0
			factories := 0
			var runDir string
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
					t.Fatal("wrong target")
				}
				raw, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				var body struct {
					Messages []agent.Message       `json:"messages"`
					Max      int                   `json:"max_tokens"`
					Thinking struct{ Type string } `json:"thinking"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max != 128 || body.Thinking.Type != "disabled" {
					t.Fatal("request envelope not fixed")
				}
				calls++
				if mode == "cancelled" {
					cancelRun()
					return nil, context.Canceled
				}
				if mode == "receipt-failure" && calls == 1 {
					if e := os.WriteFile(filepath.Join(runDir, "receipt.json"), []byte("occupied"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				first, last := "", ""
				for _, m := range body.Messages {
					if m.Role == "user" {
						if first == "" {
							first = m.Content
						}
						last = m.Content
					}
				}
				var chosen *ConversationCase
				for i := range small.Suite.Cases {
					c := &small.Suite.Cases[i]
					if c.Scenario.Turns[0].User == first {
						chosen = c
						break
					}
				}
				if chosen == nil {
					t.Fatal("unknown frozen message")
				}
				cash := chosen.Key() == "balance-020/explicit_scope_followup"
				if mode == "observer-failure" && cash {
					observerBroken.Store(true)
				}
				if (mode == "provider-failure" || (mode == "reverse-provider-failure" && factories == 7)) && cash {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"synthetic"}`))}, nil
				}
				tool := "get_balance"
				if !strings.HasPrefix(chosen.Key(), "balance-") {
					tool = chosen.Scenario.Operation.Tool
				}
				msg := contentV5("未执行")
				if body.Messages[len(body.Messages)-1].Role == "tool" {
					msg = contentV5(body.Messages[len(body.Messages)-1].Content)
				} else if tool == "get_balance" {
					if cash && last != first && mode == "structural-failure" {
						msg = contentV5("本轮未查询，请沿用前述结果。")
					} else if cash && last != first && mode == "repair" {
						msg = contentV5("当前现金余额为 ¥5000.00。")
					} else {
						msg = localToolMessage(tool, map[string]any{})
					}
				} else if !strings.Contains(last, "100") {
					if tool == "refund_recharge" {
						msg = contentV5("请问退款多少元，或请提供本人已支付充值订单号。")
					} else {
						msg = contentV5("请问充值多少元？")
					}
				} else if mode == "repair" && !strings.Contains(string(raw), "【纠正】") {
					if tool == "refund_recharge" {
						msg = contentV5("退款成功：退回 ¥100。")
					} else {
						msg = contentV5("充值成功：到账 ¥100。")
					}
				} else {
					msg = localToolMessage(tool, map[string]any{"amount_money": 100})
				}
				resp := map[string]any{"choices": []any{map[string]any{"message": msg}}}
				if mode != "usage-failure" || !cash {
					resp["usage"] = map[string]int{"prompt_tokens": 100, "completion_tokens": 8}
				}
				out, err := json.Marshal(resp)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(out)))}, nil
			})
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: observer, Model: pilot.ModelSettings(), Upstream: up, JEVFactory: func() jev.Evaluator {
				factories++
				if (mode == "jev-failure" && factories == 2) || (mode == "reverse-jev-failure" && factories == 7) {
					return jev.NewClient(broken.URL, "synthetic-only", "jev-1.13.0")
				}
				if factories > 24 {
					t.Fatal("extra JEV factory")
				}
				selected := v5RepeatedCases()[factories-1]
				intent := "get_balance"
				if strings.HasPrefix(selected.Key, "recharge-") {
					intent = "recharge"
				}
				if strings.HasPrefix(selected.Key, "refund-") {
					intent = "refund_recharge"
				}
				conf := .99
				if selected.Key == "balance-020/explicit_scope_followup" {
					conf = .5
				}
				return authorizedTestJEV{jev.NewFake(localJevScript(intent, conf)), true}
			}}
			opts.Model.APIKey = "synthetic-only"
			dir := filepath.Join(t.TempDir(), "run")
			runDir = dir
			r, err := RunAuthorizedRepeated(runCtx, dir, opts)
			if r.Schema != "authorized-v5-repeated-cohort.v1" || r.CampaignID != V5RepeatedID || len(r.Batches) != 24 {
				t.Fatalf("receipt identity: %v %+v", err, r)
			}
			raw, _ := json.Marshal(r)
			var receipt map[string]any
			json.Unmarshal(raw, &receipt)
			for key, want := range map[string]float64{"scope_reserved_model_calls": 248, "scope_reserved_jev_calls": 24, "scope_reserved_micro_cny": 8380416, "scope_reserved_micro_usd": 64512} {
				if receipt[key] != want {
					t.Fatal("full reservation not retained", key, receipt[key])
				}
			}
			if mode == "receipt-failure" {
				if err == nil || r.Status != "stopped" || r.FailedStage != "receipt_write" || calls != 88 || r.JEVAttempts != 24 {
					t.Fatalf("receipt persistence failure promoted completion: %v status=%s stage=%s calls=%d", err, r.Status, r.FailedStage, calls)
				}
			} else if mode == "normal" || mode == "repair" {
				want := 88
				if mode == "repair" {
					want = 120
				}
				if err != nil || r.Status != "checkpoint_pending" || calls != want || r.ModelState.Calls != calls || r.JEVAttempts != 24 {
					t.Fatalf("cohort incomplete: %v calls=%d r=%+v", err, calls, r)
				}
				if r.ObserverStats.Turns != 104 || r.ObserverStats.Dropped != 0 || r.ObserverStats.Failed != 0 {
					t.Fatal("coverage mismatch", r.ObserverStats)
				}
				expectedRoots := map[string]CampaignPreparationBatch{}
				for i, batch := range r.Batches {
					if batch.Report == nil {
						t.Fatal("missing pair report")
					}
					if batch.Report.PairID != v5RepeatedBatches()[i].PairID || batch.Report.BlockID != v5RepeatedBatches()[i].BlockID {
						t.Fatal("report identity mismatch")
					}
					for _, row := range batch.Report.Rows {
						expectedRoots[row.LangfuseTraceID] = v5RepeatedBatches()[i]
					}
				}
				deps.capture.Lock()
				roots := 0
				for _, span := range deps.capture.spans {
					if span.Name != "evaluation.task" {
						continue
					}
					roots++
					attrs := map[string]string{}
					for _, a := range span.Attributes {
						attrs[a.Key] = a.Value.GetStringValue()
					}
					expected, ok := expectedRoots[hex.EncodeToString(span.TraceId)]
					if !ok || attrs["langfuse.observation.metadata.pair_id"] != expected.PairID || attrs["langfuse.observation.metadata.block_id"] != expected.BlockID || attrs["langfuse.observation.metadata.arm_order"] != strings.Join(expected.ArmOrder, ",") {
						t.Fatal("missing repeated trace identity", hex.EncodeToString(span.TraceId))
					}
				}
				deps.capture.Unlock()
				if roots != 48 {
					t.Fatal("task roots", roots)
				}
				tenants := map[int64]bool{}
				traces := map[string]bool{}
				for i, c := range v5RepeatedCases() {
					if r.Batches[i].Status != "completed_pending_review" {
						t.Fatal("batch not complete")
					}
					journal, e := loadPairJournal(filepath.Join(dir, "campaign", c.ID, "run.jsonl"))
					if e != nil {
						t.Fatal(e)
					}
					for _, arm := range []string{"model", "jev_selective"} {
						entry := journal.finished[c.Key+":"+arm]
						v := entry.Result
						if tenants[int64(v.Fixture.TenantID)] || traces[entry.LangfuseTraceID] {
							t.Fatal("reused fixture or trace")
						}
						tenants[int64(v.Fixture.TenantID)] = true
						traces[entry.LangfuseTraceID] = true
						if journal.cfg.PairID != v5RepeatedBatches()[i].PairID || journal.cfg.BlockID != v5RepeatedBatches()[i].BlockID || !reflect.DeepEqual(journal.cfg.ArmOrder, v5RepeatedBatches()[i].ArmOrder) {
							t.Fatal("schedule metadata changed")
						}
						raw, _ := os.ReadFile(filepath.Join(dir, "campaign", c.ID, "run.jsonl"))
						var starts []string
						for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
							var e pairRunEvent
							json.Unmarshal(line, &e)
							if e.Kind == "start" {
								starts = append(starts, e.Arm)
							}
						}
						if !reflect.DeepEqual(starts, v5RepeatedBatches()[i].ArmOrder) {
							t.Fatal("actual order changed")
						}
						if v == nil || !v.StructuralPass || v.Success || v.Status != "needs_review" {
							t.Fatal("bad or promoted local quality")
						}
						assertV5SixCaseState(t, v.Case, arm, mode, v)
					}
				}
			} else {
				failedIndex := 1
				firstPairFailure := mode == "cancelled"
				wantCalls, wantJEV := 3, 1
				if firstPairFailure {
					failedIndex = 0
					wantJEV = 0
					wantCalls = 1

				}
				if mode == "observer-failure" {
					wantCalls = 6
					wantJEV = 1
				}
				if strings.HasPrefix(mode, "reverse-") {
					failedIndex = 6
					wantCalls = 23
					wantJEV = 7
					if mode == "reverse-jev-failure" {
						wantCalls = 22
					}
				}
				switch mode {
				case "jev-failure":
					wantCalls, wantJEV = 6, 2
				case "structural-failure":
					wantCalls = 5
				}
				if err == nil || r.Status != "stopped" || calls != wantCalls || r.ModelState.Calls != calls || r.JEVAttempts != wantJEV || (!firstPairFailure && r.Batches[0].Status != "completed_pending_review") || r.Batches[failedIndex].Status != "failed" {
					t.Fatalf("failure did not halt exactly: %v calls%d receipt%+v", err, calls, r)
				}
				for _, br := range r.Batches[failedIndex+1:] {
					if br.Status != "not_started" {
						t.Fatal("later batch started")
					}
					if _, e := os.Stat(filepath.Join(dir, "campaign", br.ID)); !os.IsNotExist(e) {
						t.Fatal("later batch dir created")
					}
				}
				if mode == "usage-failure" && r.ModelState.CostMicroCNY != nil {
					t.Fatal("unknown charge presented as known")
				}
				if mode == "structural-failure" || mode == "observer-failure" {
					rep := r.Batches[1].Report
					if rep == nil || rep.FinishedArms != 1 || rep.UnrunArms != 1 {
						t.Fatal("structural failure did not persist firstarm/unrun denominator")
					}
				}
				if mode == "jev-failure" {
					b, e := os.ReadFile(filepath.Join(dir, "campaign", "batch-002", "run.jsonl.jev.jsonl"))
					if e != nil || !strings.Contains(string(b), `"transport_cause":"eof"`) {
						t.Fatal("EOF evidence absent", e)
					}
				}
			}
			before := calls
			if _, err = RunAuthorizedRepeated(context.Background(), dir, opts); err == nil || calls != before {
				t.Fatal("cohort output reuse called upstream")
			}
			t.Logf("mode=%s model=%d FakeOrLocalJEV=%d status=%s actual_external=0", mode, calls, r.JEVAttempts, r.Status)
		})
	}
}
func TestRepeatedCannotUseHistoricalRunner(t *testing.T) {
	a, p, b, e := repeatedInputs(t)
	pilot, err := CheckAuthorizedRepeated(a, p, b, e)
	if err != nil {
		t.Fatal(err)
	}
	o := AuthorizedFirstBatchOptions{Pilot: pilot}
	if _, err = RunAuthorizedCohort(context.Background(), "absent", o); err == nil {
		t.Fatal("repeated through six-case cohort")
	}
	if _, err = RunAuthorizedRetest(context.Background(), "absent", o); err == nil {
		t.Fatal("cohort through retest")
	}
	if _, err = RunAuthorizedFirstBatch(context.Background(), "absent", o); err == nil {
		t.Fatal("cohort through legacy")
	}
}
