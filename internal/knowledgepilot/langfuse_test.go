package knowledgepilot

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only. Sends synthetic responses and public knowledge to the
// existing local project; never loads or calls a model/JEV provider.
func TestKnowledgePilotLangfuseReadback(t *testing.T) {
	path := os.Getenv("KNOWLEDGE_PILOT_LANGFUSE_ENV")
	if path == "" {
		t.Skip("opt-in local observer readback")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("private observer configuration unavailable")
	}
	vals := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	vals["AGENT_LANGFUSE_ENABLED"] = "true"
	vals["AGENT_LANGFUSE_CAPTURE_CONTENT"] = "true"
	vals["AGENT_LANGFUSE_SAMPLE_RATE"] = "1"
	vals["AGENT_LANGFUSE_ENVIRONMENT"] = "evaluation"
	vals["AGENT_RELEASE"] = "knowledge-pilot-synthetic-verification"
	cfg, e := observability.ConfigFromEnv(func(k string) string { return vals[k] })
	if e != nil {
		t.Fatal("observer settings invalid")
	}
	u, e := url.Parse(cfg.Endpoint)
	if e != nil || (u.Hostname() != "localhost" && (net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback())) {
		t.Fatal("local observer required")
	}
	obs, e := observability.New(context.Background(), cfg)
	if e != nil {
		t.Fatal("observer unavailable")
	}
	defer obs.Shutdown(context.Background())
	receipt, e := runLocalObserved(t, []string{"K001"}, func(w http.ResponseWriter, r *http.Request) {
		var x struct {
			Messages []agent.Message `json:"messages"`
			Tools    []agent.Tool    `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&x)
		tools, _ := json.Marshal(x.Tools)
		if strings.Contains(string(tools), "search_platform_knowledge") && x.Messages[len(x.Messages)-1].Role != "tool" {
			var tc agent.ToolCall
			tc.ID = "k-public"
			tc.Type = "function"
			tc.Function.Name = "search_platform_knowledge"
			tc.Function.Arguments = `{"query":"平台功能"}`
			modelAnswer(w, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
			return
		}
		modelAnswer(w, agent.Message{Role: "assistant", Content: "SYNTHETIC public answer [KB-001]"})
	}, obs)
	if e != nil {
		t.Fatal(e)
	}
	stats := obs.Stats()
	if stats.Dropped != 0 || stats.Failed != 0 {
		t.Fatal("export incomplete")
	}
	type row struct {
		ID       string         `json:"id"`
		Name     string         `json:"name"`
		Metadata map[string]any `json:"metadata"`
		Output   any            `json:"output"`
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var readbacks []Readback
	total := 0
	for _, task := range receipt.Recording.Tasks {
		if task.Telemetry.LangfuseTraceID == "" {
			t.Fatal("missing trace")
		}
		var rows []row
		var response []byte
		for attempt := 0; attempt < 25; attempt++ {
			endpoint := *u
			endpoint.Path = "/api/public/v2/observations"
			q := url.Values{"traceId": {task.Telemetry.LangfuseTraceID}, "limit": {"100"}, "fields": {"core,basic,metadata,usage,io"}}
			endpoint.RawQuery = q.Encode()
			req, _ := http.NewRequest("GET", endpoint.String(), nil)
			req.SetBasicAuth(cfg.PublicKey, cfg.SecretKey)
			resp, e := client.Do(req)
			if e != nil {
				t.Fatal("observer readback unavailable")
			}
			response, e = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			resp.Body.Close()
			if e != nil || resp.StatusCode != 200 {
				t.Fatalf("readback HTTP %d", resp.StatusCode)
			}
			var result struct {
				Data []row `json:"data"`
			}
			if json.Unmarshal(response, &result) != nil {
				t.Fatal("invalid readback")
			}
			rows = result.Data
			names := map[string]int{}
			for _, r := range rows {
				names[r.Name]++
			}
			if names["agent.turn"] > 0 && names["llm.attempt"] >= task.Telemetry.LLMCalls && (task.Arm == "a" || (names["knowledge.chunk"] > 0 && names["knowledge.request_context"] > 0)) {
				break
			}
			time.Sleep(2 * time.Second)
		}
		names := map[string]int{}
		expected := map[string]knowledge.Hit{}
		var traces []struct{ Tool, Result string }
		json.Unmarshal(task.Trace, &traces)
		for _, tr := range traces {
			if tr.Tool == "search_platform_knowledge" {
				var r knowledge.Result
				json.Unmarshal([]byte(tr.Result), &r)
				for _, hit := range r.Hits {
					expected[hit.ChunkID] = hit
				}
			}
		}
		for _, r := range rows {
			names[r.Name]++
			if r.Name == "knowledge.chunk" {
				id := fmt.Sprint(r.Metadata["knowledge_chunk_id"])
				hit, ok := expected[id]
				output := fmt.Sprint(r.Output)
				var decoded string
				if json.Unmarshal([]byte(output), &decoded) == nil {
					output = decoded
				}
				if !ok || output != hit.Text || fmt.Sprint(r.Metadata["knowledge_source_sha256"]) != hit.SHA256 {
					t.Fatal("knowledge body/source mismatch")
				}
			}
		}
		if names["agent.turn"] == 0 || names["llm.attempt"] < task.Telemetry.LLMCalls || (task.Arm == "b" && (names["knowledge.chunk"] != len(expected) || names["knowledge.request_context"] == 0)) {
			t.Fatalf("missing task observations: %v", names)
		}
		total += len(rows)
		readbacks = append(readbacks, Readback{TaskID: task.Identity, TraceID: task.Telemetry.LangfuseTraceID, Verified: true, Observations: len(rows), ReceiptSHA: SHA(response)})
		t.Logf("task=%s trace=%s observations=%d", task.Identity, task.Telemetry.LangfuseTraceID, len(rows))
	}
	if uint64(total) != stats.Exported {
		t.Fatalf("observation coverage %d/%d", total, stats.Exported)
	}
	if p := os.Getenv("KNOWLEDGE_PILOT_READBACK_RECEIPT"); p != "" {
		b, _ := json.MarshalIndent(map[string]any{"readback": readbacks, "exported": stats.Exported, "dropped": stats.Dropped, "failed": stats.Failed}, "", "  ")
		if e = os.WriteFile(p, append(b, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
