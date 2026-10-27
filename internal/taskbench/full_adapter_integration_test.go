package taskbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
)

func TestFullAdapterLocalHandlerAndCampaign(t *testing.T) {
	for _, item := range []struct{ key, fault string }{
		{"models-001/answer_complete", ""},
		{"refund-001/supplement_then_confirm", ""},
		{"cancel_subscription-001/confirm", ""},
		{"models-001/answer_complete", "provider"},
		{"models-001/answer_complete", "jev"},
		{"models-001/answer_complete", "observer"},
		{"models-001/answer_complete", "case"},
		{"models-001/answer_complete", "selection"},
		{"balance-001/answer_complete", "legacy-budget"},
	} {
		t.Run(item.key+"/"+item.fault, func(t *testing.T) { runFullAdapterLocal(t, item.key, item.fault) })
	}
}

func runFullAdapterLocal(t *testing.T, key, fault string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	env, err := OpenPairedFixtureEnvironment(ctx, PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Close(); err != nil {
			t.Error(err)
		}
	})
	source, profiles := fullAdapterSource(t)
	var modelCalls, judgeFactories, exportCalls atomic.Int64
	tool := "list_models"
	if strings.HasPrefix(key, "refund-") {
		tool = "refund_recharge"
	}
	if strings.HasPrefix(key, "cancel_subscription-") {
		tool = "cancel_subscription"
	}
	if strings.HasPrefix(key, "balance-") {
		tool = "get_balance"
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		if fault == "provider" {
			w.WriteHeader(503)
			return
		}
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type    string `json:"type"`
					Text    string `json:"text"`
					Content string `json:"content"`
				} `json:"content"`
			} `json:"messages"`
			Max int `json:"max_tokens"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Max != 128 || r.URL.Path != "/v1/messages" {
			t.Error("wrong local request contract")
			w.WriteHeader(400)
			return
		}
		args := map[string]any{}
		if tool == "refund_recharge" {
			args["amount_money"] = 100
		}
		content := []any{map[string]any{"type": "tool_use", "id": "local-call", "name": tool, "input": args}}
		stop := "tool_use"
		if len(request.Messages) > 0 {
			for _, b := range request.Messages[len(request.Messages)-1].Content {
				if b.Type == "tool_result" {
					content = []any{map[string]any{"type": "text", "text": b.Content}}
					stop = "end_turn"
				}
				if b.Type == "text" && b.Text == "我要退款" {
					content = []any{map[string]any{"type": "text", "text": "请提供退款金额或原充值订单号。"}}
					stop = "end_turn"
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "local", "content": content, "stop_reason": stop, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
	}))
	t.Cleanup(provider.Close)
	model := PairModelSettings{BaseURL: provider.URL, APIKey: "local-only", Model: "local", Protocol: "anthropic"}
	cfg, err := PrepareFullCustomerAgentPairConfig(source, []string{key}, profiles, model, "local_stub", "test-revision", "")
	if err != nil {
		t.Fatal(err)
	}
	if fault == "case" {
		cfg.Suite.Cases[0].Scenario.Turns[0].User += " changed"
	}
	if fault == "selection" {
		cfg.SelectionSHA256 = strings.Repeat("f", 64)
	}
	observer, capture := pairedTestObserver(t)
	if fault == "observer" {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { exportCalls.Add(1); w.WriteHeader(503) }))
		t.Cleanup(bad.Close)
		observer, err = observability.New(ctx, observability.Config{Enabled: true, CaptureContent: true, Endpoint: bad.URL + "/api/public/otel/v1/traces", PublicKey: "pk-local", SecretKey: "sk-local", HashKey: strings.Repeat("h", 32), Environment: "evaluation", SampleRate: 1, QueueSize: 256, ExportTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = observer.Shutdown(context.Background()) })
	}
	limits := pairedLimits()
	limits.MaxCalls = 8
	limits.MaxRequestBytes = 16384
	limits.InputTokenCeiling = 16384
	limits.MaxMicroCNY = 1000000
	dir := t.TempDir()
	campaign, err := OpenCampaignBudget(filepath.Join(dir, "campaign"), conversationHash([]byte("full-local:"+key+fault)), CampaignLimits{MaxBatches: 2, MaxModelCalls: 16, MaxModelMicroCNY: 2000000, MaxJEVCalls: 4, MaxJEVMicroUSD: 20000, JEVMicroUSDPerAttempt: 2688})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := campaign.Close(); err != nil {
			t.Error(err)
		}
	})
	lease, err := campaign.ReserveBatch("batch-001", PairRunContract(cfg), limits, 2)
	if err != nil {
		t.Fatal(err)
	}
	budgetContract := PairRunContract(cfg)
	if fault == "legacy-budget" {
		legacy := cfg
		legacy.SelectionScope = ""
		legacy.SelectionSHA256 = ""
		budgetContract = PairRunContract(legacy)
	}
	budget, err := OpenPairBudget(filepath.Join(lease.Dir, "budget.jsonl"), budgetContract, limits, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := budget.Close(); err != nil {
			t.Error(err)
		}
	})
	o := AgentPairOptions{AllowExecution: true, AllowJEVCalls: true, MaxJEVCalls: 2, DefinitionsDir: "../../eval/task-definitions/2026-10-12-full-dev-v1", Config: cfg, Environment: env, Budget: budget, Model: model, Observer: observer, StopOnStructuralFailure: true, StopOnObserverFailure: true, StopOnFactFailure: true, StopOnJEVFailure: true, JEVFactory: func() jev.Evaluator {
		judgeFactories.Add(1)
		if fault == "jev" {
			return policyFailureJEV{}
		}
		return jev.NewFake(localJevScript(tool, .5))
	}}
	err = RunFullCustomerCampaignBatch(ctx, lease, o)
	path := filepath.Join(lease.Dir, "run.jsonl")
	campaign.mu.Lock()
	reserved := campaign.used
	campaign.mu.Unlock()
	if reserved.ModelCalls != 8 || reserved.JEVCalls != 2 || reserved.ModelMicroCNY != 8*limits.cost(limits.InputTokenCeiling, int64(limits.MaxOutputTokens)) || reserved.JEVMicroUSD != 2*2688 {
		t.Fatal("full campaign released or changed permanent child reservation")
	}
	if fault != "" {
		if err == nil {
			t.Fatal("injected fault passed")
		}
		if _, err := campaign.ReserveBatch("batch-002", PairRunContract(cfg), limits, 2); err == nil {
			t.Fatal("campaign continued after failure")
		}
		if fault == "case" || fault == "selection" || fault == "legacy-budget" {
			if modelCalls.Load() != 0 || judgeFactories.Load() != 0 || budget.Snapshot().Calls != 0 {
				t.Fatal("invalid full selection reached provider/JEV")
			}
			for _, name := range []string{path, path + ".jev.jsonl"} {
				if _, err := os.Stat(name); !os.IsNotExist(err) {
					t.Fatal("rejected full case created execution evidence")
				}
			}
		} else {
			want := int64(2)
			if fault == "provider" {
				want = 1
			}
			if modelCalls.Load() != want {
				t.Fatalf("unexpected retry/fallback model attempts=%d want=%d", modelCalls.Load(), want)
			}
			if fault == "observer" && (observer.Stats().Failed == 0 || exportCalls.Load() == 0) {
				t.Fatal("observer failure not retained")
			}
		}
		t.Logf("full local fault=%s model_attempts=%d judge_factories=%d; campaign stopped; no real inference", fault, modelCalls.Load(), judgeFactories.Load())
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	report, err := ReportPreparedPair(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.FinishedArms != 2 {
		t.Fatal("incomplete primary pair")
	}
	for _, r := range report.Rows {
		if !r.StructuralPass || r.FactErrors != 0 || r.Status != "needs_review" || len(r.LangfuseTraceID) != 32 {
			t.Fatalf("wrong full primary result: %+v", r)
		}
	}
	j, err := loadPairJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if j.cfg.SelectionScope != FullCustomerPrimaryScope || j.cfg.SelectionSHA256 != cfg.SelectionSHA256 {
		t.Fatal("full source contract not durable")
	}
	for _, finish := range j.finished {
		r := finish.Result
		if tool == "refund_recharge" && r.Final.Own.BalanceCent-r.Initial.Own.BalanceCent != -10000 {
			t.Fatal("primary refund did not execute exactly100 yuan")
		}
		if tool == "cancel_subscription" && sameConversationBusiness(r.Initial, r.Final) {
			t.Fatal("primary subscription did not change state")
		}
		if tool == "list_models" && !sameConversationBusiness(r.Initial, r.Final) {
			t.Fatal("model catalog read changed business state")
		}
	}
	if stats := observer.Stats(); stats.Failed != 0 || stats.Dropped != 0 || stats.Accepted != stats.Exported {
		t.Fatal("successful full adapter lost local observations")
	}
	if capture != nil {
		capture.Lock()
		defer capture.Unlock()
		roots := 0
		for _, span := range capture.spans {
			attrs := map[string]string{}
			for _, a := range span.Attributes {
				attrs[a.Key] = a.Value.GetStringValue()
			}
			if attrs["langfuse.observation.metadata.run_id"] != PairRunContract(cfg) || attrs["langfuse.observation.metadata.case_key"] != key {
				t.Fatal("observation lost full selection contract association")
			}
			if span.Name == "evaluation.task" {
				roots++
			}
		}
		if roots != 2 {
			t.Fatalf("full pair root count=%d", roots)
		}
	}
	t.Logf("full local key=%s model_attempts=%d judge_factories=%d; both arms structurally valid, needs_review retained; synthetic usage", key, modelCalls.Load(), judgeFactories.Load())
}
