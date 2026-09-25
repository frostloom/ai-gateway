// Package handler gateway 的 OpenAI 兼容 HTTP 处理。
// chat.go：/v1/chat/completions 编排（非流式 + SSE 流式）——
//
//	路由 → 预占 → 转发上游 → ReportUsage+结算（独立后台 ctx） → 失败保持 pending（M4 对账冲正）
//
// 计费规则一句话：只有「上游产出了完整结果（拿到 usage 帧）」才结算；
// 其余情况（客户端断连/上游中断/非 2xx）一律不结算，留下 pending 账单交给 M4 对账收敛。
// 这是流式计费的正确性核心：流中断的边界太模糊（谁先断、有没有 half-usage），
// 网关自己猜会误判，统一用对账「以 marker 一锤定音」。
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
func Chat(b *client.Billing, r *client.Router, log *slog.Logger, upstreamTimeout time.Duration) gin.HandlerFunc {
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
func forwardNonStream(c *gin.Context, b *client.Billing, r *client.Router, log *slog.Logger, prov *client.RouteResult, req *chatRequest, requestID string, billID int64, estPrompt int64, _ time.Duration) {
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
		settleAndLog(c, b, log, requestID, billID, prompt, completion)
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
// 结算规则（关键）：
//   - 读到带 usage 的终帧（choices 为空 + usage 字段）→ 记录 usage；
//   - 读到 [DONE] → 流正常结束 → 若已拿到 usage 则结算（按实际，不按预占）；
//   - 客户端写失败（断连）/上游读错误 → 直接返回，不结算 → 账单保持 pending → M4 对账冲正。
//     （若断连发生但 usage 已到手，则照收——「上游跑完就算数，客户端断连不影响计费」）
func relayStream(c *gin.Context, b *client.Billing, r *client.Router, log *slog.Logger, prov *client.RouteResult, req *chatRequest, requestID string, billID int64, estPrompt int64) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	upstreamBody, _ := json.Marshal(req)
	uReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
		prov.BaseURL+"/v1/chat/completions", bytes.NewReader(upstreamBody))
	if err != nil {
		log.Error("build stream request", "request_id", requestID, "err", err)
		return
	}
	setUpstreamHeaders(uReq, c, true, prov.APIKey)

	// 流式用无总超时的共享 client（长连接），靠请求 ctx 取消；数据读取本身逐行阻塞。
	upResp, err := streamClient.Do(uReq)
	if err != nil {
		// 建连失败：provider 故障 → 上报。账单保持 pending（M4 冲正）。
		r.Report(c.Request.Context(), prov.ProviderID, false)
		log.Warn("upstream dial failed, bill stays pending", "request_id", requestID, "err", err)
		return
	}
	defer upResp.Body.Close()
	if upResp.StatusCode/100 != 2 {
		// 首个字节前 5xx：provider 故障 → 上报。流式不重试（无法保证上游没跑一半）。
		if upResp.StatusCode >= 500 {
			r.Report(c.Request.Context(), prov.ProviderID, false)
		} else {
			r.Report(c.Request.Context(), prov.ProviderID, true)
		}
		log.Warn("upstream non-2xx, bill stays pending", "request_id", requestID, "status", upResp.StatusCode)
		c.Data(upResp.StatusCode, "application/json", nil)
		return
	}
	// 流成功建立：provider 健康（中途失败不追究——分不清是 provider 还是客户端断的）。
	r.Report(c.Request.Context(), prov.ProviderID, true)

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
	)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// EOF 或上游中断：normal 收尾（[DONE] 或 usage 已到手）则结算，否则 pending
			if err != io.EOF && usageSeen == nil && !doneSeen {
				log.Warn("upstream stream interrupted, bill stays pending", "request_id", requestID, "err", err)
			}
			break
		}

		// 透传每一行 + 立即 Flush（增量可见的关键）
		if _, werr := w.Write(line); werr != nil {
			log.Warn("client disconnected mid-stream, bill stays pending", "request_id", requestID)
			break
		}
		flusher.Flush()

		if u := parseUsageFrame(string(line)); u != nil {
			usageSeen = u
		}
		if strings.Contains(string(line), "[DONE]") {
			doneSeen = true
			break
		}
	}

	if usageSeen != nil {
		// 上游跑完（拿到 usage）→ 照收，按实际结算，不按预占
		settleAndLog(c, b, log, requestID, billID, int64(usageSeen.PromptTokens), int64(usageSeen.CompletionTokens))
		return
	}
	// 没拿到 usage → 流未完成，保持 pending，M4 对账会冲正
	log.Info("stream incomplete, bill left pending", "request_id", requestID, "done", doneSeen)
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
func settleAndLog(c *gin.Context, b *client.Billing, log *slog.Logger, requestID string, billID, prompt, completion int64) {
	settleCtx, settleCancel := client.BackgroundCtx(10 * time.Second)
	defer settleCancel()
	_, errU := b.ReportUsage(settleCtx, &billingv1.ReportUsageRequest{
		RequestId: requestID, BillId: billID, PromptTokens: prompt, CompletionTokens: completion,
	})
	sResp, errS := b.Settle(settleCtx, &billingv1.SettleRequest{RequestId: requestID, BillId: billID})
	if errU != nil || errS != nil || sResp == nil || !sResp.Settled {
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
func reverseWithBackground(b *client.Billing, log *slog.Logger, requestID string, billID int64, reason string) {
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
