package knowledgepilot

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runLocal(t *testing.T, ids []string, handler http.HandlerFunc) (Receipt, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	_, s := template(t)
	in, e := LoadInputs("../..", s)
	if e != nil {
		t.Fatal(e)
	}
	c, e := OpenLocalClaim(filepath.Join(t.TempDir(), "claim"), srv.URL, s)
	if e != nil {
		t.Fatal(e)
	}
	j, e := OpenJournal(filepath.Join(t.TempDir(), "attempts.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	tr := NewTransport(http.DefaultTransport, c, s.Limits, j)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return Run(ctx, RunOptions{AllowExecution: true, Root: "../..", Inputs: in, Claim: c, Transport: tr, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", Out: filepath.Join(t.TempDir(), "run"), CaseIDs: ids})
}
func modelAnswer(w http.ResponseWriter, msg agent.Message) {
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "prompt_tokens_details": map[string]any{"cached_tokens": 0}}, "model": "local-synthetic"})
}
func TestRunnerRealHandlerOffOn(t *testing.T) {
	var off, on, contextSeen atomic.Int32
	r, e := runLocal(t, []string{"K001"}, func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Messages []agent.Message `json:"messages"`
			Tools    []agent.Tool    `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&x) != nil {
			t.Error("bad request")
		}
		b, _ := json.Marshal(x.Tools)
		if !strings.Contains(string(b), "search_platform_knowledge") {
			off.Add(1)
			modelAnswer(w, agent.Message{Role: "assistant", Content: "SYNTHETIC baseline"})
			return
		}
		on.Add(1)
		if x.Messages[len(x.Messages)-1].Role == "tool" {
			if !strings.Contains(x.Messages[len(x.Messages)-1].Content, "KB-") {
				t.Errorf("no knowledge in next request: %s", x.Messages[len(x.Messages)-1].Content)
			}
			contextSeen.Add(1)
			modelAnswer(w, agent.Message{Role: "assistant", Content: "SYNTHETIC [KB-001]"})
			return
		}
		var tc agent.ToolCall
		tc.ID = "k1"
		tc.Type = "function"
		tc.Function.Name = "search_platform_knowledge"
		tc.Function.Arguments = `{"query":"平台功能"}`
		modelAnswer(w, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
	})
	if e != nil {
		t.Fatal(e)
	}
	if off.Load() != 1 || on.Load() != 2 || contextSeen.Load() != 1 || len(r.Recording.Tasks) != 2 || r.Recording.Complete {
		t.Fatalf("unexpected paired local run: %+v off=%d on=%d", r, off.Load(), on.Load())
	}
	if len(r.Recording.Tasks[1].Searches) != 1 {
		t.Fatal("retrieval missing")
	}
}
func TestRunnerIndependentStateAndPending(t *testing.T) {
	r, e := runLocal(t, []string{"S011", "S012"}, func(w http.ResponseWriter, r *http.Request) {
		modelAnswer(w, agent.Message{Role: "assistant", Content: "SYNTHETIC"})
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Recording.Tasks) != 4 || !r.Cleanup {
		t.Fatal(r)
	}
	seen := map[uint64]bool{}
	for _, x := range r.Recording.Tasks {
		if seen[x.Tenant] {
			t.Fatal("shared account")
		}
		seen[x.Tenant] = true
		if x.CaseID == "S011" {
			if !x.PendingBefore || !x.PendingAfter || x.StateOK {
				t.Fatal("unhandled frozen cancellation was falsely counted as passing")
			}
		} else if x.PendingBefore || x.PendingAfter || !x.StateOK {
			t.Fatal("empty pending fixture changed")
		}
		if x.Telemetry.JevCalls != 0 {
			t.Fatal("JEV called")
		}

	}
}
func TestRunnerNoJEVOrGoldLeak(t *testing.T) {
	r, e := runLocal(t, []string{"K001"}, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "required_facts") || strings.Contains(string(b), "forbidden_claims") {
			t.Error("gold leaked")
		}
		modelAnswer(w, agent.Message{Role: "assistant", Content: "SYNTHETIC"})
	})
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range r.Recording.Tasks {
		if x.Telemetry.JevCalls != 0 || x.Runtime.RoutingMode != "model" {
			t.Fatal("JEV path enabled")
		}
	}
}
func TestRunnerStopsOnProviderFailureAndWrites(t *testing.T) {
	var n atomic.Int32
	r, e := runLocal(t, []string{"K001", "K002"}, func(w http.ResponseWriter, r *http.Request) { n.Add(1); http.Error(w, "synthetic unavailable", 503) })
	if e == nil || n.Load() != 1 || r.Status != "stopped" || !r.Cleanup || len(r.Recording.Tasks) != 1 {
		t.Fatalf("failure was lost or retried: %+v %v calls=%d", r, e, n.Load())
	}
	if len(r.Recording.Events) != 2 || r.Recording.Events[1].Kind != "failed" {
		t.Fatal("lost failed attempt")
	}
	// Business changes are independently detected, never inferred from HTTP success.
	a := []byte(`{"balance":25}`)
	b := []byte(`{"balance":26}`)
	if UnchangedBusiness(a, b) {
		t.Fatal("write not detected")
	}
}
func TestRunnerDefaultRefusesBeforeFixture(t *testing.T) {
	r, e := Run(context.Background(), RunOptions{})
	if e == nil || r.Status != "stopped" {
		t.Fatal(fmt.Sprint(r, e))
	}
}
func TestRunnerFiveIterationBound(t *testing.T) {
	var calls atomic.Int32
	r, e := runLocal(t, []string{"D011"}, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var tc agent.ToolCall
		tc.ID = fmt.Sprintf("read-%d", calls.Load())
		tc.Type = "function"
		tc.Function.Name = "get_recent_bills"
		tc.Function.Arguments = fmt.Sprintf(`{"limit":%d}`, (calls.Load()-1)%5+1)
		modelAnswer(w, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
	})
	if e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 10 {
		t.Fatal("handler exceeded five per arm or stopped early", calls.Load())
	}
	for _, task := range r.Recording.Tasks {
		if task.Telemetry.LLMCalls != 5 || len(task.Attempts) != 5 {
			t.Fatal("attempt count differs")
		}
	}
}
