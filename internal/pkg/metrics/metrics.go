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
