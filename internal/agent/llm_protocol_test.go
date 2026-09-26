package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 协议推断：显式配置优先，其次看 URL 特征。
func TestDetectProtocol(t *testing.T) {
	cases := []struct {
		configured, base string
		want             llmProtocol
	}{
		{"", "https://api.deepseek.com/v1", protoOpenAI},
		{"", "https://token-plan.maas.qianwenaiapi.com/apps/anthropic", protoAnthropic},
		{"", "https://x.example.com/v1/messages", protoAnthropic},
		{"", "https://gw.example.com/claude-proxy", protoAnthropic},
		{"anthropic", "https://api.deepseek.com/v1", protoAnthropic}, // 显式覆盖
		{"openai", "https://x.example.com/anthropic", protoOpenAI},   // 显式覆盖
		{" Claude ", "https://x.example.com", protoAnthropic},        // 大小写/空格容错
	}
	for _, c := range cases {
		if got := detectProtocol(c.configured, c.base); got != c.want {
			t.Errorf("detectProtocol(%q,%q) = %q, want %q", c.configured, c.base, got, c.want)
		}
	}
}

// Anthropic 请求翻译：system 提到顶层、工具结果并入 user 的 tool_result 块。
func TestBuildAnthropicRequest(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "你是客服"},
		{Role: "system", Content: "不要越权"},
		{Role: "user", Content: "帮我查余额"},
		{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "call_1", Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "get_balance", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: `{"balance_cent":5022803}`},
	}
	tools := []Tool{{
		Type: "function",
		Function: struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		}{Name: "get_balance", Description: "查余额", Parameters: map[string]any{"type": "object"}},
	}}

	req, err := buildAnthropicRequest("deepseek-v4.1-flash", messages, tools, 0)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// system 合并到顶层，且不再出现在 messages 里
	if req.System != "你是客服\n\n不要越权" {
		t.Errorf("system = %q", req.System)
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			t.Fatal("system 不应作为 message 出现")
		}
	}

	// max_tokens 必填
	if req.MaxTokens <= 0 {
		t.Error("max_tokens 必须为正数（Anthropic 必填字段）")
	}

	// 工具定义是扁平结构
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_balance" || req.Tools[0].InputSchema == nil {
		t.Fatalf("tools = %+v", req.Tools)
	}

	// 消息序列：user → assistant(tool_use) → user(tool_result)
	if len(req.Messages) != 3 {
		t.Fatalf("messages 数 = %d, want 3: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[1].Role != "assistant" {
		t.Fatalf("第 2 条应为 assistant, got %s", req.Messages[1].Role)
	}
	blk, _ := req.Messages[1].Content[0].(map[string]any)
	if blk["type"] != "tool_use" || blk["name"] != "get_balance" {
		t.Fatalf("assistant 块 = %+v", blk)
	}
	last := req.Messages[2]
	if last.Role != "user" {
		t.Fatalf("工具结果应以 user 角色发送, got %s", last.Role)
	}
	tr, _ := last.Content[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["tool_use_id"] != "call_1" {
		t.Fatalf("tool_result 块 = %+v", tr)
	}

	// 必须能序列化成合法 JSON
	if _, err := json.Marshal(req); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

// 连续多条工具结果必须合并到同一条 user 消息（Anthropic 不允许 user 连续）。
func TestBuildAnthropicMergesToolResults(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "查余额和消费"},
		{Role: "assistant", ToolCalls: []ToolCall{
			newToolCall("c1", "get_balance", `{}`),
			newToolCall("c2", "get_consumption", `{"days":7}`),
		}},
		{Role: "tool", ToolCallID: "c1", Content: "余额 100"},
		{Role: "tool", ToolCallID: "c2", Content: "消费 20"},
	}
	req, err := buildAnthropicRequest("m", messages, nil, 0)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages 数 = %d, want 3（两条工具结果应合并）: %+v", len(req.Messages), req.Messages)
	}
	if n := len(req.Messages[2].Content); n != 2 {
		t.Fatalf("末条 user 应含 2 个 tool_result, got %d", n)
	}
}

// 非法/空 arguments 要兜成 {}，否则上游 400。
func TestBuildAnthropicBadArguments(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("c1", "t", ``)}},
	}
	req, err := buildAnthropicRequest("m", messages, nil, 0)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	blk, _ := req.Messages[1].Content[0].(map[string]any)
	if string(blk["input"].(json.RawMessage)) != "{}" {
		t.Fatalf("空 arguments 应兜成 {}, got %s", blk["input"])
	}
}

// 响应翻译：thinking 丢弃、text 拼接、tool_use 还原成内部 ToolCall。
func TestAnthropicToMessage(t *testing.T) {
	raw := `{
	  "id":"msg_1","stop_reason":"tool_use",
	  "content":[
	    {"type":"thinking","thinking":"用户在问余额","signature":""},
	    {"type":"text","text":" 我来查一下 "},
	    {"type":"tool_use","id":"toolu_1","name":"get_balance","input":{}}
	  ]}`
	var resp anthropicResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msg, err := anthropicToMessage(&resp)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	// thinking 不能泄漏到正文
	if strings.Contains(msg.Content, "用户在问余额") {
		t.Fatalf("thinking 内容泄漏进正文: %q", msg.Content)
	}
	if msg.Content != "我来查一下" {
		t.Errorf("content = %q, want 已 trim 的正文", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Function.Name != "get_balance" || tc.Function.Arguments != "{}" {
		t.Errorf("tool_call = %+v", tc)
	}
	// Type 必须回填 "function"：回传给上一轮时缺 type 会被拒
	if tc.Type != "function" {
		t.Errorf("tool_call.Type = %q, want function", tc.Type)
	}
}

// 纯 thinking、无正文无工具 → 报错而不是返回空消息。
func TestAnthropicToMessageEmpty(t *testing.T) {
	var resp anthropicResponse
	_ = json.Unmarshal([]byte(`{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"x"}]}`), &resp)
	_, err := anthropicToMessage(&resp)
	if err == nil {
		t.Fatal("无正文无工具时应报错")
	}
	// 真实案例（线上抓到）：预算给 4096 时，「跨租户充值」「拒绝注入」这类
	// 需要斟酌的对话，推理会把预算吃光 → content 里只有 thinking 块。
	// 报错必须点明是预算问题，否则会被误判成上游故障、白查半天。
	if !strings.Contains(err.Error(), "max_tokens") || !strings.Contains(err.Error(), "AGENT_LLM_MAX_TOKENS") {
		t.Fatalf("err = %v, want 点明 max_tokens 预算不足", err)
	}
}

// 预算口径：**不是越大越好**。
//
// 实测带 thinking 的模型预算越大、思考越久，反而越容易撞 HTTP 超时
//（「改套餐」在 32768 下会思考到超时）。默认值应当是"够用且促使收敛"，
// 靠撞顶后加倍重试兜底。
func TestAnthropicMaxTokensBudget(t *testing.T) {
	if anthropicDefaultMaxTokens > 16384 {
		t.Fatalf("anthropicDefaultMaxTokens = %d 偏大：预算越大思考越久，反而更易超时",
			anthropicDefaultMaxTokens)
	}
	if anthropicDefaultMaxTokens < 4096 {
		t.Fatalf("anthropicDefaultMaxTokens = %d 偏小：正常回复会被截断", anthropicDefaultMaxTokens)
	}
	if anthropicMaxTokensCeiling < anthropicDefaultMaxTokens*2 {
		t.Fatalf("ceiling(%d) 应至少是默认预算(%d)的 2 倍，留出加倍重试空间",
			anthropicMaxTokensCeiling, anthropicDefaultMaxTokens)
	}
	// maxTokens<=0 应回落到默认值，而不是发 0（上游会拒）
	req, err := buildAnthropicRequest("m", []Message{{Role: "user", Content: "hi"}}, nil, 0)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if req.MaxTokens != anthropicDefaultMaxTokens {
		t.Fatalf("MaxTokens = %d, want %d", req.MaxTokens, anthropicDefaultMaxTokens)
	}
	// 显式值应被尊重
	req2, err := buildAnthropicRequest("m", []Message{{Role: "user", Content: "hi"}}, nil, 65536)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if req2.MaxTokens != 65536 {
		t.Fatalf("MaxTokens = %d, want 65536（显式值应生效）", req2.MaxTokens)
	}
}

// isBudgetExhausted 只认「思考撞顶」，不把其他错误误判成预算问题。
func TestIsBudgetExhausted(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("LLM 思考超出 max_tokens（stop_reason=max_tokens，仅有 thinking 块）：请调大 AGENT_LLM_MAX_TOKENS"), true},
		{fmt.Errorf("LLM HTTP 401: invalid api key"), false},
		{fmt.Errorf("LLM 返回空内容（stop_reason=end_turn）"), false},
	}
	for _, c := range cases {
		if got := isBudgetExhausted(c.err); got != c.want {
			t.Errorf("isBudgetExhausted(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// 预算撞顶时自动加倍重试，直到成功（最多 3 次尝试）。
func TestChatRetriesOnBudgetExhausted(t *testing.T) {
	defer SetProtocolFromEnv(func() string { return "" }) // 还原，避免污染其他测试
	SetProtocolFromEnv(func() string { return "anthropic" })

	var calls int
	var seenTokens []int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls++
		seenTokens = append(seenTokens, req.MaxTokens)
		if calls < 3 {
			// 前两次：只有 thinking，撞顶
			_, _ = w.Write([]byte(`{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"想很久"}]}`))
			return
		}
		// 第三次（预算再次加倍后）：正常给出工具调用
		_, _ = w.Write([]byte(`{"stop_reason":"tool_use","content":[
			{"type":"thinking","thinking":"ok"},
			{"type":"tool_use","id":"t1","name":"get_balance","input":{}}]}`))
	}))
	defer ts.Close()

	c := NewLLMClient(ts.URL, "k", "m")
	if c.Protocol() != "anthropic" {
		t.Fatalf("protocol = %s, want anthropic", c.Protocol())
	}
	msg, err := c.Chat(context.Background(),
		[]Message{{Role: "user", Content: "我想升个级"}}, nil)
	if err != nil {
		t.Fatalf("Chat 应在加倍预算后成功: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3（两次撞顶 + 第三次成功）", calls)
	}
	// 预算应递增
	for i := 1; i < len(seenTokens); i++ {
		if seenTokens[i] <= seenTokens[i-1] {
			t.Fatalf("预算应逐次加倍: %v", seenTokens)
		}
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "get_balance" {
		t.Fatalf("msg = %+v, want get_balance 工具调用", msg)
	}
}

// 重试仍撞顶 → 返回原始预算错误（便于定位是预算问题）。
func TestChatRetryStillExhausted(t *testing.T) {
	defer SetProtocolFromEnv(func() string { return "" })
	SetProtocolFromEnv(func() string { return "anthropic" })

	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"x"}]}`))
	}))
	defer ts.Close()
	c := NewLLMClient(ts.URL, "k", "m")
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("反复撞顶应报错")
	}
	if !strings.Contains(err.Error(), "AGENT_LLM_MAX_TOKENS") {
		t.Fatalf("err = %v, want 点明预算配置项", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3（最多尝试 3 次，不无限放大）", calls)
	}
}

// 上游错误结构要透出可读信息。
func TestAnthropicToMessageError(t *testing.T) {
	var resp anthropicResponse
	_ = json.Unmarshal([]byte(`{"error":{"type":"invalid_request_error","message":"model not found"}}`), &resp)
	_, err := anthropicToMessage(&resp)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v, want 含上游消息", err)
	}
}

// newToolCall 构造内部 ToolCall（匿名结构体字段，测试里复用）。
func newToolCall(id, name, args string) ToolCall {
	var tc ToolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}
