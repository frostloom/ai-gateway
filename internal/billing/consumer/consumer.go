// Package consumer event-consumer 的逻辑：消费 billing.events（+ DLQ）→ 写 audit_events
// 审计明细 + 指标（M10）。
//
// 语义：at-least-once 消费（FetchMessage → 落库 → CommitMessages），落库幂等靠
// audit_events.uk_event_id 唯一索引（重投/多副本撞键 → UPDATE id=id no-op），等价 exactly-once。
// 载荷解码失败的消息直接 commit 跳过（毒化消息重投没有意义），metrics 记 decode_errors。
package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	appkafka "github.com/frostloom/ai-gateway/internal/pkg/kafka"
)

// Config 消费端参数。
type Config struct {
	DLQTopic string // billing.events.dlq（用于区分消息来源 source=dlq）
}

// Stats 累计统计（/status 观测用）。
type Stats struct {
	Consumed   int64     `json:"consumed"`    // 落库成功事件数（含幂等重投吸收）
	DecodeErrs int64     `json:"decode_errors"` // 载荷解码失败数
	Errors     int64     `json:"errors"`      // 落库失败未提交（待重投）数
	Lag        int64     `json:"lag"`         // 消费落后量
	LastAt     time.Time `json:"last_at"`     // 最近一次处理时间
}

// EventConsumer 消费端。
type EventConsumer struct {
	st  *store.Store
	kc  *appkafka.Consumer
	cfg Config
	log *slog.Logger

	mu    sync.Mutex
	stats Stats
}

// New 建消费端。
func New(st *store.Store, kc *appkafka.Consumer, cfg Config, log *slog.Logger) *EventConsumer {
	return &EventConsumer{st: st, kc: kc, cfg: cfg, log: log}
}

// Run 常驻消费直到 ctx 取消。后台 goroutine 周期性刷 lag gauge。
func (c *EventConsumer) Run(ctx context.Context) error {
	go c.lagLoop(ctx)
	c.log.Info("event consumer started", "dlq_topic", c.cfg.DLQTopic)
	for {
		msg, err := c.kc.FetchMessage(ctx)
		if err != nil {
			// ctx 取消/超时（-once 模式兜底）都退出；其余（broker 暂不可达）退避重试。
			if ctx.Err() != nil {
				return nil
			}
			c.log.Error("fetch message", "err", err)
			time.Sleep(time.Second)
			continue
		}
		rec, err := c.handle(ctx, msg)
		c.record(rec)
		if err != nil {
			// 落库失败：不 commit → broker 重投（at-least-once 的自愈路径）
			c.log.Error("handle message", "event_id", string(msg.Key), "err", err)
			continue
		}
		if err := c.kc.CommitMessages(ctx, msg); err != nil {
			c.log.Error("commit message", "event_id", string(msg.Key), "err", err)
			// commit 失败由下轮 FetchMessage 重投 + uk_event_id 幂等吸收
		}
	}
}

// lagLoop 周期刷消费落后量 gauge。
func (c *EventConsumer) lagLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lag, err := c.kc.Lag(ctx)
			if err != nil {
				c.log.Debug("lag", "err", err)
				continue
			}
			metrics.SetBillingConsumerLag(lag)
			c.mu.Lock()
			c.stats.Lag = lag
			c.mu.Unlock()
		}
	}
}

// handle 处理一条消息：解码 → 幂等落审计 → 指标。返回 nil 表示应 commit。
func (c *EventConsumer) handle(ctx context.Context, msg kafka.Message) (Stats, error) {
	var ev store.BillingEvent
	if err := json.Unmarshal(msg.Value, &ev); err != nil {
		metrics.BillingEventsDecodeErrors()
		c.log.Error("decode event", "key", string(msg.Key), "err", err)
		return Stats{DecodeErrs: 1}, nil // 毒化消息：跳过不重投
	}
	if ev.EventID == "" {
		ev.EventID = string(msg.Key) // 兜底：老/外部消息没带 event_id 时用 key
	}
	source := "kafka"
	if msg.Topic == c.cfg.DLQTopic {
		source = "dlq"
	}
	audit := &store.AuditEvent{
		EventID:          ev.EventID,
		TenantID:         ev.TenantID,
		RequestID:        ev.RequestID,
		Phase:            ev.Phase,
		AmountCents:      ev.AmountCents,
		Model:            ev.Model,
		Funding:          ev.Funding,
		PromptTokens:     ev.PromptTokens,
		CompletionTokens: ev.CompletionTokens,
		Payload:          string(msg.Value),
		Source:           source,
		KafkaPartition:   int32(msg.Partition),
		KafkaOffset:      msg.Offset,
		ConsumedAt:       time.Now(),
	}
	if err := c.st.UpsertAudit(ctx, audit); err != nil {
		return Stats{Errors: 1}, err
	}
	metrics.BillingEventsConsumed(ev.Phase)
	return Stats{Consumed: 1}, nil
}

// record 累计统计。
func (c *EventConsumer) record(s Stats) {
	c.mu.Lock()
	c.stats.Consumed += s.Consumed
	c.stats.DecodeErrs += s.DecodeErrs
	c.stats.Errors += s.Errors
	c.stats.LastAt = time.Now()
	c.mu.Unlock()
}

// Stats 返回累计统计（/status 用）。
func (c *EventConsumer) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// HTTPHandler /healthz + /status + /metrics（端口 EVENT_CONSUMER_PORT，供探活与运维）。
func (c *EventConsumer) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"svc":"event-consumer"}`))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		s := c.Stats()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "svc": "event-consumer",
			"consumed": s.Consumed, "decode_errors": s.DecodeErrs,
			"errors": s.Errors, "lag": s.Lag, "last_at": s.LastAt,
		})
	})
	mux.Handle("/metrics", metrics.Handler())
	return mux
}
