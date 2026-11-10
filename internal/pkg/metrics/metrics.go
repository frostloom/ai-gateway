// Package metrics Prometheus 可观测（M5）：HTTP 请求量 + 延迟直方图 + 在途数。
// gateway（gin）与 router（net/http）共用一套指标，服务名用 label 区分。
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_http_requests_total",
		Help: "HTTP 请求总量（按服务/方法/状态码）",
	}, []string{"service", "method", "status"})
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gateway_http_request_duration_seconds",
		Help:    "HTTP 请求延迟直方图",
		Buckets: prometheus.DefBuckets, // 5ms ~ 10s
	}, []string{"service", "method", "path"})
	activeRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gateway_http_in_flight_requests",
		Help: "在途 HTTP 请求数",
	}, []string{"service"})
)

// GinMiddleware gin 中间件：计请求量、延迟、在途数（服务名区分）。
func GinMiddleware(service string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		activeRequests.WithLabelValues(service).Inc()
		c.Next()
		activeRequests.WithLabelValues(service).Dec()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		httpRequestsTotal.WithLabelValues(service, c.Request.Method, strconv.Itoa(c.Writer.Status())).Inc()
		httpRequestDuration.WithLabelValues(service, c.Request.Method, path).Observe(time.Since(start).Seconds())
	}
}

// Handler 返回 /metrics 暴露端点（net/http 与 gin 通用）。
func Handler() http.Handler {
	return promhttp.Handler()
}

// ReportRequest 供 router 的 net/http 中间件调用：请求结束时记录指标。
func ReportRequest(service, method, status, path string, start time.Time) {
	httpRequestsTotal.WithLabelValues(service, method, status).Inc()
	httpRequestDuration.WithLabelValues(service, method, path).Observe(time.Since(start).Seconds())
}

// ---------- M10 Kafka 计费事件流指标 ----------

var (
	// billing_events_produced_total  relay 投递结果：ok=主 topic 成功 / dlq=超限改投 DLQ / error=失败待重试。
	billingEventsProduced = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_events_produced_total",
		Help: "outbox relay 投递计费事件总数（按结果 ok/dlq/error）",
	}, []string{"result"})
	billingOutboxMarkErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_outbox_mark_errors_total",
		Help: "Outbox sent or DLQ marker calls returning an error; commit outcome may be unknown",
	}, []string{"operation"})
	// billing_events_consumed_total  消费端按 phase（reserve/usage/settle/reverse）落库成功数。
	billingEventsConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_events_consumed_total",
		Help: "event-consumer 落库审计明细总数（按阶段）",
	}, []string{"phase"})
	// billing_events_decode_errors_total  载荷解码失败数（毒化消息跳过，不重投）。
	billingEventsDecodeErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_events_decode_errors_total",
		Help: "计费事件载荷解码失败总数",
	})
	// billing_consumer_lag_messages  消费组落后量 gauge（积压告警用）。
	billingConsumerLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "billing_consumer_lag_messages",
		Help: "event-consumer 消费落后量（未消费消息数）",
	})
)

// BillingEventsProduced 暴露 relay 投递计数（按结果）。
func BillingEventsProduced(result string) {
	billingEventsProduced.WithLabelValues(result).Inc()
}

// BillingOutboxMarkError counts failed marker calls, not confirmed rollbacks.
func BillingOutboxMarkError(operation string) {
	if operation == "sent" || operation == "dlq" {
		billingOutboxMarkErrors.WithLabelValues(operation).Inc()
	}
}

// BillingEventsConsumed 暴露消费落库计数（按阶段）。
func BillingEventsConsumed(phase string) {
	billingEventsConsumed.WithLabelValues(phase).Inc()
}

// BillingEventsDecodeErrors 暴露载荷解码失败计数。
func BillingEventsDecodeErrors() {
	billingEventsDecodeErrors.Inc()
}

// SetBillingConsumerLag 更新消费落后量 gauge。
func SetBillingConsumerLag(lag int64) {
	billingConsumerLag.Set(float64(lag))
}

// routerReportTotal records only the gateway's observed HTTP outcome. A 200
// does not imply the router persisted its state.
var routerReportTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_router_report_total",
	Help: "Router report HTTP outcomes observed by the gateway; HTTP 200 does not prove persistence",
}, []string{"result"})

// RouterReport records a bounded outcome without provider or request labels.
func RouterReport(result string) {
	switch result {
	case "http_200", "http_error", "transport_error", "body_error", "invalid_request":
		routerReportTotal.WithLabelValues(result).Inc()
	}
}

// routerPersistenceErrors counts calls that returned an error, including
// potentially ambiguous commit outcomes. It does not count zero-row updates.
var routerPersistenceErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "router_persistence_errors_total",
	Help: "Router provider persistence calls returning an error; commit outcome may be unknown",
}, []string{"operation"})

// RouterPersistenceError accepts only the three provider write operations.
func RouterPersistenceError(operation string) {
	switch operation {
	case "increment", "reset", "ban":
		routerPersistenceErrors.WithLabelValues(operation).Inc()
	}
}

// billingCompletionTotal records the gateway's observed response to its
// completion RPCs; an unacknowledged call may nevertheless have committed.
var billingCompletionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_billing_completion_total",
	Help: "Gateway-observed billing completion RPC outcomes; unacknowledged calls may have committed",
}, []string{"result"})

var billingCompletionStageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "gateway_billing_completion_stage_seconds",
	Help:    "Time spent in gateway billing completion RPCs; calls may have committed on error",
	Buckets: prometheus.DefBuckets,
}, []string{"stage"})

var routerReportDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "gateway_router_report_duration_seconds",
	Help:    "Time the gateway waits for a router report, including failed calls",
	Buckets: prometheus.DefBuckets,
})

func RouterReportDuration(duration time.Duration) {
	routerReportDuration.Observe(duration.Seconds())
}

// BillingCompletionStage records bounded, non-identifying RPC stage timings.
func BillingCompletionStage(stage string, duration time.Duration) {
	switch stage {
	case "usage", "settle":
		billingCompletionStageDuration.WithLabelValues(stage).Observe(duration.Seconds())
	}
}

var requestHopDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "gateway_request_hop_seconds",
	Help:    "Time spent in one gateway or billing hop of a chat request",
	Buckets: prometheus.DefBuckets,
}, []string{"hop"})

// RequestHop records one bounded hop. Gateway and billing expose it on their own metrics.
func RequestHop(hop string, duration time.Duration) {
	switch hop {
	case "auth", "idempotency", "ratelimit", "route", "reserve", "upstream",
		"reserve_match", "reserve_lua", "reserve_enqueue",
		"settle_lookup", "settle_guard", "settle_match", "settle_lua", "settle_queue":
		requestHopDuration.WithLabelValues(hop).Observe(duration.Seconds())
	}
}

var billingAuthCacheRead = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "billing_auth_cache_read_total",
	Help: "Auth cache read outcomes; misses and errors fall back to the authority, browser sessions are never cached",
}, []string{"result"})

// BillingAuthCacheRead records only bounded, non-identifying cache outcomes.
func BillingAuthCacheRead(result string) {
	switch result {
	case "hit", "miss", "invalid", "error", "bypass":
		billingAuthCacheRead.WithLabelValues(result).Inc()
	}
}

// BillingCompletion accepts only bounded, non-identifying outcome labels.
func BillingCompletion(result string) {
	switch result {
	case "usage_unacknowledged", "settlement_unacknowledged", "settlement_acknowledged":
		billingCompletionTotal.WithLabelValues(result).Inc()
	}
}
