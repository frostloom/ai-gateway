// Package middleware gateway 中间件：API Key 鉴权 · 租户限流 · 幂等键 · auto 路由。
// auto_route.go 实现 model:"auto" 请求的三态路由（off 拒绝 / shadow 只记录 / on 改写）：
// 读全量 body 取顶层 model，auto 请求经 Selector 判定后改写 model 并标记 X-Auto-* 响应头，
// 非 auto 请求字节原样透传（零变化）。
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/autoroute"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
)

// autoRouteModelField 触发 auto 路由的 model 字面值。
const autoRouteModelField = "auto"

// autoRouteMaxBody 读 body 上限 8MiB，与 Chat/Native handler 一致。
const autoRouteMaxBody = 8 << 20

// AutoRoute 处理 model=="auto" 的请求路由。
//
// 非 auto 请求（含 body 非 JSON、缺 model、model 非字符串）零变化：body 原字节装回
// c.Request.Body 透传，不设任何 X-Auto- 头、不改 ContentLength。auto 请求按模式：
//   - off：400 `model not listed (auto routing disabled)`，不触达下游；
//   - shadow：Selector 判定但按 ShadowServe 实际服务，X-Auto-Applied: false；
//   - on：按判定档（或 JEV 失败兜底档）改写 body 的 model，同步 ContentLength。
//
// 响应头：X-Auto-Model（实际服务模型）、X-Auto-Would-Be 与 X-Auto-Score（仅判定成功时）、
// X-Auto-Decision-Ms（判定耗时毫秒）、X-Auto-Applied、X-Auto-Fallback（仅兜底时）、
// X-Auto-Degraded（仅 route 探测失败跨档改写时，strong_unavailable / cheap_unavailable）。
//
// route 探测：仅 on 模式且 JEV 判定成功时，在 Decide 之后、c.Next() 之前对判定档做一次
// probe；失败按非对称矩阵跨档——判定强档且 DegradeStrongUnavailable 时降便宜档
// （strong_unavailable）、判定便宜档且 UpgradeCheapUnavailable 时升强档（cheap_unavailable），
// 对应开关关闭则保持原档且无 Degraded 头；probe 成功或降级后均不二次探测。
// shadow 实际服务固定档、兜底判定（未判定档位）不探测，保持各自零流量影响语义。
// 指标：每次决策记录 decisions/latency/breaker_state，兜底与降级另有计数；非 auto 请求零指标。
// sel 仅在 off 态可为 nil；log 为 nil 时不输出日志（日志含
// method/path/mode/model/would_be/score/fallback/degraded/decision_ms/breaker_state）。
func AutoRoute(cfg autoroute.Config, sel *autoroute.Selector, routeProbe func(ctx context.Context, model string) error, log *slog.Logger) gin.HandlerFunc {
	if cfg.Mode != autoroute.ModeOff && sel == nil {
		panic("middleware: AutoRoute 非 off 模式必须提供 Selector")
	}
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, autoRouteMaxBody))
		if err != nil {
			abortAutoRoute(c, http.StatusBadRequest, "invalid request body")
			return
		}
		model, ok := autoRouteRequestModel(body)
		if !ok || model != autoRouteModelField {
			// 非 auto 请求零变化：原字节装回透传
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Next()
			return
		}
		if cfg.Mode == autoroute.ModeOff {
			abortAutoRoute(c, http.StatusBadRequest, "model not listed (auto routing disabled)")
			return
		}

		dec := sel.Decide(c.Request.Context(), time.Now(), body)
		breakerState := sel.BreakerState()
		metrics.SetAutoBreakerState(breakerState)

		// route 探测：只对判定档做一次；降级/升级后不二次探测目标档。
		served := dec.ServedModel
		degraded := ""
		if routeProbe != nil && cfg.Mode == autoroute.ModeOn && dec.Judged {
			if err := routeProbe(c.Request.Context(), dec.WouldBeModel); err != nil {
				switch {
				case dec.WouldBeModel == cfg.StrongTier && cfg.DegradeStrongUnavailable:
					served = cfg.CheapTier
					degraded = "strong_unavailable"
				case dec.WouldBeModel == cfg.CheapTier && cfg.UpgradeCheapUnavailable:
					served = cfg.StrongTier
					degraded = "cheap_unavailable"
				}
			}
		}

		newBody, err := autoroute.RewriteModel(body, served)
		if err != nil {
			// body 已确认为含 model 的 JSON 对象，此处错误理论不可达；防御性原样透传。
			if log != nil {
				log.Error("auto_route", "err", err.Error())
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Next()
			return
		}

		dec.ServedModel = served
		setAutoRouteHeaders(c.Writer.Header(), dec)
		if degraded != "" {
			c.Writer.Header().Set("X-Auto-Degraded", degraded)
			metrics.AutoDegraded(degraded)
		}

		metrics.AutoDecision(string(cfg.Mode), served, dec.LatencyMS)
		metrics.AutoFallback(dec.Fallback)
		if log != nil {
			log.Info("auto_route",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"mode", string(cfg.Mode),
				"model", served,
				"would_be", dec.WouldBeModel,
				"score", dec.Score,
				"fallback", dec.Fallback,
				"degraded", degraded,
				"decision_ms", dec.LatencyMS,
				"breaker_state", breakerState)
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(newBody))
		c.Request.ContentLength = int64(len(newBody))
		c.Next()
	}
}

// autoRouteRequestModel 提取顶层 JSON 对象的 model 字符串值；
// body 非 JSON 对象、缺 model 或值非字符串返回 ok=false。
func autoRouteRequestModel(body []byte) (model string, ok bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return "", false
	}
	raw, has := top["model"]
	if !has {
		return "", false
	}
	if err := json.Unmarshal(raw, &model); err != nil {
		return "", false
	}
	return model, true
}

// setAutoRouteHeaders 写 auto 路由响应头；判定未成功时无 Would-Be/Score，无兜底时无 Fallback。
func setAutoRouteHeaders(h http.Header, dec autoroute.Decision) {
	h.Set("X-Auto-Model", dec.ServedModel)
	h.Set("X-Auto-Applied", strconv.FormatBool(dec.Applied))
	h.Set("X-Auto-Decision-Ms", strconv.FormatFloat(dec.LatencyMS, 'f', -1, 64))
	if dec.Judged {
		h.Set("X-Auto-Would-Be", dec.WouldBeModel)
		h.Set("X-Auto-Score", strconv.FormatFloat(dec.Score, 'f', -1, 64))
	}
	if dec.Fallback != "" {
		h.Set("X-Auto-Fallback", dec.Fallback)
	}
}

// abortAutoRoute 以 {"error":{"message":…}} 形状中止请求。
func abortAutoRoute(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": gin.H{"message": msg}})
}
