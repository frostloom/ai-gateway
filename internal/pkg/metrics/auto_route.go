// auto 路由观测指标：决策计数、判定延迟、兜底/降级计数与熔断相位。
// 仅 auto 请求在 Decide 之后记录；非 auto 请求零变化（不触发任何指标）。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ---------- auto 路由指标 ----------

var (
	// autoDecisionsTotal auto 路由决策总数（按模式 off/shadow/on 与最终服务模型，降级后为降级档）。
	autoDecisionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_auto_decisions_total",
		Help: "auto 路由决策总数（按模式与最终服务模型）",
	}, []string{"mode", "model"})
	// autoDecisionLatency Selector.Decide 的判定耗时直方图（毫秒转秒，含兜底路径）。
	autoDecisionLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "gateway_auto_decision_latency_seconds",
		Help:    "auto 路由 JEV 判定耗时直方图",
		Buckets: prometheus.DefBuckets,
	})
	// autoFallbackTotal JEV 判定不可用兜底计数（按原因码 breaker_open/unparseable/decode/jev_error/timeout）。
	autoFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_auto_fallback_total",
		Help: "auto 路由 JEV 判定失败兜底总数（按原因码）",
	}, []string{"reason"})
	// autoDegradedTotal route 探测失败后的跨档降级计数（原因码固定两种）。
	autoDegradedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_auto_degraded_total",
		Help: "auto 路由 route 探测失败跨档降级总数（strong_unavailable/cheap_unavailable）",
	}, []string{"reason"})
	// autoBreakerState JEV 判定熔断器相位 gauge：closed=0，open/half_open=1。
	autoBreakerState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateway_auto_breaker_state",
		Help: "auto 路由熔断器相位（closed=0，open/half_open=1），每次决策后更新",
	})
)

// AutoDecision 记录一次 auto 路由决策：决策计数（按模式与最终服务模型）与判定延迟。
func AutoDecision(mode, model string, latencyMS float64) {
	autoDecisionsTotal.WithLabelValues(mode, model).Inc()
	autoDecisionLatency.Observe(latencyMS / 1000)
}

// AutoFallback 记录一次判定不可用兜底（按原因码）；空原因码不记。
func AutoFallback(reason string) {
	if reason == "" {
		return
	}
	autoFallbackTotal.WithLabelValues(reason).Inc()
}

// AutoDegraded 记录一次 route 探测失败后的跨档降级，只接受两种固定原因码。
func AutoDegraded(reason string) {
	switch reason {
	case "strong_unavailable", "cheap_unavailable":
		autoDegradedTotal.WithLabelValues(reason).Inc()
	}
}

// SetAutoBreakerState 更新熔断器相位 gauge：closed=0，open/half_open=1。每次决策后调用。
func SetAutoBreakerState(state string) {
	if state == "closed" {
		autoBreakerState.Set(0)
		return
	}
	autoBreakerState.Set(1)
}
