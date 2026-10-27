package knowledgecompare

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, fn func(*http.Request) string) (Options, *int) {
	t.Helper()
	s, e := LoadSuite("../..")
	if e != nil {
		t.Fatal(e)
	}
	c := testContract()
	c.SuiteSHA = s.SHA
	cap, e := Acquire(filepath.Join(t.TempDir(), "claim"), c, auth(c))
	if e != nil {
		t.Fatal(e)
	}
	n := new(int)
	guard, e := NewGuard(filepath.Join(t.TempDir(), "events"), cap, roundFunc(func(r *http.Request) (*http.Response, error) {
		*n++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fn(r)))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { guard.Close() })
	return Options{Root: "../..", Suite: s, Guard: guard, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", Out: filepath.Join(t.TempDir(), "run"), APIKey: "local-unused", LocalVerification: true}, n
}
func encode(msg agent.Message) string {
	b, _ := json.Marshal(map[string]any{"model": "closed-transport-synthetic", "choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20, "prompt_cache_hit_tokens": 0, "prompt_cache_miss_tokens": 100}})
	return string(b)
}
func TestRunnerUsesRealHandlersAndNoGold(t *testing.T) {
	o, n := fixture(t, func(r *http.Request) string {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "required_facts") {
			t.Error("gold leaked")
		}
		var b struct {
			Messages []agent.Message `json:"messages"`
			Tools    []agent.Tool    `json:"tools"`
		}
		json.Unmarshal(raw, &b)
		tools, _ := json.Marshal(b.Tools)
		if strings.Contains(string(tools), "search_platform_knowledge") && b.Messages[len(b.Messages)-1].Role != "tool" {
			var tc agent.ToolCall
			tc.ID = "k"
			tc.Type = "function"
			tc.Function.Name = "search_platform_knowledge"
			tc.Function.Arguments = `{"query":"平台功能"}`
			return encode(agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
		}
		return encode(agent.Message{Role: "assistant", Content: "SYNTHETIC"})
	})
	o.CaseIDs = []string{"K001"}
	r, e := Run(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Tasks) != 2 || *n != 3 || r.Complete || !r.Cleanup {
		t.Fatalf("%+v calls%d", r, *n)
	}
	if r.Tasks[0].Tenant == r.Tasks[1].Tenant {
		t.Fatal("shared account")
	}
}
func TestRunnerQualityPreviewNegativeCleansBeforeNextArm(t *testing.T) {
	o, n := fixture(t, func(r *http.Request) string {
		var tc agent.ToolCall
		tc.ID = "wrong"
		tc.Type = "function"
		tc.Function.Name = "cancel_subscription"
		tc.Function.Arguments = `{}`
		return encode(agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
	})
	o.CaseIDs = []string{"S010"}
	r, e := Run(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Tasks) != 2 || *n != 2 || r.Tasks[0].StatePass || !r.Tasks[0].OwnedCleanup || !r.Tasks[0].BusinessUnchanged {
		t.Fatal("preview safety proof lost")
	}
}
func TestRunnerNativeProviderFailureStopsAll(t *testing.T) {
	o, n := fixture(t, func(r *http.Request) string { return `{"choices":[]}` })
	o.CaseIDs = []string{"K001", "K002"}
	r, e := Run(context.Background(), o)
	if e == nil || *n != 1 || r.Status != "stopped" || len(r.Tasks) != 1 || !r.Cleanup {
		t.Fatal("unsafe continuation", r.Status, e)
	}
}
func TestRunnerMultiTurnPendingCancellation(t *testing.T) {
	o, _ := fixture(t, func(r *http.Request) string {
		var tc agent.ToolCall
		tc.ID = "recharge"
		tc.Type = "function"
		tc.Function.Name = "recharge"
		tc.Function.Arguments = `{"amount_money":100}`
		return encode(agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
	})
	o.CaseIDs = []string{"S013"}
	r, e := Run(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range r.Tasks {
		if len(task.Turns) != 2 || task.Turns[1].Pending || !task.OwnedCleanup || !task.BusinessUnchanged {
			t.Fatal("cancel failed")
		}
	}
}

func TestReviewHidesRuntimeAndConditionMetadata(t *testing.T) {
	d := t.TempDir()
	r := Result{Tasks: []Task{{CaseID: "K001", Arm: "b", Turns: []Turn{{Reply: "answer", Telemetry: agent.TurnTelemetry{Runtime: &agent.RuntimeMetadata{RoutingMode: "model"}}}}}}}
	if e := writeReview(d, r); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(d, "review-package.json"))
	if strings.Contains(string(b), "runtime") || strings.Contains(string(b), "telemetry") {
		t.Fatal("runtime leaks arm")
	}
}

// Explicit full schedule verification; never uses a network provider. Keep the
// 200-owned-task database matrix out of the default unit-test runtime.
func TestFull100ClosedTransportMatrix(t *testing.T) {
	if os.Getenv("KNOWLEDGE_ANSWER100_FULL_LOCAL") != "1" {
		t.Skip("explicit full closed-transport fixture matrix")
	}
	o, n := fixture(t, func(r *http.Request) string {
		return encode(agent.Message{Role: "assistant", Content: "SYNTHETIC: not a real model answer; no factual-quality claim"})
	})
	if out := os.Getenv("KNOWLEDGE_ANSWER100_LOCAL_OUT"); out != "" {
		o.Out = out
	}
	r, e := Run(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Tasks) != 200 || r.ActualUserTurns != 224 || !r.Complete || !r.Cleanup || *n == 0 || r.EvidenceKind != "closed_transport_synthetic" {
		t.Fatalf("tasks%d turns%d calls%d cleanup%v", len(r.Tasks), r.ActualUserTurns, *n, r.Cleanup)
	}
	for _, task := range r.Tasks {
		if !task.BusinessUnchanged || !task.OwnedCleanup {
			t.Fatal("unsafe task")
		}
	}
}

func TestReviewRemovesRetrievalAudit(t *testing.T) {
	d := t.TempDir()
	snap := json.RawMessage(`{"state":{"own":{"audits":[{"tool":"search_platform_knowledge","result":"SECRET_KNOWLEDGE"}],"balance":2500}},"pending_action":null}`)
	r := Result{Tasks: []Task{{CaseID: "K001", Arm: "b", Before: snap, Turns: []Turn{{Snapshot: snap}}}}}
	if e := writeReview(d, r); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(d, "review-package.json"))
	if strings.Contains(string(b), "search_platform_knowledge") || strings.Contains(string(b), "SECRET_KNOWLEDGE") {
		t.Fatal("retrieval audit reveals arm")
	}
}
func TestSkippedUserTurnCannotBeComplete(t *testing.T) {
	r := Result{Tasks: make([]Task, 200), ActualUserTurns: 223}
	r.setCoverage()
	if r.Complete || !r.AllTasksDisposed {
		t.Fatal("incomplete conversation called complete")
	}
}
func TestAdminFaultBlocksFollowingModelRequest(t *testing.T) {
	g, n := testGuard(t, goodNative)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "synthetic", 500) }))
	defer backend.Close()
	proxy := adminProxy(backend.URL, g)
	defer proxy.Close()
	resp, e := http.Get(proxy.URL)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if _, e := call(g, 1); e == nil || *n != 0 || !g.State().Stopped {
		t.Fatal("model called after business tool technical failure")
	}
}

func TestObserverFaultStopsAndPersistsBoundedDiagnostics(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "synthetic receiver failure", 503) }))
	defer endpoint.Close()
	obs, e := observability.New(context.Background(), observability.Config{Enabled: true, Endpoint: endpoint.URL + "/api/public/otel/v1/traces", PublicKey: "synthetic-public", SecretKey: "synthetic-secret", HashKey: strings.Repeat("x", 32), Release: "synthetic-observer-stop", Environment: "evaluation", SampleRate: 1, ExportTimeout: 200 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	defer obs.Shutdown(context.Background())
	o, n := fixture(t, func(r *http.Request) string { return encode(agent.Message{Role: "assistant", Content: "SYNTHETIC"}) })
	o.Observer = obs
	o.CaseIDs = []string{"K001", "K002"}
	result, e := Run(context.Background(), o)
	if e == nil || *n != 1 || len(result.Tasks) != 1 || result.FailureStage != "observation_flush" || result.ObserverStats == nil || result.ObserverStats.Failed == 0 || !result.Budget.Stopped || !result.Cleanup {
		t.Fatalf("observer failure not faithfully persisted: err%v calls%d stage%s", e, *n, result.FailureStage)
	}
	b, err := os.ReadFile(filepath.Join(o.Out, "recording.json"))
	if err != nil || !strings.Contains(string(b), `"failure_stage": "observation_flush"`) {
		t.Fatal("failure stage absent from recording")
	}
}

func TestCompletionRunnerSkipsRecordedArm(t *testing.T) {
	c, p, r, j := completionFixture(t)
	cap, e := AcquireCompletion(filepath.Join(t.TempDir(), "claim"), c, auth(c), p, r, j)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	g, e := NewGuard(filepath.Join(t.TempDir(), "events"), cap, roundFunc(func(req *http.Request) (*http.Response, error) {
		n++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(encode(agent.Message{Role: "assistant", Content: "SYNTHETIC"})))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	o, _ := fixture(t, func(req *http.Request) string { return encode(agent.Message{Role: "assistant", Content: "UNUSED"}) })
	o.Guard = g
	o.CaseIDs = []string{"K001"}
	result, e := Run(context.Background(), o)
	if e != nil || len(result.Tasks) != 1 || result.Tasks[0].Arm != "b" || n != 1 || result.PriorTasks != 1 || result.PriorUserTurns != 1 || !result.Cleanup {
		t.Fatalf("old arm rerun or coverage lost: %v tasks%d calls%d", e, len(result.Tasks), n)
	}
}
