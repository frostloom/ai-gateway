// mock-provider 假 LLM：返回可控 token 数的 OpenAI 兼容响应。
// M2 只做非流式；M3 加 SSE 流式 + usage 帧。
// 故障注入（失败率/超时）在 M5 加入，用于熔断/容灾验证。
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/tokens"
)

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index        int     `json:"index"`
	Message      msgBody `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type msgBody struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func main() {
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	port := config.Getenv("MOCK_PROVIDER_PORT", "9201")
	name := config.Getenv("MOCK_PROVIDER_NAME", "mock-provider")
	// 默认 5000 completion token：分/百万 token 单价下，单次调用才有非零扣费可见
	// （deepseek-v4-flash ≈1 分、step-2 ≈38 分，正好演示「同用量贵 38 倍」）。
	completionTokens := config.GetenvInt("MOCK_COMPLETION_TOKENS", 5000)
	// M5 故障注入：failRate 0-100（百分比，命中返回 500），latencyMs 模拟慢 provider。
	failRate := config.GetenvInt("MOCK_FAIL_RATE", 0)
	latencyMs := config.GetenvInt("MOCK_LATENCY_MS", 0)
	if failRate < 0 || failRate > 100 {
		log.Error("MOCK_FAIL_RATE must be 0-100", "got", failRate)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "svc": name})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if failRate > 0 && rand.Intn(100) < failRate {
			// 故障注入：provider 侧 500（首个字节前失败），触发网关 failover + 熔断
			log.Warn("injected failure", "provider", name, "fail_rate", failRate)
			http.Error(w, `{"error":{"message":"mock injected 500"}}`, http.StatusInternalServerError)
			return
		}
		if latencyMs > 0 {
			time.Sleep(time.Duration(latencyMs) * time.Millisecond)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		promptTokens := int(tokens.Estimate(messagesText(req.Messages)))
		ct := completionTokens
		if req.MaxTokens > 0 {
			ct = req.MaxTokens
		}

		if req.Stream {
			streamCompletion(w, req, name, promptTokens, ct, log)
			return
		}

		resp := chatResponse{
			ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []choice{{
				Index:        0,
				Message:      msgBody{Role: "assistant", Content: generateReply(ct)},
				FinishReason: "stop",
			}},
			Usage: usage{
				PromptTokens:     promptTokens,
				CompletionTokens: ct,
				TotalTokens:      promptTokens + ct,
			},
		}
		log.Info("completion", "provider", name, "prompt_tokens", promptTokens, "completion_tokens", ct)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	addr := ":" + port
	log.Info("mock provider listening", "name", name, "addr", addr, "completion_tokens", completionTokens)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

// streamCompletion OpenAI 兼容 SSE 流式响应：
// role 帧 → 内容增量帧（分 8 块，间隔 50ms，让流可观察）→ 带 usage 的终帧 → [DONE]。
// 网关靠「最后一帧的 usage」按实际计费，所以 usage 帧必须存在且字段与 JSON 模式一致。
func streamCompletion(w http.ResponseWriter, req chatRequest, name string, promptTokens, ct int, log *slog.Logger) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	now := time.Now().Unix()
	chunkBase := map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": now, "model": req.Model,
	}
	send := func(payload map[string]any) {
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	send(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"role": "assistant"}}}})

	content := generateReply(ct)
	runes := []rune(content)
	chunks := 8
	per := (len(runes) + chunks - 1) / chunks
	for i := 0; i < chunks; i++ {
		lo := i * per
		if lo >= len(runes) {
			break
		}
		hi := lo + per
		if hi > len(runes) {
			hi = len(runes)
		}
		chunk := runes[lo:hi]
		send(mergeMaps(chunkBase, map[string]any{
			"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": string(chunk)}}},
		}))
		time.Sleep(50 * time.Millisecond) // 让增量可见；kill 客户端测试依赖这个节奏
	}

	// usage 终帧：choices 为空 + usage（OpenAI 兼容）。
	send(mergeMaps(chunkBase, map[string]any{
		"choices": []any{},
		"usage": map[string]int{
			"prompt_tokens": promptTokens, "completion_tokens": ct, "total_tokens": promptTokens + ct,
		},
	}))
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	log.Info("stream done", "provider", name, "prompt_tokens", promptTokens, "completion_tokens", ct)
}

func mergeMaps(a, b map[string]any) map[string]any {
	m := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		m[k] = v
	}
	for k, v := range b {
		m[k] = v
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func messagesText(msgs []chatMessage) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Content)
	}
	return sb.String()
}

// generateReply 生成约 n 个 token（≈3n 个字符）的回复，让 usage 计数看得见摸得着。
func generateReply(n int) string {
	base := "这是一段由 mock provider 生成的回复文本。用于验证网关流式转发与计费链路。"
	repeats := (n*3 + len(base) - 1) / len(base)
	return strings.Repeat(base, repeats)
}
