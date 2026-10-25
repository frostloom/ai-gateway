package taskbench

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
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

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

func TestRepeatedDiagnosticLocalRunnerStopsAndAccounts(t *testing.T) {
	modes := []string{
		"normal", "repair",
		"observer-failure", "truncated-observer",
		"provider-failure", "jev-failure", "structural-failure", "usage-failure", "fact-failure", "receipt-failure",
		"reverse-provider-failure", "reverse-jev-failure", "reverse-structural-failure", "reverse-usage-failure", "reverse-fact-failure",
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			o := localCampaignOptions(t)
			revision := strings.Repeat("a", 40)
			a, p, b, e := diagnosticMaterials(true, revision)
			pilot, err := CheckAuthorizedRepeatedDiagnostic(a, p, b, e)
			if err != nil || !pilot.Ready() || !pilot.IsRepeated() || !pilot.IsRepeatedDiagnostic() {
				t.Fatal(err, pilot.Blockers())
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
			var exportRequests atomic.Int64
			var observerBroken atomic.Bool
			exportFault := mode == "observer-failure" || mode == "truncated-observer"
			observer := deps.observer
			if exportFault {
				bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					exportRequests.Add(1)
					if !observerBroken.Load() {
						deps.capture.ServeHTTP(w, r)
						return
					}
					if _, err := io.ReadAll(r.Body); err != nil {
						t.Error(err)
						return
					}
					r.Body.Close()
					if mode == "observer-failure" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Type", "application/x-protobuf")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte{0xff})
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
				followup := chosen.Key() == "balance-020/explicit_scope_followup"
				if exportFault && followup {
					observerBroken.Store(true)
				}
				reverseArm := factories == 7
				if (mode == "provider-failure" && followup) || (mode == "reverse-provider-failure" && followup && reverseArm) {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"synthetic"}`))}, nil
				}
				tool := "get_balance"
				if !strings.HasPrefix(chosen.Key(), "balance-") {
					tool = chosen.Scenario.Operation.Tool
				}
				factCase := mode == "fact-failure" && chosen.Key() == "balance-001/answer_complete"
				if mode == "reverse-fact-failure" && followup && reverseArm {
					factCase = true
				}
				structural := (mode == "structural-failure" && followup) || (mode == "reverse-structural-failure" && followup && reverseArm)
				msg := contentV5("未执行")
				if factCase && len(body.Messages) > 0 && body.Messages[len(body.Messages)-1].Role == "tool" {
					msg = contentV5("当前现金余额为 ¥1.00。")
				} else if body.Messages[len(body.Messages)-1].Role == "tool" {
					msg = contentV5(body.Messages[len(body.Messages)-1].Content)
				} else if tool == "get_balance" {
					if structural && last != first {
						msg = contentV5("本轮未查询，请沿用前述结果。")
					} else if followup && last != first && mode == "repair" {
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
				omitUsage := (mode == "usage-failure" && followup) || (mode == "reverse-usage-failure" && followup && reverseArm)
				if !omitUsage {
					resp["usage"] = map[string]int{"prompt_tokens": 100, "completion_tokens": 8}
				}
				out, err := json.Marshal(resp)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(out)))}, nil
			})
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: revision, Observer: observer, Model: pilot.ModelSettings(), Upstream: up, JEVFactory: func() jev.Evaluator {
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
			r, err := RunAuthorizedRepeatedDiagnostic(context.Background(), dir, opts)
			if r.Schema != "authorized-v5-repeated-export-diagnostic.v1" || r.CampaignID != V5RepeatedDiagnosticID || len(r.Batches) != 24 || r.Revision != revision || r.Revision == v5RepeatedDiagnosticBinding().ObserverRevision {
				t.Fatalf("receipt identity: %v %+v", err, r)
			}
			if r.RepeatedDiagnosticBinding != v5RepeatedDiagnosticBinding() || r.DiagnosticSourceRevision != RechargeDiagnosticSourceRevision {
				t.Fatalf("diagnostic binding missing: %+v %s", r.RepeatedDiagnosticBinding, r.DiagnosticSourceRevision)
			}
			raw, _ := json.Marshal(r)
			for _, key := range []string{`"observer_revision":"4d887382300a906ae6d1a33972f3fe4d4aca4e0d"`, `"observer_source_sha256":"16121832bb7f1850e699aab4b005bc9e3755016dc430002a490406352c6f3ba9"`, `"synthetic_provenance_sha256":"08f6badd79c44bf2d9648ae0698f2c5a324144bbede791e4de5606017d331409"`, `"prior_result_manifest_sha256":"41b2560c7f4230cad1c8b22df4123544f2687a6c2a275a3c2fe7bcf66cf781e8"`, `"diagnostic_source_revision":"` + RechargeDiagnosticSourceRevision + `"`} {
				if !bytes.Contains(raw, []byte(key)) {
					t.Fatal("persisted binding field missing", key)
				}
			}
			var receipt map[string]any
			json.Unmarshal(raw, &receipt)
			for key, want := range map[string]float64{"scope_reserved_model_calls": 248, "scope_reserved_jev_calls": 24, "scope_reserved_micro_cny": 8380416, "scope_reserved_micro_usd": 64512} {
				if receipt[key] != want {
					t.Fatal("full reservation not retained", key, receipt[key])
				}
			}
			if mode == "receipt-failure" {
				if err == nil || r.Status != "stopped" || r.FailedStage != "receipt_write" || calls != 88 || r.JEVAttempts != 24 || r.CampaignReservedModelCalls != 248 || r.CampaignReservedJEVCalls != 24 {
					t.Fatalf("receipt persistence failure promoted completion: %v status=%s stage=%s calls=%d reserved=%d", err, r.Status, r.FailedStage, calls, r.CampaignReservedModelCalls)
				}
			} else if mode == "normal" || mode == "repair" {
				want := 88
				if mode == "repair" {
					want = 120
				}
				if err != nil || r.Status != "checkpoint_pending" || calls != want || r.ModelState.Calls != calls || r.JEVAttempts != 24 || r.CampaignReservedModelCalls != 248 {
					t.Fatalf("cohort incomplete: %v calls=%d r=%+v", err, calls, r)
				}
				if r.ObserverStats.Turns != 104 || r.ObserverStats.Dropped != 0 || r.ObserverStats.Failed != 0 || r.ObserverStats.ExportDiagnostics != nil {
					t.Fatal("coverage mismatch", r.ObserverStats)
				}
				expectedRoots := map[string]CampaignPreparationBatch{}
				for i, batch := range r.Batches {
					if batch.Report == nil || batch.Report.EffectClaimsAllowed || !containsLimitation(batch.Report.Limitations, "not a supplier invoice") {
						t.Fatal("synthetic usage presented as a bill", batch.Report)
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
					if !ok || attrs["langfuse.observation.metadata.pair_id"] != expected.PairID || attrs["langfuse.observation.metadata.block_id"] != expected.BlockID || attrs["langfuse.observation.metadata.arm_order"] != strings.Join(expected.ArmOrder, ",") || attrs["langfuse.observation.metadata.task_success"] != "unknown" || attrs["langfuse.observation.metadata.evidence_kind"] == "" {
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
				if len(tenants) != 48 || len(traces) != 48 {
					t.Fatal("account/trace coverage", len(tenants), len(traces))
				}
			} else {
				failedIndex := 1
				wantCalls, wantJEV := 3, 1
				switch mode {
				case "fact-failure":
					failedIndex, wantCalls, wantJEV = 0, 2, 0
				case "observer-failure", "truncated-observer":
					wantCalls, wantJEV = 6, 1
				case "jev-failure":
					wantCalls, wantJEV = 6, 2
				case "structural-failure":
					wantCalls = 5
				case "reverse-provider-failure", "reverse-usage-failure":
					failedIndex, wantCalls, wantJEV = 6, 23, 7
				case "reverse-jev-failure":
					failedIndex, wantCalls, wantJEV = 6, 22, 7
				case "reverse-structural-failure":
					failedIndex, wantCalls, wantJEV = 6, 25, 7
				case "reverse-fact-failure":
					failedIndex, wantCalls, wantJEV = 6, 26, 7
				}
				if err == nil || r.Status != "stopped" || calls != wantCalls || r.ModelState.Calls != calls || r.JEVAttempts != wantJEV || (failedIndex > 0 && r.Batches[0].Status != "completed_pending_review") || r.Batches[failedIndex].Status != "failed" {
					t.Fatalf("failure did not halt exactly: %v calls%d jev%d receipt%+v", err, calls, r.JEVAttempts, r)
				}
				for _, br := range r.Batches[failedIndex+1:] {
					if br.Status != "not_started" {
						t.Fatal("later batch started")
					}
					if _, e := os.Stat(filepath.Join(dir, "campaign", br.ID)); !os.IsNotExist(e) {
						t.Fatal("later batch dir created")
					}
				}
				reservedCalls, reservedJEV := 0, 0
				var reservedCNY int64
				for i := 0; i <= failedIndex; i++ {
					reservedCalls += v5RepeatedCases()[i].ModelCalls
					reservedJEV += v5RepeatedCases()[i].JEVCalls
					reservedCNY += int64(v5RepeatedCases()[i].ModelCalls) * 33792
				}
				if r.CampaignReservedModelCalls != reservedCalls || r.CampaignReservedJEVCalls != reservedJEV || r.CampaignReservedMicroCNY != reservedCNY || r.CampaignReservedMicroUSD != int64(reservedJEV)*2688 || r.ScopeReservedModelCalls != 248 || reservedCalls >= r.ScopeReservedModelCalls {
					t.Fatalf("prefix lease collapsed into full reservation: campaign=%d/%d scope=%d", r.CampaignReservedModelCalls, r.CampaignReservedJEVCalls, r.ScopeReservedModelCalls)
				}
				if mode == "usage-failure" || mode == "reverse-usage-failure" {
					if r.ModelState.CostMicroCNY != nil {
						t.Fatal("unknown charge presented as known")
					}
				}
				if mode == "structural-failure" || mode == "reverse-structural-failure" || mode == "observer-failure" || mode == "truncated-observer" || mode == "fact-failure" || mode == "reverse-fact-failure" {
					rep := r.Batches[failedIndex].Report
					if rep == nil || rep.FinishedArms != 1 || rep.UnrunArms != 1 {
						t.Fatal("failure did not persist firstarm/unrun denominator", rep)
					}
					if strings.Contains(mode, "fact") {
						found := false
						for _, row := range rep.Rows {
							if row.Status == "failed" && row.FactErrors > 0 {
								found = true
							}
						}
						if !found {
							t.Fatal("fact mismatch not persisted", rep)
						}
					}
				}
				if mode == "observer-failure" || mode == "truncated-observer" {
					persisted, readErr := os.ReadFile(filepath.Join(dir, "receipt.json"))
					if readErr != nil {
						t.Fatal(readErr)
					}
					var saved map[string]any
					if json.Unmarshal(persisted, &saved) != nil {
						t.Fatal("invalid persisted receipt")
					}
					stats := saved["observer_stats"].(map[string]any)
					diagnostics, ok := stats["export_diagnostics"].(map[string]any)
					if !ok {
						t.Fatal("stopped receipt lost export diagnostic")
					}
					records := diagnostics["failures"].([]any)
					if len(records) == 0 || len(records) > 8 {
						t.Fatal("diagnostic bound invalid", diagnostics)
					}
					kind, status := "http_status", float64(503)
					if mode == "truncated-observer" {
						kind, status = "response_decode", float64(200)
					}
					failedSpans := float64(0)
					for _, value := range records {
						record := value.(map[string]any)
						if record["error_kind"] != kind || record["http_status"] != status || record["http_round_trips"] != float64(1) {
							t.Fatal("wrong stopped receipt cause", record)
						}
						failedSpans += record["span_count"].(float64)
					}
					omitted, _ := diagnostics["omitted_failures"].(float64)
					if omitted != diagnostics["failed_batches"].(float64)-float64(len(records)) || failedSpans != stats["Failed"] || stats["Failed"] == float64(0) || stats["Dropped"] != float64(0) {
						t.Fatal("diagnostics not bound to original failure counts", diagnostics, stats["Failed"])
					}
					expectedStats, _ := json.Marshal(r.ObserverStats)
					var returned map[string]any
					json.Unmarshal(expectedStats, &returned)
					if !reflect.DeepEqual(stats, returned) {
						t.Fatal("persisted diagnostics differ from returned receipt")
					}
				}
				if mode == "jev-failure" {
					b, e := os.ReadFile(filepath.Join(dir, "campaign", "batch-002", "run.jsonl.jev.jsonl"))
					if e != nil || !strings.Contains(string(b), `"transport_cause":"eof"`) {
						t.Fatal("EOF evidence absent", e)
					}
				}
			}
			deps.capture.Lock()
			spansBefore := len(deps.capture.spans)
			deps.capture.Unlock()
			exportsBefore := exportRequests.Load()
			before := calls
			if _, err = RunAuthorizedRepeatedDiagnostic(context.Background(), dir, opts); err == nil || calls != before || exportRequests.Load() != exportsBefore {
				t.Fatal("diagnostic output reuse called upstream or replayed export")
			}
			deps.capture.Lock()
			spansAfter := len(deps.capture.spans)
			deps.capture.Unlock()
			if spansAfter != spansBefore {
				t.Fatal("export replayed into capture", spansBefore, spansAfter)
			}
			t.Logf("mode=%s model=%d FakeOrLocalJEV=%d status=%s synthetic_usage_not_billable actual_external=0", mode, calls, r.JEVAttempts, r.Status)
		})
	}
}

func containsLimitation(items []string, part string) bool {
	for _, item := range items {
		if strings.Contains(item, part) {
			return true
		}
	}
	return false
}

func TestRepeatedDiagnosticCleanupStopsBothArmOrders(t *testing.T) {
	for _, index := range []int{0, 6} {
		order := v5RepeatedBatches()[index].ArmOrder
		t.Run(v5RepeatedBatches()[index].PairID, func(t *testing.T) {
			if index == 0 && !reflect.DeepEqual(order, []string{"model", "jev_selective"}) {
				t.Fatal("block0 order changed", order)
			}
			if index == 6 && !reflect.DeepEqual(order, []string{"jev_selective", "model"}) {
				t.Fatal("block1 order changed", order)
			}
			cfg := explicitTestOrder(t, pairTestConfig(), order)
			path := filepath.Join(t.TempDir(), "pair")
			calls := 0
			err := runPreparedPair(context.Background(), path, cfg, nil, func(context.Context, string, ConversationCase) (ConversationDriver, error) {
				calls++
				return &pairFaultDriver{ConversationDriver: rechargeDriver(), stage: "cleanup"}, nil
			}, nil, true, true, true)
			if err == nil || calls != 1 {
				t.Fatalf("cleanup failure continued: %v arms=%d", err, calls)
			}
			r, e := ReportPreparedPair(path)
			if e != nil || r.FinishedArms != 1 || r.UnrunArms != 1 || len(r.Rows) != 2 {
				t.Fatalf("cleanup stop lost order: %+v %v", r, e)
			}
			seenFailed := false
			for _, row := range r.Rows {
				if row.Arm == order[0] {
					if row.Status != "failed" || row.StructuralPass || len(row.ExecutionFailures) != 1 || row.ExecutionFailures[0].Stage != "cleanup" {
						t.Fatalf("first arm cleanup not retained: %+v", row)
					}
					seenFailed = true
				} else if row.Status != "not_started" {
					t.Fatalf("later arm started: %+v", row)
				}
			}
			if !seenFailed {
				t.Fatal("scheduled first arm missing")
			}
		})
	}
}
