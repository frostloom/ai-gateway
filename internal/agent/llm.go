// Package agent AI 客服运行时（网关的第 6 个服务，也是网关的一个用户）。
// 执行层不信任 LLM：tenant 只来自验证过的 API key，工具参数一律服务端重新校验，
// 写操作必须用户确认，每笔执行写审计。LLM 只负责「意图 → 工具/参数」，不负责安全。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------- LLM 消息模型（OpenAI 兼容，DeepSeek function calling 同构） ----------

type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type,omitempty"` // 必须保留 "function"：回填 assistant tool_calls 时 DeepSeek 校验缺 type 会 400
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens,omitempty"`
	Messages  []Message          `json:"messages"`
	Tools     []Tool             `json:"tools,omitempty"`
	Thinking  *anthropicThinking `json:"thinking,omitempty"`
}

type chatResponse struct {
	Usage *struct {
		PromptTokens          *int64 `json:"prompt_tokens"`
		CompletionTokens      *int64 `json:"completion_tokens"`
		PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
		PromptTokensDetails   *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// LLMClient 真实 LLM 客户端（无 mock 模式：key 缺失即报错）。
//
// 支持两种上游协议，由 NewLLMClient 依据 base URL 推断（可用 AGENT_LLM_PROTOCOL 覆盖）：
//   - OpenAI 兼容   POST {base}/chat/completions   （DeepSeek 官方端点）
//   - Anthropic     POST {base}/v1/messages        （MaaS / Claude 端点）
//
// 协议差异全部收敛在 llm_protocol.go，本文件只负责分派与超时。
type LLMClient struct {
	baseURL          string
	apiKey           string
	model            string
	proto            llmProtocol
	maxTokens        int
	cli              *http.Client
	strictEvaluation bool
}

// protoFromEnv 读取 AGENT_LLM_PROTOCOL（空则由 URL 推断）。
// 用回调注入避免 internal/agent 反向依赖 config 包。
var protoFromEnv = func() string { return "" }

// maxTokensFromEnv 读取 AGENT_LLM_MAX_TOKENS（0 或缺省 = anthropicDefaultMaxTokens）。
var maxTokensFromEnv = func() int { return 0 }

// SetProtocolFromEnv 由 main 注入协议与输出预算读取函数
// （测试不调用则保持 URL 推断 + 默认预算）。
func SetProtocolFromEnv(f func() string) {
	if f != nil {
		protoFromEnv = f
	}
}

// SetMaxTokensFromEnv 由 main 注入输出预算读取函数。
func SetMaxTokensFromEnv(f func() int) {
	if f != nil {
		maxTokensFromEnv = f
	}
}

// llmTransport LLM 专用连接池。
//
// 关键设计（都是实测踩出来的）：
//
//  1. 不能用 &http.Client{} 的默认 Transport —— 其 MaxIdleConnsPerHost 只有 2，
//     而客服是「多轮 tool loop + 评测并发」，同一上游会被并发打多次。
//     空闲连接不够时新请求要等旧连接释放，表现为耗时**精确撞上** client timeout。
//
//  2. **不启用 HTTP/2**。上游是网关型服务，HTTP/2 下长响应会出现
//     "http2: timeout awaiting response headers" —— 同样的请求走 HTTP/1.1 正常。
//     带 thinking 的模型响应头本身就慢，叠加 HTTP/2 的流控/头等待后更易触发。
//
//  3. **不设 ResponseHeaderTimeout**：它与 client.Timeout 语义重叠，
//     且会和 HTTP/2 的超时逻辑打架。统一的失败边界交给 client.Timeout。
var llmTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:          200,
	MaxIdleConnsPerHost:   64, // 默认仅 2，是多轮并发卡死的根因
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

func NewLLMClient(baseURL, apiKey, model string) *LLMClient {
	mt := maxTokensFromEnv()
	if mt <= 0 {
		mt = anthropicDefaultMaxTokens
	}
	return &LLMClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		proto:     detectProtocol(protoFromEnv(), baseURL),
		maxTokens: mt,
		cli:       &http.Client{Timeout: llmHTTPTimeout, Transport: llmTransport},
	}
}

func (c *LLMClient) Model() string { return c.model }

// NewEvaluationLLMClient uses explicit inputs only, bypassing global readers.
func NewEvaluationLLMClient(baseURL, apiKey, model, protocol string, client *http.Client, maxOutput int) (*LLMClient, error) {
	if baseURL == "" || model == "" || (protocol != "openai" && protocol != "anthropic") {
		return nil, fmt.Errorf("explicit evaluation endpoint/model/protocol required")
	}
	c := &LLMClient{baseURL: baseURL, apiKey: apiKey, model: model, proto: llmProtocol(protocol)}
	if err := c.ConfigureEvaluationClient(client, maxOutput); err != nil {
		return nil, err
	}
	return c, nil
}

// ConfigureEvaluationClient is explicit per-client dependency injection for a
// budgeted evaluator. Configure before serving; ordinary service defaults stay
// unchanged. The supplied transport must guard every upstream attempt.
func (c *LLMClient) ConfigureEvaluationClient(client *http.Client, maxOutput int) error {
	if client == nil || client.Transport == nil || maxOutput < 1 || maxOutput > 512 {
		return fmt.Errorf("explicit bounded evaluation HTTP client required")
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout <= 0 || copyClient.Timeout > 90*time.Second {
		copyClient.Timeout = 90 * time.Second
	}
	c.cli = &copyClient
	c.maxTokens = maxOutput
	c.strictEvaluation = true
	return nil
}

// Protocol 当前生效的协议（启动日志用，便于排查路径配错）。
func (c *LLMClient) Protocol() string { return string(c.proto) }

// The official Flash model enables thinking by default. The bounded evaluator
// uses ordinary tool messages and must reserve its small output cap for answers.
func (c *LLMClient) evaluationDisablesThinking() bool {
	if !c.strictEvaluation {
		return false
	}
	if c.proto == protoAnthropic {
		return true
	}
	u, err := url.Parse(c.baseURL)
	return err == nil && c.proto == protoOpenAI && u.Hostname() == "api.deepseek.com" && c.model == "deepseek-flash"
}

// Chat 调用一次 LLM，返回助手消息（可能带 tool_calls）。
//
// 「预算 vs 长思考」的实测结论（决定了下面的策略）：
//
//	带 thinking 的模型，max_tokens 越大 → 允许思考越久 → 反而越容易撞 HTTP 超时。
//	实测「改套餐」类请求：
//	  max_tokens=8192  → 撞顶（只有 thinking 块，无输出）
//	  max_tokens=16384 → 仍撞顶
//	  max_tokens=32768 → 思考更久，直接顶到 HTTP 超时
//
// 所以策略不是"预算越大越好"，而是**先小后大**：
//  1. 先用小预算（快，逼模型尽快收敛到工具调用或回答）；
//  2. 仅当确实因预算不足失败时才加倍重试；
//  3. 每次尝试都受 HTTP 超时约束，避免单次无限拖。
//
// 常规请求一次即成（2~10s），少数复杂请求最多多花一轮。
func (c *LLMClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("AGENT_LLM_API_KEY 未配置，AI 客服不可用（请在 env 填入密钥）")
	}
	if c.proto != protoAnthropic {
		return c.chatOpenAI(ctx, messages, tools)
	}
	if c.strictEvaluation {
		return c.chatAnthropic(ctx, messages, tools, c.maxTokens)
	}

	budget := c.maxTokens
	if budget <= 0 {
		budget = anthropicFirstTryTokens
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		t0 := time.Now()
		// 每次调用都记录：能看出是「单次慢」还是「多次重试」，
		// 以及当时的预算和 payload 规模 —— 缺这些信息排查只能靠猜。
		if callLog != nil {
			callLog(attempt, budget, len(messages), len(tools))
		}
		msg, err := c.chatAnthropic(ctx, messages, tools, budget)
		if err == nil {
			return msg, nil
		}
		if !isBudgetExhausted(err) {
			return nil, err // 不是预算问题（含 HTTP 超时），直接失败
		}
		if budgetLog != nil {
			budgetLog(attempt, budget, time.Since(t0))
		}
		lastErr = err
		next := budget * 2
		if next > anthropicMaxTokensCeiling {
			break
		}
		budget = next
	}
	return nil, lastErr
}

// budgetLog 预算撞顶重试的回调（由 main 接日志；nil 时不记录）。
var budgetLog func(attempt, budget int, spent time.Duration)

// callLog 每次 LLM 调用的回调（由 main 接日志；nil 时不记录）。
var callLog func(attempt, budget, msgs, tools int)

// SetCallLogger 注入调用日志回调。
func SetCallLogger(f func(attempt, budget, msgs, tools int)) { callLog = f }

// SetBudgetLogger 注入重试日志回调。
func SetBudgetLogger(f func(attempt, budget int, spent time.Duration)) { budgetLog = f }

// isBudgetExhausted 判断错误是否为「思考吃光 max_tokens」。
func isBudgetExhausted(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "max_tokens") && strings.Contains(s, "thinking")
}

// chatOpenAI 走 OpenAI 兼容的 chat/completions。
func (c *LLMClient) chatOpenAI(ctx context.Context, messages []Message, tools []Tool) (result *Message, err error) {
	payload := chatRequest{Model: c.model, MaxTokens: c.maxTokens, Messages: messages, Tools: tools}
	if c.evaluationDisablesThinking() {
		payload.Thinking = &anthropicThinking{Type: "disabled"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	ctx, finishAttempt := beginLLMAttempt(ctx, protoOpenAI)
	defer func() { finishAttempt(err) }()
	observeKnowledgeRequest(ctx, messages, knowledgeRequestOrdinal(ctx))
	resp, err := c.cli.Do(req)
	observeKnowledgeResponse(ctx, messages, err)
	if err != nil {
		return nil, fmt.Errorf("LLM 请求失败: %v", err)
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out chatResponse
	parseErr := json.Unmarshal(raw, &out)
	recordProviderUsage(ctx, raw)
	if parseErr == nil && out.Usage != nil {
		u := out.Usage
		hit := u.PromptCacheHitTokens
		if hit == nil && u.PromptTokensDetails != nil {
			hit = u.PromptTokensDetails.CachedTokens
		}
		recordLLMUsage(ctx, u.PromptTokens, u.CompletionTokens, hit, u.PromptCacheMissTokens, nil)
	}
	if resp.StatusCode != http.StatusOK {
		var e chatResponse
		_ = json.Unmarshal(raw, &e)
		msg := string(raw)
		if e.Error != nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return nil, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, msg)
	}
	if parseErr != nil {
		return nil, fmt.Errorf("LLM 响应解析失败: %v", parseErr)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("LLM 返回空 choices")
	}
	return &out.Choices[0].Message, nil
}
