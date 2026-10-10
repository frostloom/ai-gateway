// Package handler gateway 的 OpenAI 兼容 HTTP 处理。
// chat.go：/v1/chat/completions 编排（非流式 + SSE 流式）——
//
//	路由 → 预占 → 转发上游 → ReportUsage+结算（独立后台 ctx） → 失败保持 pending（M4 对账冲正）
//
// 计费规则：读到 usage 帧按实际上报结算。已经转发过正文但没有 usage 时，
// 用已转发文本估算 completion 并结算，避免对账把已交付内容全额退掉。
// 一个正文都没转发则保持 pending。首字节前拨号失败或 5xx 会换 provider，
// 全部失败则立即 Reverse 并返回 502。
package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/tokens"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// billingAPI / routingAPI 是网关编排依赖的最小面，测试用假实现锁住计费分支。
type billingAPI interface {
	Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error)
	ReportUsage(ctx context.Context, req *billingv1.ReportUsageRequest) (*billingv1.ReportUsageResponse, error)
	Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error)
	Reverse(ctx context.Context, req *billingv1.ReverseRequest) (*billingv1.ReverseResponse, error)
}

type routingAPI interface {
	Route(ctx context.Context, model string, exclude ...uint64) (*client.RouteResult, error)
	Report(ctx context.Context, providerID uint64, ok bool)
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"` // 缺席不转发：真实上游用模型默认值（0 会被 OpenAI 兼容接口拒掉）
	Stream    bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// providerChatResponse 只关心 usage 字段；完整响应透传回客户端。
type providerChatResponse struct {
	Usage *usage `json:"usage"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// 上游转发复用同一个 http.Client（Transport 连接池复用）。
// 压测教训 1：每次请求 new Client 会新建 Transport/连接，高并发下疯狂建连把 Windows
// 临时端口占满 → dial mysql: Only one usage of each socket address。必须复用。
// 压测教训 2：池不能只够 64/host——300 并发时并发持有 >64 条上游连接，多出的在响应后
// 被关闭进 TIME_WAIT，堆积同样打爆临时端口。池子要能容纳峰值并发（压测 300/500 复用）。
var (
	upstreamClient = &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &http.Transport{MaxIdleConns: 2048, MaxIdleConnsPerHost: 1024},
	}
	streamClient = &http.Client{ // 流式无总超时（长连接），靠请求 ctx 取消
		Transport: &http.Transport{MaxIdleConns: 2048, MaxIdleConnsPerHost: 1024},
	}
)

// Chat 编排入口：鉴权/限流/幂等已在中间件完成，这里只负责「路由→预占→转发→收尾」。
func Chat(b billingAPI, r routingAPI, log *slog.Logger, upstreamTimeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req chatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			abort(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Model == "" || len(req.Messages) == 0 {
			abort(c, http.StatusBadRequest, "model and messages required")
			return
		}

		tenantID := c.GetInt64("tenant_id")
		apiKeyID := c.GetInt64("api_key_id")
		requestID := c.GetString("request_id")
		estPrompt := estimateMessages(&req)

		// 1) 路由：选 provider（无可用直接 503，没产生任何计费动作）
		prov, err := r.Route(c.Request.Context(), req.Model)
		if err != nil {
			abort(c, http.StatusServiceUnavailable, "routing unavailable")
			return
		}
		if prov == nil {
			abort(c, http.StatusServiceUnavailable, "no available provider for model")
			return
		}

		// 2) 预占：先扣后用，防超卖/防白嫖
		rc, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		rr, err := b.Reserve(rc, &billingv1.ReserveRequest{
			RequestId:           requestID,
			TenantId:            tenantID,
			ApiKeyId:            apiKeyID,
			ProviderId:          int64(prov.ProviderID),
			Model:               req.Model,
			EstPromptTokens:     estPrompt,
			MaxCompletionTokens: int64(req.MaxTokens),
		})
		cancel()
		if err != nil {
			// 目录里没有/下架的模型 → billing 返回 InvalidArgument，映射为 400「模型未上架/已下架」
			if status.Code(err) == codes.InvalidArgument {
				abort(c, http.StatusBadRequest, "model not listed or disabled: "+req.Model)
				return
			}
			abort(c, http.StatusServiceUnavailable, "billing unavailable")
			return
		}
		if rr.Code == billingv1.ReserveCode_RESERVE_INSUFFICIENT {
			abort(c, http.StatusPaymentRequired, "insufficient balance")
			return
		}
		if rr.Code == billingv1.ReserveCode_RESERVE_SUB_QUOTA {
			abort(c, http.StatusPaymentRequired, "订阅额度不足，每 N 小时滚动刷新")
			return
		}
		billID := rr.BillId
		log.Info("reserved", "request_id", requestID, "tenant_id", tenantID, "model", req.Model,
			"provider", prov.Name, "stream", req.Stream, "est_prompt", estPrompt, "balance_after", rr.BalanceAfter)

		if req.Stream {
			relayStream(c, b, r, log, prov, &req, requestID, billID, estPrompt)
			return
		}
		forwardNonStream(c, b, r, log, prov, &req, requestID, billID, estPrompt, upstreamTimeout)
	}
}

// ---------- 非流式 ----------

// forwardNonStream 转发上游，带 provider failover（M5）。
// 只在「首个字节前失败」重试：建连失败 / 5xx。此时客户端还没收到任何响应，
// 重试另一个 provider 不会双扣（Reserve 是 per-request 幂等的）。4xx 是业务拒绝
// （provider 正常，只是拒绝请求），不重试，直接透传退款。流中途失败绝不重试（M3 规则）。
func forwardNonStream(c *gin.Context, b billingAPI, r routingAPI, log *slog.Logger, prov *client.RouteResult, req *chatRequest, requestID string, billID int64, estPrompt int64, _ time.Duration) {
	// tried：本请求已尝试失败的 provider。failover 必须换一个节点——排除后重路由，
	// 避免加权随机再次选中同一个坏 provider（第一次试就失败，说明它此刻就是坏的）。
	tried := map[uint64]bool{}
	failover := func(providerID uint64) bool {
		tried[providerID] = true
		r.Report(c.Request.Context(), providerID, false)
		if next, rerr := r.Route(c.Request.Context(), req.Model, triedSlice(tried)...); rerr == nil && next != nil {
			prov = next
			return true
		}
		return false
	}

	for attempt := 0; attempt < 3; attempt++ {
		upstreamBody, _ := json.Marshal(req)
		uReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
			prov.BaseURL+"/v1/chat/completions", bytes.NewReader(upstreamBody))
		if err != nil {
			reverseWithBackground(b, log, requestID, billID, "build upstream request failed")
			abort(c, http.StatusBadGateway, "upstream error")
			return
		}
		setUpstreamHeaders(uReq, c, false, prov.APIKey)

		upResp, err := upstreamClient.Do(uReq)
		if err != nil {
			// 建连失败：provider 故障 → 上报 + 换下一个
			log.Warn("upstream dial failed, failover", "provider", prov.Name, "attempt", attempt+1, "err", err)
			if failover(prov.ProviderID) {
				continue
			}
			break
		}
		body, _ := io.ReadAll(upResp.Body)
		upResp.Body.Close()
		if upResp.StatusCode/100 != 2 {
			if upResp.StatusCode >= 500 {
				// provider 故障（5xx）→ 上报 + failover
				log.Warn("upstream 5xx, failover", "provider", prov.Name, "status", upResp.StatusCode, "attempt", attempt+1)
				if failover(prov.ProviderID) {
					continue
				}
				break
			}
			// 4xx：业务拒绝，provider 正常 → 不重试，直接退款透传
			r.Report(c.Request.Context(), prov.ProviderID, true)
			reverseWithBackground(b, log, requestID, billID, "upstream 4xx: "+upResp.Status)
			c.Data(upResp.StatusCode, "application/json", body)
			return
		}

		// 成功：上报健康 + 按实际结算
		r.Report(c.Request.Context(), prov.ProviderID, true)
		prompt, completion := estPrompt, int64(0)
		var pr providerChatResponse
		_ = json.Unmarshal(body, &pr)
		if pr.Usage != nil {
			prompt = int64(pr.Usage.PromptTokens)
			completion = int64(pr.Usage.CompletionTokens)
		}
		settleAndLog(c, b, log, requestID, billID, prompt, completion, int64(prov.ProviderID))
		c.Data(http.StatusOK, "application/json", body)
		return
	}

	// 所有 provider 都失败 → 退款（非流式边界清晰，直接退，不需要对账）
	log.Error("all providers failed", "request_id", requestID, "model", req.Model)
	reverseWithBackground(b, log, requestID, billID, "all providers failed")
	abort(c, http.StatusBadGateway, "all providers unavailable")
}

// ---------- SSE 流式 ----------

// relayStream 逐帧透传上游 SSE。逐帧 Flush，让客户端看到增量。
//
// 首字节前（拨号失败 / 5xx）与非流式一样最多换 2 次 provider。全部失败则立即 Reverse
// 并返回 502，不先承诺 SSE，也不把额度锁到对账宽限期。
//
// 结算规则：
//   - 读到 usage 帧 → 按上报结算；
//   - 已经转发过正文但没有 usage（客户端断连或上游提前结束）→ 用已转发文本估算 token 并结算，
//     避免对账把已经交付的内容全额退掉；
//   - 一个正文都没转发 → 保持 pending，交给对账。
func relayStream(c *gin.Context, b billingAPI, r routingAPI, log *slog.Logger, prov *client.RouteResult, req *chatRequest, requestID string, billID int64, estPrompt int64) {
	tried := map[uint64]bool{}
	failover := func(providerID uint64) bool {
		tried[providerID] = true
		r.Report(c.Request.Context(), providerID, false)
		if next, rerr := r.Route(c.Request.Context(), req.Model, triedSlice(tried)...); rerr == nil && next != nil {
			prov = next
			return true
		}
		return false
	}

	var upResp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		upstreamBody, _ := json.Marshal(req)
		uReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
			prov.BaseURL+"/v1/chat/completions", bytes.NewReader(upstreamBody))
		if err != nil {
			reverseWithBackground(b, log, requestID, billID, "build stream request failed")
			abort(c, http.StatusBadGateway, "upstream error")
			return
		}
		setUpstreamHeaders(uReq, c, true, prov.APIKey)

		resp, err := streamClient.Do(uReq)
		if err != nil {
			log.Warn("upstream dial failed, failover", "provider", prov.Name, "attempt", attempt+1, "err", err)
			if failover(prov.ProviderID) {
				continue
			}
			break
		}
		if resp.StatusCode >= 500 {
			// An error body may never reach EOF. Close it immediately so failover
			// and refund do not wait on an unbounded stream read.
			resp.Body.Close()
			log.Warn("upstream 5xx, failover", "provider", prov.Name, "status", resp.StatusCode, "attempt", attempt+1)
			if failover(prov.ProviderID) {
				continue
			}
			break
		}
		if resp.StatusCode/100 != 2 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			r.Report(c.Request.Context(), prov.ProviderID, true)
			reverseWithBackground(b, log, requestID, billID, "upstream 4xx: "+resp.Status)
			c.Data(resp.StatusCode, "application/json", body)
			return
		}
		upResp = resp
		break
	}
	if upResp == nil {
		log.Error("all stream providers failed", "request_id", requestID, "model", req.Model)
		reverseWithBackground(b, log, requestID, billID, "all providers failed")
		abort(c, http.StatusBadGateway, "all providers unavailable")
		return
	}
	defer upResp.Body.Close()

	// 流成功建立之后才写 SSE 头。中途失败不再换上游。
	r.Report(c.Request.Context(), prov.ProviderID, true)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Error("response writer not flushable", "request_id", requestID)
		return
	}

	var (
		reader    = bufio.NewReader(upResp.Body)
		usageSeen *usage
		doneSeen  bool
		forwarded strings.Builder
	)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF && usageSeen == nil && forwarded.Len() == 0 {
				log.Warn("upstream stream interrupted before content", "request_id", requestID, "err", err)
			}
			break
		}
		if _, werr := w.Write(line); werr != nil {
			log.Warn("client disconnected mid-stream", "request_id", requestID)
			break
		}
		flusher.Flush()
		if text := streamDeltaText(string(line)); text != "" {
			forwarded.WriteString(text)
		}
		if u := parseUsageFrame(string(line)); u != nil {
			usageSeen = u
		}
		if strings.Contains(string(line), "[DONE]") {
			doneSeen = true
			break
		}
	}

	if usageSeen != nil {
		settleAndLog(c, b, log, requestID, billID, int64(usageSeen.PromptTokens), int64(usageSeen.CompletionTokens), int64(prov.ProviderID))
		return
	}
	if text := strings.TrimSpace(forwarded.String()); text != "" {
		completion := tokens.Estimate(text)
		if completion < 1 {
			completion = 1
		}
		log.Info("stream ended without usage, settling estimate", "request_id", requestID, "completion_tokens", completion)
		settleAndLog(c, b, log, requestID, billID, estPrompt, completion, int64(prov.ProviderID))
		return
	}
	log.Info("stream incomplete, bill left pending", "request_id", requestID, "done", doneSeen)
}

// streamDeltaText 取出一帧里已经生成的正文，供断连时估算 completion token。
func streamDeltaText(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return ""
	}
	var frame struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return ""
	}
	var b strings.Builder
	for _, ch := range frame.Choices {
		b.WriteString(ch.Delta.Content)
		b.WriteString(ch.Message.Content)
	}
	return b.String()
}

// parseUsageFrame 从 SSE data 行解析 usage（仅当 choices 为空 + usage 非空才返回）。
func parseUsageFrame(line string) *usage {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return nil
	}
	var frame struct {
		Choices []json.RawMessage `json:"choices"`
		Usage   *usage            `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return nil
	}
	if len(frame.Choices) == 0 && frame.Usage != nil {
		return frame.Usage
	}
	return nil
}

// ---------- 收尾与公共 helpers ----------

// triedSlice 把「本请求已试失败」的 provider 集合转成切片传给 router 的 exclude 参数。
func triedSlice(tried map[uint64]bool) []uint64 {
	out := make([]uint64, 0, len(tried))
	for id := range tried {
		out = append(out, id)
	}
	return out
}

// settleAndLog ReportUsage + Settle（独立后台 ctx）。
func settleAndLog(c *gin.Context, b billingAPI, log *slog.Logger, requestID string, billID, prompt, completion, providerID int64) {
	settleCtx, settleCancel := client.BackgroundCtx(10 * time.Second)
	defer settleCancel()
	_, errU := b.ReportUsage(settleCtx, &billingv1.ReportUsageRequest{
		RequestId: requestID, BillId: billID, PromptTokens: prompt, CompletionTokens: completion,
		ProviderId: providerID,
	})
	if errU != nil {
		// Settle must only consume durably recorded usage and provider attribution.
		log.Error("usage report failed, bill left pending for reconciliation", "request_id", requestID, "err_report", errU)
		return
	}
	sResp, errS := b.Settle(settleCtx, &billingv1.SettleRequest{RequestId: requestID, BillId: billID})
	if errS != nil || sResp == nil || !sResp.Settled {
		// 返回给客户端已成功，但结算没落 → 交给 M4 对账补结算
		log.Error("settle failed, reconciliation will catch up",
			"request_id", requestID, "err_report", errU, "err_settle", errS)
		return
	}
	log.Info("settled", "request_id", requestID, "delta_quota", sResp.DeltaQuota, "balance_after", sResp.BalanceAfter)
}

func setUpstreamHeaders(uReq *http.Request, c *gin.Context, stream bool, upstreamKey string) {
	uReq.Header.Set("Content-Type", "application/json")
	auth := ""
	if upstreamKey != "" {
		auth = "Bearer " + upstreamKey // 真实渠道：用渠道自己的上游 key 鉴权，而不是租户 key
	} else if a := c.GetHeader("Authorization"); a != "" {
		auth = a // 无上游 key 的渠道（mock）维持透传
	}
	if auth != "" {
		uReq.Header.Set("Authorization", auth)
	}
	if stream {
		uReq.Header.Set("Accept", "text/event-stream")
	}
}

func estimateMessages(req *chatRequest) int64 {
	var total int64
	for _, m := range req.Messages {
		total += tokens.Estimate(m.Content)
	}
	return total
}

// reverseWithBackground 独立 ctx 退款：上游失败时退回预扣。若退款本身失败，
// 交 M4 对账冲正——所以这里只记日志，不让退款错误影响已发生的失败响应。
func reverseWithBackground(b billingAPI, log *slog.Logger, requestID string, billID int64, reason string) {
	ctx, cancel := client.BackgroundCtx(10 * time.Second)
	defer cancel()
	resp, err := b.Reverse(ctx, &billingv1.ReverseRequest{RequestId: requestID, BillId: billID, Reason: reason})
	if err != nil {
		log.Error("reverse failed", "request_id", requestID, "reason", reason, "err", err)
		return
	}
	log.Info("reversed", "request_id", requestID, "reversed", resp.Reversed, "refund", resp.RefundQuota, "reason", reason)
}

func abort(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": gin.H{"message": msg}})
}
