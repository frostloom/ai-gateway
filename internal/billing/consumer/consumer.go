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
	appkafka "github.com/frostloom/ai-gateway/internal/pkg/kafka"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
)

// Config 消费端参数。
type Config struct {
	DLQTopic string // billing.events.dlq（用于区分消息来源 source=dlq）
}

// Stats 累计统计（/status 观测用）。
type Stats struct {
	Consumed   int64     `json:"consumed"`      // 落库成功事件数（含幂等重投吸收）
	DecodeErrs int64     `json:"decode_errors"` // 载荷解码失败数
	Errors     int64     `json:"errors"`        // 落库失败未提交（待重投）数
	Lag        int64     `json:"lag"`           // 消费落后量
	LastAt     time.Time `json:"last_at"`       // 最近一次处理时间
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

// Run 常驻消费直到 ctx 取消。消息先攒到最多 200 条或 25 毫秒，再一次写入 audit_events。
func (c *EventConsumer) Run(ctx context.Context) error {
	go c.lagLoop(ctx)
	c.log.Info("event consumer started", "dlq_topic", c.cfg.DLQTopic)
	pending := make([]kafka.Message, 0, 200)
	for {
		if len(pending) >= 200 {
			if err := c.applyBatch(ctx, pending); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				c.log.Error("apply batch", "size", len(pending), "err", err)
				time.Sleep(time.Second)
				continue
			}
			pending = pending[:0]
		}
		fetchCtx := ctx
		cancel := func() {}
		if len(pending) > 0 {
			var stop context.CancelFunc
			fetchCtx, stop = context.WithTimeout(ctx, 25*time.Millisecond)
			cancel = stop
		}
		msg, err := c.kc.FetchMessage(fetchCtx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if len(pending) > 0 {
				if err := c.applyBatch(ctx, pending); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					c.log.Error("apply batch", "size", len(pending), "err", err)
					time.Sleep(time.Second)
					continue
				}
				pending = pending[:0]
				continue
			}
			c.log.Error("fetch message", "err", err)
			time.Sleep(time.Second)
			continue
		}
		pending = append(pending, msg)
	}
}

func (c *EventConsumer) applyBatch(ctx context.Context, msgs []kafka.Message) error {
	rows := make([]store.AuditEvent, 0, len(msgs))
	events := make([]*store.BillingEvent, 0, len(msgs))
	var decodeErrs int64
	for _, msg := range msgs {
		var ev store.BillingEvent
		if err := json.Unmarshal(msg.Value, &ev); err != nil {
			metrics.BillingEventsDecodeErrors()
			c.log.Error("decode event", "key", string(msg.Key), "err", err)
			decodeErrs++
			continue
		}
		if ev.EventID == "" {
			ev.EventID = string(msg.Key)
		}
		if ev.EventID == "" {
			decodeErrs++
			continue
		}
		source := "kafka"
		if msg.Topic == c.cfg.DLQTopic {
			source = "dlq"
		}
		events = append(events, &ev)
		rows = append(rows, store.AuditEvent{
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
		})
	}
	if err := c.st.ApplyLedgerEvents(ctx, events); err != nil {
		c.record(Stats{Errors: int64(len(events)), DecodeErrs: decodeErrs})
		return err
	}
	if err := c.st.UpsertAuditBatch(ctx, rows); err != nil {
		c.record(Stats{Errors: int64(len(rows)), DecodeErrs: decodeErrs})
		return err
	}
	if err := c.kc.CommitMessages(ctx, msgs...); err != nil {
		c.record(Stats{Errors: int64(len(msgs)), DecodeErrs: decodeErrs})
		return err
	}
	for _, row := range rows {
		metrics.BillingEventsConsumed(row.Phase)
	}
	c.record(Stats{Consumed: int64(len(rows)), DecodeErrs: decodeErrs})
	return nil
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
