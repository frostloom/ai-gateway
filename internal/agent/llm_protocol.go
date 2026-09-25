// llm_protocol.go LLM 上游协议适配。
//
// 背景：客服最早只对接 DeepSeek 的 OpenAI 兼容端点（POST {base}/chat/completions）。
// 后来上游换成 MaaS 的 Anthropic 端点（POST {base}/v1/messages），两者请求体、
// 鉴权头、响应结构都不同：
//
//	OpenAI     : Authorization: Bearer <k>   | messages[].role=tool + tools[].function{}
//	Anthropic  : x-api-key: <k>              | 工具结果放在 user 消息的 tool_result 块里
//	             anthropic-version: 2023-06-01 | 工具定义是扁平的 name/input_schema
//
// 这里只做「协议翻译」：对上层仍然进出一致的 Message/Tool（内部表示沿用 OpenAI 形状，
// 因为 chat.go 的状态机与测试都依赖它），差异全部收敛在本文件。
// 选择哪种协议由 NewLLMClient 依据 base URL 推断，也可用 AGENT_LLM_PROTOCOL 显式覆盖。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// llmProtocol 上游协议族。
type llmProtocol string

const (
	protoOpenAI    llmProtocol = "openai"    // POST {base}/chat/completions
	protoAnthropic llmProtocol = "anthropic" // POST {base}/v1/messages
)

// detectProtocol 依据 base URL 推断协议；显式配置优先。
//
//	"anthropic" → Anthropic
//	"openai"    → OpenAI
//	空          → 看 URL 里有没有 /anthropic 或 /messages 之类特征
func detectProtocol(configured, baseURL string) llmProtocol {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "anthropic", "claude", "messages":
		return protoAnthropic
	case "openai", "chat", "chat_completions":
		return protoOpenAI
	}
	u := strings.ToLower(baseURL)
	if strings.Contains(u, "/anthropic") || strings.Contains(u, "/messages") ||
		strings.Contains(u, "claude") {
		return protoAnthropic
	}
	return protoOpenAI
}

// ---------- Anthropic 请求/响应形状 ----------

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"` // user / assistant
	Content []any  `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicResponse 只取我们关心的字段。
type anthropicResponse struct {
	ID         string `json:"id"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"` // text / tool_use / thinking
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// anthropicDefaultMaxTokens 上游要求 max_tokens 必填。
//
// 这个值必须给足：deepseek-v4.1-flash 是带 thinking 的模型，推理内容同样计入
// 输出预算。实测「改套餐」这类请求最费预算 —— 模型要先查当前订阅、再列可选档位、
// 比对差价、判断生效时间，推理链很长：
//
//	max_tokens=8192   → 撞顶（content 里只有 thinking 块）
//	max_tokens=16384  → 仍会撞顶（约 1/5 的改套餐请求）
//	max_tokens=32768  → 稳定通过
//
// 所以默认给 32768。预算撞顶时还有一次加倍重试兜底（见 Chat）。
// 可用 AGENT_LLM_MAX_TOKENS 覆盖。
const anthropicDefaultMaxTokens = 32768

// anthropicMaxTokensCeiling 自动重试时的预算上限，防无限放大。
const anthropicMaxTokensCeiling = 131072

// DebugDumpRequest 为 true 时把即将发出的请求体交给 dumpFn（排查"卡住"用）。
// 通过 AGENT_LLM_DEBUG=1 打开；只在需要时开，避免把对话内容刷进日志。
var (
	DebugDumpRequest = false
	dumpFn           func(size int, body string)
)

// SetDebugDump 注入 dump 回调（由 main 接日志）。
func SetDebugDump(f func(size int, body string)) { dumpFn = f }

// chatAnthropic 走 Anthropic Messages 协议。
func (c *LLMClient) chatAnthropic(ctx context.Context, messages []Message, tools []Tool, maxTokens int) (*Message, error) {
	req, err := buildAnthropicRequest(c.model, messages, tools, maxTokens)
	if err != nil {
		return nil, err
	}
	if DebugDumpRequest && dumpFn != nil {
		if b, err := json.Marshal(req); err == nil {
			dumpFn(len(b), string(b))
		}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.cli.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LLM 请求失败: %v", err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var e anthropicResponse
		_ = json.Unmarshal(body, &e)
		msg := string(body)
		if e.Error != nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return nil, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, msg)
	}

	var out anthropicResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("LLM 响应解析失败: %v", err)
	}
	return anthropicToMessage(&out)
}

// buildAnthropicRequest 把内部（OpenAI 形状）消息翻译成 Anthropic 请求。
//
// 两个必须处理的差异：
//  1. system 不是一条 message，而是顶层字段（多条 system 用空行拼接）。
//  2. OpenAI 把工具结果作为独立 role=tool 消息；Anthropic 要求 tool_result 块
//     放在 user 消息的 content 数组里，且必须紧跟在带 tool_use 的 assistant 消息之后。
func buildAnthropicRequest(model string, messages []Message, tools []Tool, maxTokens int) (*anthropicRequest, error) {
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	out := &anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
	}

	var sysParts []string
	for _, m := range messages {
		if m.Role == "system" {
			if s := strings.TrimSpace(m.Content); s != "" {
				sysParts = append(sysParts, s)
			}
		}
	}
	out.System = strings.Join(sysParts, "\n\n")

	for _, m := range messages {
		switch m.Role {
		case "system":
			continue // 已并入顶层 system

		case "user":
			out.Messages = append(out.Messages, anthropicMessage{
				Role:    "user",
				Content: []any{map[string]any{"type": "text", "text": m.Content}},
			})

		case "assistant":
			var blocks []any
			if s := strings.TrimSpace(m.Content); s != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := json.RawMessage(tc.Function.Arguments)
				if len(args) == 0 || !json.Valid(args) {
					args = json.RawMessage(`{}`)
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": args,
				})
			}
			if len(blocks) == 0 {
				continue // 空 assistant 消息会被上游拒
			}
			out.Messages = append(out.Messages, anthropicMessage{Role: "assistant", Content: blocks})

		case "tool":
			// 工具结果：并入 user 消息的 tool_result 块。
			// 连续多条 tool 结果要合并到同一条 user 消息里（Anthropic 不允许 user 连续）。
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}
			if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == "user" && isToolResultOnly(out.Messages[n-1]) {
				out.Messages[n-1].Content = append(out.Messages[n-1].Content, block)
			} else {
				out.Messages = append(out.Messages, anthropicMessage{
					Role:    "user",
					Content: []any{block},
				})
			}
		}
	}

	for _, t := range tools {
		out.Tools = append(out.Tools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out, nil
}

// isToolResultOnly 判断该 user 消息是否只装工具结果（用于合并连续 tool_result）。
func isToolResultOnly(m anthropicMessage) bool {
	if len(m.Content) == 0 {
		return false
	}
	for _, b := range m.Content {
		bm, ok := b.(map[string]any)
		if !ok || bm["type"] != "tool_result" {
			return false
		}
	}
	return true
}

// anthropicToMessage 把 Anthropic 响应翻译回内部 Message（OpenAI 形状）。
//
// thinking 块要丢弃：它是上游的推理内容，既不该回填给下一轮，
// 也不能混进给用户看的正文（否则用户会看到一大段英文思考过程）。
func anthropicToMessage(r *anthropicResponse) (*Message, error) {
	if r.Error != nil && r.Error.Message != "" {
		return nil, fmt.Errorf("LLM 返回错误: %s", r.Error.Message)
	}
	msg := &Message{Role: "assistant"}
	var texts []string
	var thoughtOnly bool
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			if s := strings.TrimSpace(b.Text); s != "" {
				texts = append(texts, s)
			}
		case "tool_use":
			var tc ToolCall
			tc.ID = b.ID
			tc.Type = "function"
			tc.Function.Name = b.Name
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			tc.Function.Arguments = args
			msg.ToolCalls = append(msg.ToolCalls, tc)
		case "thinking":
			// 丢弃正文，但记下"这一轮只产出了思考"
			thoughtOnly = true
		}
	}
	msg.Content = strings.Join(texts, "\n")
	if len(msg.ToolCalls) == 0 && msg.Content == "" {
		// 区分两种空：
		//   - 只思考没输出 → 通常是 max_tokens 被 thinking 吃光，属于预算问题
		//   - 什么都没有   → 上游异常
		// 错误信息里点明，便于排查（真实案例：4096 预算下跨租户对话必现）。
		if thoughtOnly && r.StopReason == "max_tokens" {
			return nil, fmt.Errorf("LLM 思考超出 max_tokens（stop_reason=max_tokens，仅有 thinking 块）：请调大 AGENT_LLM_MAX_TOKENS")
		}
		if thoughtOnly {
			return nil, fmt.Errorf("LLM 只返回了思考内容（stop_reason=%s）", r.StopReason)
		}
		return nil, fmt.Errorf("LLM 返回空内容（stop_reason=%s）", r.StopReason)
	}
	return msg, nil
}

// llmHTTPTimeout 单次 LLM 调用超时。
//
// 这个值要同时容纳两件事：
//  1. 带 thinking 的模型在复杂场景（改套餐要查现有档位+比对+算生效日）推理很长；
//  2. 并发压测时上游会排队，单次排队 + 推理可能到 2 分钟以上。
// 60s / 120s 实测都会在并发下被掐断（表现为 context deadline exceeded），
// 用户看到的是"服务错误"，其实只是慢。给到 300s 与网关/评测侧对齐。
// 注意：这是**自保**超时，不是性能目标；正常单次调用在 5~15s。
const llmHTTPTimeout = 300 * time.Second
