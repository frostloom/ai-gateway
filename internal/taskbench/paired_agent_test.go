package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
)

func TestPreparedPairRealAgentLocalBudget(t *testing.T) {
	db, rdb, st := runtimeProfileTestDatabase(t)
	profiles := loadRuntimeProfileArchive(t)
	plans := seedRuntimeCatalogue(t, db, profiles)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	token := "local-pair-control"
	t.Setenv(controlplane.Env, token)
	adminServer := httptest.NewServer(admin.Handler(st, log, token))
	defer adminServer.Close()
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Messages []agent.Message `json:"messages"`
			Max      int             `json:"max_tokens"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Max != 128 {
			t.Error("missing output bound")
		}
		msg := localToolMessage("get_balance", map[string]any{})
		if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "tool" {
			msg = agent.Message{Role: "assistant", Content: req.Messages[len(req.Messages)-1].Content}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
	}))
	defer provider.Close()
	suite, e := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := PrepareSmallPair(suite)
	if e != nil {
		t.Fatal(e)
	}
	suite = plan.Suite
	suite.Cases = suite.Cases[:1]
	prototype := agent.NewLLMClient(provider.URL, "local-only", "local-model")
	if e = prototype.ConfigureEvaluationClient(provider.Client(), 128); e != nil {
		t.Fatal(e)
	}
	metadata := agent.NewHandler(nil, agent.NewToolbox(agent.NewAdminClient(adminServer.URL)), prototype, st, rdb, log).EvaluationRuntimeMetadata()
	h := strings.Repeat("a", 64)
	a := ConversationComparisonConfig{RoutingMode: "model", EvidenceKind: "local_stub", DefinitionSHA256: suite.DefinitionSHA256, ManifestSHA256: suite.ManifestSHA256, ProfileSHA256: map[string]string{"active_standard": h}, Model: metadata.Model, ProviderProtocol: metadata.Protocol, PromptSHA256: metadata.PromptSHA256, ToolSchemaSHA256: metadata.ToolsSHA256, OutputBudget: 128, ExecutionRevision: "local-test"}
	b := a
	b.RoutingMode = "jev_selective"
	cfg := PairRunConfig{Schema: "paired-run.v1", Suite: suite, Model: a, Selective: b}
	dir := t.TempDir()
	limits := pairedLimits()
	limits.MaxCalls = 3
	limits.MaxRequestBytes = 16384
	limits.InputTokenCeiling = 32768
	budget, e := OpenPairBudget(filepath.Join(dir, "budget.jsonl"), PairRunContract(cfg), limits, http.DefaultTransport)
	if e != nil {
		t.Fatal(e)
	}
	defer budget.Close()
	factory := func(_ context.Context, arm string, _ ConversationCase) (ConversationDriver, error) {
		oracle := &agentConversationOracle{&storeConversationDouble{t: t, db: db, rdb: rdb, st: st, profiles: profiles, plans: plans}}
		model := agent.NewLLMClient(provider.URL, "local-only", "local-model")
		if err := model.ConfigureEvaluationClient(&http.Client{Transport: budget, Timeout: 10 * time.Second}, 128); err != nil {
			return nil, err
		}
		handler := agent.NewHandler(nil, agent.NewToolbox(agent.NewAdminClient(adminServer.URL)), model, st, rdb, log)
		if err := handler.SetRoutingMode(arm); err != nil {
			return nil, err
		}
		handler.SetAdminAccess(token, 0)
		if arm == "jev_selective" {
			handler.SetJev(jev.NewFake(localJevScript("get_balance", .99)))
		}
		wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Agent-Internal", token)
			r.Header.Set("X-Agent-Tenant", fmt.Sprint(oracle.f.TenantID))
			handler.ServeHTTP(w, r)
		})
		return NewHandlerConversationDriver(wrapper, oracle)
	}
	p := filepath.Join(dir, "pair.jsonl")
	if e = RunPreparedPair(ctx, p, cfg, budget, factory); e != nil {
		t.Fatal(e)
	}
	r, e := ReportPreparedPair(p)
	if e != nil {
		t.Fatal(e)
	}
	if r.FinishedArms != 2 || calls.Load() != 2 || r.CompletionRate != nil || r.Rows[0].Status != "needs_review" || r.Rows[1].Status != "needs_review" {
		t.Fatalf("report/calls: %+v %d", r, calls.Load())
	}
	if r.Rows[0].ModelAttempts == nil || *r.Rows[0].ModelAttempts != 2 || r.Rows[1].ModelAttempts == nil || *r.Rows[1].ModelAttempts != 0 {
		t.Fatal("wrong guarded model attempts")
	}
	if e = RunPreparedPair(ctx, p, cfg, budget, factory); e != nil || calls.Load() != 2 {
		t.Fatal("resume repeated provider")
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "sk-runtime-") {
		t.Fatal("fixture API key persisted")
	}
	j, e := loadPairJournal(p)
	if e != nil {
		t.Fatal(e)
	}
	left := j.finished[suite.Cases[0].Key()+":model"].Result
	right := j.finished[suite.Cases[0].Key()+":jev_selective"].Result
	if left.Fixture.TenantID == right.Fixture.TenantID {
		t.Fatal("arms reused mutated account")
	}
	t.Logf("two real-agent arms; local model requests=2; external=0; semantic review pending")
}
