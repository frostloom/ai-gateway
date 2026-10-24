package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in: sends only explicit synthetic/public data to the existing local project.
func TestKnowledgeLangfuseReadback(t *testing.T) {
	path := os.Getenv("KNOWLEDGE_LANGFUSE_ENV")
	if path == "" {
		t.Skip("opt-in local Langfuse acceptance")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
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
	vals["AGENT_LANGFUSE_SAMPLE_RATE"] = "1"
	vals["AGENT_LANGFUSE_ENVIRONMENT"] = "evaluation"
	vals["AGENT_RELEASE"] = "knowledge-synthetic-verification"
	type receipt struct {
		Capture      bool   `json:"capture"`
		QueryKind    string `json:"query_kind"`
		TraceID      string `json:"trace_id"`
		Observations int    `json:"observations"`
		Exported     uint64 `json:"exported"`
		Dropped      uint64 `json:"dropped"`
		Failed       uint64 `json:"failed"`
	}
	var receipts []receipt
	for _, tc := range []struct {
		capture     bool
		query, kind string
	}{{false, "退订退款", "known"}, {true, "退订退款", "known"}, {true, "外星天气", "no_match"}, {true, strings.Repeat("中", 257), "unavailable"}} {
		vals["AGENT_LANGFUSE_CAPTURE_CONTENT"] = fmt.Sprint(tc.capture)
		cfg, e := observability.ConfigFromEnv(func(k string) string { return vals[k] })
		if e != nil {
			t.Fatal("invalid local telemetry settings")
		}
		o, e := observability.New(context.Background(), cfg)
		if e != nil {
			t.Fatal(e)
		}
		tb := NewToolbox(nil)
		enableKnowledge(t, tb)
		ctx, turn := o.StartTurn(context.Background(), "model")
		ctx = context.WithValue(ctx, knowledgeCorpusKey{}, tb.knowledge)
		stats := &TurnTelemetry{}
		ctx = context.WithValue(ctx, telemetryKey{}, stats)
		turn.Input("SYNTHETIC public knowledge verification")
		result, searchErr := tb.Run(ctx, "search_platform_knowledge", map[string]any{"query": tc.query}, 1, "")
		if tc.kind != "unavailable" && searchErr != nil {
			t.Fatal(searchErr)
		}
		if searchErr == nil {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req chatRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					t.Error("invalid local model request")
				}
				if tc.kind == "known" && len(requestedKnowledge(req.Messages)) != 1 {
					t.Error("lost actual knowledge context")
				}
				io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"SYNTHETIC answer only"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`)
			}))
			c := NewLLMClient(srv.URL, "local-unused", "local-stub")
			args, _ := json.Marshal(map[string]any{"query": tc.query})
			msgs := []Message{*toolCallMsg("lf-k", "search_platform_knowledge", string(args)), {Role: "tool", ToolCallID: "lf-k", Content: result}}
			_, e = c.Chat(ctx, msgs, nil)
			srv.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		if tc.capture {
			_, op := observability.Start(ctx, "knowledge.sanitizer_probe", "span")
			op.KnowledgeContent("password=synthetic-value user@example.com " + strings.Repeat("中", 1000))
			op.End(nil)
		}
		turn.Output("SYNTHETIC answer only")
		trace := turn.TraceID()
		turn.End(200, observability.Summary{})
		if e = o.ForceFlush(context.Background()); e != nil {
			t.Fatal(e)
		}
		s := o.Stats()
		if s.Dropped != 0 || s.Failed != 0 || s.Accepted != s.Exported {
			t.Fatalf("incomplete local export %+v", s)
		}
		if e = o.Shutdown(context.Background()); e != nil {
			t.Fatal(e)
		}
		endpoint, _ := url.Parse(cfg.Endpoint)
		endpoint.Path = "/api/public/v2/observations"
		q := endpoint.Query()
		q.Set("traceId", trace)
		q.Set("limit", "100")
		q.Set("fields", "core,basic,metadata,usage,io")
		endpoint.RawQuery = q.Encode()
		type observation struct {
			Name     string         `json:"name"`
			Input    any            `json:"input"`
			Output   any            `json:"output"`
			Metadata map[string]any `json:"metadata"`
		}
		var rows []observation
		t.Logf("exported capture=%t kind=%s trace=%s spans=%d", tc.capture, tc.kind, trace, s.Exported)
		client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for attempt := 0; attempt < 25; attempt++ {
			req, _ := http.NewRequest("GET", endpoint.String(), nil)
			req.SetBasicAuth(cfg.PublicKey, cfg.SecretKey)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("Langfuse readback transport failed")
			}
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("Langfuse readback HTTP %d", resp.StatusCode)
			}
			var body struct {
				Data []observation `json:"data"`
			}
			if json.Unmarshal(data, &body) != nil {
				t.Fatal("invalid readback JSON")
			}
			if len(body.Data) >= int(s.Exported) {
				rows = body.Data
				break
			}
			time.Sleep(2 * time.Second)
		}
		if len(rows) < int(s.Exported) {
			t.Fatalf("trace %s not fully ingested: got=%d want=%d", trace, len(rows), s.Exported)
		}
		var expected struct {
			Hits []struct {
				ChunkID string `json:"chunk_id"`
				Text    string `json:"text"`
			}
		}
		json.Unmarshal([]byte(result), &expected)
		texts := map[string]string{}
		for _, hit := range expected.Hits {
			texts[hit.ChunkID] = hit.Text
		}
		chunks, requests, sanitized := 0, 0, false
		for _, row := range rows {
			if !tc.capture && (row.Input != nil || row.Output != nil) {
				t.Fatal("content-off trace retained input/output")
			}
			if row.Name == "knowledge.chunk" {
				chunks++
				output := fmt.Sprint(row.Output)
				var decoded string
				if json.Unmarshal([]byte(output), &decoded) == nil {
					output = decoded
				}
				if tc.capture && output != texts[fmt.Sprint(row.Metadata["knowledge_chunk_id"])] {
					t.Fatal("retrieved chunk not readable")
				}
				if row.Metadata["knowledge_source_sha256"] == nil {
					t.Fatal("missing source digest")
				}
			}
			if row.Name == "knowledge.request_context" {
				requests++
				if row.Metadata["knowledge_stage"] != "included_in_request" {
					t.Fatal("request attribution mismatch")
				}
			}
			if row.Name == "knowledge.sanitizer_probe" {
				sanitized = true
				if fmt.Sprint(row.Metadata["knowledge_redacted"]) != "true" || fmt.Sprint(row.Metadata["knowledge_clipped"]) != "true" || strings.Contains(fmt.Sprint(row.Output), "synthetic-value") {
					t.Fatal("redaction/clipping not visible")
				}
			}
		}
		if tc.kind == "known" && (chunks == 0 || requests == 0) {
			t.Fatal("missing knowledge observations")
		}
		if tc.capture && !sanitized {
			t.Fatal("sanitizer observation missing")
		}
		receipts = append(receipts, receipt{tc.capture, tc.kind, trace, len(rows), s.Exported, s.Dropped, s.Failed})
		t.Logf("capture=%t kind=%s trace=%s observations=%d", tc.capture, tc.kind, trace, len(rows))
	}
	if path := os.Getenv("KNOWLEDGE_LANGFUSE_RECEIPT"); path != "" {
		b, _ := json.MarshalIndent(receipts, "", "  ")
		if e = os.WriteFile(path, append(b, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
