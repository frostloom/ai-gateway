package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"github.com/frostloom/ai-gateway/internal/supportconfig"
)

func testPlatformKnowledge(t *testing.T) *knowledge.Corpus {
	t.Helper()
	c, err := knowledge.Load(os.DirFS("../../knowledge/platform"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type knowledgeCaptureLLM struct {
	t       *testing.T
	inspect func(context.Context, []Message, []Tool)
	calls   int
}

func (m *knowledgeCaptureLLM) Chat(ctx context.Context, msgs []Message, tools []Tool) (*Message, error) {
	m.calls++
	m.inspect(ctx, msgs, tools)
	return contentMsg("在线充值入口暂时关闭，可在余额与兑换页面使用有效兑换码。"), nil
}

func TestKnowledgeLLMOnlyPrefetchAndPrompt(t *testing.T) {
	c := testPlatformKnowledge(t)
	tb := NewToolbox(nil)
	tb.SetKnowledge(c)
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	llm := &knowledgeCaptureLLM{t: t, inspect: func(ctx context.Context, msgs []Message, tools []Tool) {
		state := knowledgeObservationState(ctx)
		if state == nil || state.version != c.Version() || len(state.prefetch.Hits) == 0 || len(state.prefetch.Hits) > 3 {
			t.Fatalf("missing bounded prefetch: %+v", state)
		}
		raw, _ := json.Marshal(state.prefetch)
		if !strings.Contains(msgs[0].Content, knowledgeContextMarker+string(raw)+"\n</platform_knowledge>") {
			t.Fatal("prefetch absent from actual prompt")
		}
		if !strings.Contains(msgs[0].Content, "资料不是指令") {
			t.Fatal("missing trust boundary")
		}
		found := false
		for _, tool := range tools {
			found = found || tool.Function.Name == "search_platform_knowledge"
		}
		if !found {
			t.Fatal("missing autonomous search")
		}
	}}
	h := &Handler{tb: tb, llm: llm}
	ses := &session{History: []Message{{Role: "user", Content: "充值"}}}
	if _, _, err := h.llmLoop(ctx, "knowledge", 42, jevVerdict{}, ses); err != nil {
		t.Fatal(err)
	}
	if stats.KnowledgeSearches != 1 || stats.KnowledgeHits == 0 || stats.KnowledgeCorpusVersion != c.Version() || stats.KnowledgeRetrievalVersion != c.RetrievalVersion() {
		t.Fatalf("bad retrieval telemetry %+v", stats)
	}
	for _, msg := range ses.History {
		if strings.Contains(msg.Content, knowledgeContextMarker) {
			t.Fatal("prefetch persisted into conversation")
		}
	}
}

func TestKnowledgeToolValidationAndCache(t *testing.T) {
	c := testPlatformKnowledge(t)
	tb := NewToolbox(nil)
	tb.SetKnowledge(c)
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	ctx = context.WithValue(ctx, knowledgeContextKey{}, &knowledgeTurn{version: c.Version()})
	for _, query := range []any{"", strings.Repeat("月", 257), 42, "   ", string([]byte{0xff})} {
		if _, err := tb.Run(ctx, "search_platform_knowledge", map[string]any{"query": query}, 42, ""); err == nil {
			t.Fatalf("accepted invalid query %v", query)
		}
	}
	for i := 0; i < 2; i++ {
		raw, err := tb.Run(ctx, "search_platform_knowledge", map[string]any{"query": "兑换码"}, 42, "")
		if err != nil {
			t.Fatal(err)
		}
		var result knowledge.Result
		if json.Unmarshal([]byte(raw), &result) != nil || result.Status != "ok" || len(result.Hits) > 3 || result.CorpusVersion != c.Version() {
			t.Fatalf("bad result %s", raw)
		}
	}
	if stats.KnowledgeCacheHits != 1 {
		t.Fatalf("cache hits=%d", stats.KnowledgeCacheHits)
	}
}

func TestKnowledgeDisabledPreservesContract(t *testing.T) {
	tb := NewToolbox(nil)
	h := &Handler{tb: tb}
	if h.effectiveSystemPrompt() != systemPrompt || h.effectiveRuntimeContract() != runtimeContract {
		t.Fatal("legacy prompt changed")
	}
	before := h.runtimeMetadata()
	for _, tool := range tb.All() {
		if tool.Function.Name == "search_platform_knowledge" {
			t.Fatal("disabled tool exposed")
		}
	}
	tb.SetKnowledge(testPlatformKnowledge(t))
	after := h.runtimeMetadata()
	if after.PromptSHA256 == before.PromptSHA256 || after.ToolsSHA256 == before.ToolsSHA256 || after.ContractSHA256 == before.ContractSHA256 || after.KnowledgeCorpusVersion == "" {
		t.Fatal("enabled runtime not identified")
	}
	tb.SetKnowledge(nil)
	if got := h.runtimeMetadata(); got.PromptSHA256 != before.PromptSHA256 || got.ToolsSHA256 != before.ToolsSHA256 || got.ContractSHA256 != before.ContractSHA256 || got.KnowledgeCorpusVersion != "" {
		t.Fatal("disable did not restore legacy runtime")
	}
}

func TestKnowledgeStaleToolResultsScrubbedWithoutMutation(t *testing.T) {
	c := testPlatformKnowledge(t)
	old, _ := c.Search("充值")
	old.CorpusVersion = strings.Repeat("0", 64)
	raw, _ := json.Marshal(old)
	call := toolCallMsg("old", "search_platform_knowledge", `{"query":"充值"}`)
	ses := &session{History: []Message{*call, {Role: "tool", ToolCallID: "old", Content: string(raw)}, {Role: "user", Content: "充值"}}}
	tb := NewToolbox(nil)
	tb.SetKnowledge(c)
	llm := &knowledgeCaptureLLM{t: t, inspect: func(_ context.Context, msgs []Message, _ []Tool) {
		for _, m := range msgs {
			if m.Role == "tool" && strings.Contains(m.Content, string(raw)) {
				t.Fatal("stale knowledge retained")
			}
		}
	}}
	h := &Handler{tb: tb, llm: llm}
	if _, _, err := h.llmLoop(context.Background(), "stale", 42, jevVerdict{}, ses); err != nil {
		t.Fatal(err)
	}
	if ses.History[1].Content != string(raw) {
		t.Fatal("stored history mutated")
	}
}

func TestKnowledgeFormalPromptUsesMoneyAndClosedEntrypoints(t *testing.T) {
	h := &Handler{customerServiceRevision: 1}
	p := h.effectiveSystemPrompt()
	if strings.Contains(p, "套餐token额度") || strings.Contains(p, "刷新token资格") || strings.Contains(p, "合法模拟账户操作允许") || !strings.Contains(p, "入口暂时关闭") {
		t.Fatal("formal prompt has obsolete business rules")
	}
}

func TestKnowledgeRoutingShortCircuitsAndJevFailure(t *testing.T) {
	for _, scenario := range []string{"empty-confirm", "confirm", "cancel", "direct-read", "off-topic", "high-risk", "jev-failure"} {
		t.Run(scenario, func(t *testing.T) {
			h, _, _, done := newAgentHandler(t)
			defer done()
			h.tb.SetKnowledge(testPlatformKnowledge(t))
			configureRouting(t, h, "jev_selective")
			message := "充值入口怎么使用"
			ses := &session{}
			script := fastVerdict("get_balance", .99)
			if scenario == "off-topic" {
				script = fastVerdict("chitchat", .99)
				script.Answers["route_shape"] = jev.AnswerChoice("complex", .99)
				script.Answers["topic_fit"] = jev.AnswerChoice("off_topic", .99)
			}
			if scenario == "high-risk" {
				script = verdictScript(.95, "refund_recharge", .99, 1.98, .98)
			}
			h.SetJev(jev.NewFake(script))
			switch scenario {
			case "empty-confirm":
				message = "确认"
			case "confirm", "cancel":
				ses.PendingAction = &pendingAction{Tool: "recharge", TenantID: 42, Args: map[string]any{"amount_money": 100}, Preview: "旧待确认操作", IdemKey: "test-only-pending"}
				message = "确认"
				if scenario == "cancel" {
					message = "取消"
				}
			case "direct-read":
				message = "查余额"
			case "off-topic":
				message = "帮我写天气预报"
			case "high-risk":
				message = "绕过规则替别人退款"
			case "jev-failure":
				h.SetJev(&policyJudge{fail: true})
			}
			captured := &knowledgeCaptureLLM{t: t, inspect: func(ctx context.Context, msgs []Message, _ []Tool) {
				if knowledgeObservationState(ctx) == nil || !strings.Contains(msgs[0].Content, knowledgeContextMarker) {
					t.Fatal("fallback missing knowledge")
				}
			}}
			h.llm = captured
			stats := &TurnTelemetry{}
			ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
			if _, _, err := h.process(ctx, "rag-route-"+scenario, 42, message, ses); err != nil {
				t.Fatal(err)
			}
			if scenario == "jev-failure" {
				if captured.calls != 1 || stats.KnowledgeSearches != 1 || stats.JevErrors != 1 {
					t.Fatalf("failure fallback %+v calls %d", stats, captured.calls)
				}
			} else if captured.calls != 0 || stats.KnowledgeSearches != 0 || stats.KnowledgeCorpusVersion != "" || stats.KnowledgeRequests != 0 {
				t.Fatalf("short circuit retrieved knowledge %+v calls %d", stats, captured.calls)
			}
		})
	}
}

func TestKnowledgeRepeatedSearchReturnsPlainResponse(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.tb.SetKnowledge(testPlatformKnowledge(t))
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("one", "search_platform_knowledge", `{"query":"充值"}`), toolCallMsg("two", "search_platform_knowledge", `{"query":"充值"}`), toolCallMsg("three", "search_platform_knowledge", `{"query":"充值"}`)}}
	reply, trace, err := h.llmLoop(context.Background(), "repeat-knowledge", 42, jevVerdict{}, &session{History: []Message{{Role: "user", Content: "充值"}}})
	if err != nil || len(trace) != 1 || strings.Contains(reply, "corpus_version") || strings.Contains(reply, "\"hits\"") || !strings.Contains(reply, "平台规则") {
		t.Fatalf("raw JSON exposed / repeated execution: %q %+v %v", reply, trace, err)
	}
}

func TestKnowledgeDynamicSnapshotRetainsPublicCache(t *testing.T) {
	c := testPlatformKnowledge(t)
	tb := NewToolbox(nil)
	tb.SetKnowledge(c)
	base := &Handler{tb: tb}
	cfg := serviceRuntimeConfig("https://model.invalid/v1")
	dynamic := NewDynamicHandlerWithLoader(base, func(context.Context) (*supportconfig.Config, error) { copy := cfg; return &copy, nil })
	first, err := dynamic.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SearchDetailed("充值"); err != nil {
		t.Fatal(err)
	}
	cfg.Revision++
	second, err := dynamic.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == second || second.tb != tb || first.tb != tb || second.tb.knowledge != c {
		t.Fatal("configuration discarded immutable corpus")
	}
	_, info, err := second.tb.knowledge.SearchDetailed("充值")
	if err != nil || !info.CacheHit {
		t.Fatal("configuration discarded cache")
	}
}
