// Package outbox billing 的 outbox relay（M10）：轮询未投计费事件 → produce 到 Kafka
// → mark sent。失败 bump attempts，超 OUTBOX_MAX_ATTEMPTS 改投 DLQ topic。
//
// 为什么需要它：记账与 Kafka 是两套系统，事务提交后 broker 可能挂/断。先落同事务
// outbox 行，relay 单写者兜底投递，事件不丢；消费端 uk_event_id 幂等吸收重复投递。
package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
)

// Producer 投递抽象：主 topic 与 DLQ topic 各一个实现（kafka.Producer）。
type Producer interface {
	Publish(ctx context.Context, key string, value []byte) error
}

// Store outbox 行存取的最小接口（*store.Store 天然满足；单测用内存假实现）。
type Store interface {
	PollOutbox(ctx context.Context, limit int, maxAttempts int) ([]store.OutboxEvent, error)
	MarkOutboxSent(ctx context.Context, id uint64) error
	BumpOutboxAttempt(ctx context.Context, id uint64, errMsg string) error
	MarkOutboxDLQ(ctx context.Context, id uint64, errMsg string) error
}

var _ Store = (*store.Store)(nil)

// Config relay 参数。
type Config struct {
	Topic        string        // 主 topic（billing.events）
	DLQTopic     string        // 死信 topic（billing.events.dlq）
	PollInterval time.Duration // 轮询周期（默认 1s）
	MaxAttempts  int           // 投递上限，超限改投 DLQ（默认 5）
	BatchSize    int           // 每轮取批（默认 100）
}

// Stats 最近一轮 flush 统计（/status 观测用）。
type Stats struct {
	Polled    int64     `json:"polled"`     // 本轮取到的待投行
	Sent      int64     `json:"sent"`       // 主 topic 投递成功
	Failed    int64     `json:"failed"`     // 失败待重试
	DLQed     int64     `json:"dlqed"`      // 超限改投 DLQ
	LastRunAt time.Time `json:"last_run_at"` // 本轮结束时间
}

// Relay outbox 轮询投递器。
type Relay struct {
	st      Store
	prod    Producer
	dlqProd Producer
	cfg     Config
	log     *slog.Logger

	mu   sync.Mutex
	last Stats
}

// NewRelay 建 relay。prod 投主 topic，dlqProd 投 DLQ topic。
func NewRelay(st Store, prod, dlqProd Producer, cfg Config, log *slog.Logger) *Relay {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	return &Relay{st: st, prod: prod, dlqProd: dlqProd, cfg: cfg, log: log}
}

// Run 常驻轮询直到 ctx 取消。启动先跑一轮（积压立刻排空），再按 PollInterval 循环。
func (r *Relay) Run(ctx context.Context) {
	r.log.Info("outbox relay started", "topic", r.cfg.Topic, "dlq_topic", r.cfg.DLQTopic,
		"poll_interval", r.cfg.PollInterval, "max_attempts", r.cfg.MaxAttempts)
	r.flush(ctx)
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.log.Info("outbox relay stopped")
			return
		case <-ticker.C:
			r.flush(ctx)
		}
	}
}

// FlushOnce 投递一批未投事件，返回本轮统计（可测试直接调用）。
func (r *Relay) FlushOnce(ctx context.Context) (Stats, error) {
	rows, err := r.st.PollOutbox(ctx, r.cfg.BatchSize, r.cfg.MaxAttempts)
	if err != nil {
		return Stats{}, fmt.Errorf("poll outbox: %w", err)
	}
	st := Stats{Polled: int64(len(rows))}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			break
		}
		value := []byte(row.Payload)
		if err := r.prod.Publish(ctx, row.EventKey, value); err != nil {
			metrics.BillingEventsProduced("error")
			st.Failed++
			// 超限 → 改投 DLQ。
			if row.Attempts+1 >= r.cfg.MaxAttempts {
				if dErr := r.dlqProd.Publish(ctx, row.EventKey, value); dErr != nil {
					// DLQ 也失败（通常是 broker 宕机）→ 不标终态：attempts/sent_at 都不动，
					// 该行仍满足 PollOutbox 过滤（attempts < max），下轮继续重试；
					// 等 broker 恢复后主 topic 或 DLQ 必有一端成功，事件不丢。
					r.log.Error("publish dlq", "event_key", row.EventKey, "err", dErr)
					continue
				}
				metrics.BillingEventsProduced("dlq")
				if err := r.st.MarkOutboxDLQ(ctx, row.ID, err.Error()); err != nil {
					r.log.Error("mark dlq", "id", row.ID, "err", err)
					if bErr := r.st.BumpOutboxAttempt(ctx, row.ID, err.Error()); bErr != nil {
						r.log.Error("bump attempt fallback", "id", row.ID, "err", bErr)
					}
				}
				st.DLQed++
				continue
			}
			if err := r.st.BumpOutboxAttempt(ctx, row.ID, err.Error()); err != nil {
				r.log.Error("bump attempt", "id", row.ID, "err", err)
			}
			continue
		}
		// 已 produce，标记 sent。若标记失败下轮会重投——由消费端 uk_event_id 幂等吸收
		//（at-least-once 的正常代价，不丢不重库）。
		if err := r.st.MarkOutboxSent(ctx, row.ID); err != nil {
			r.log.Error("mark sent", "id", row.ID, "err", err)
		}
		metrics.BillingEventsProduced("ok")
		st.Sent++
	}
	st.LastRunAt = time.Now()
	r.mu.Lock()
	r.last = st
	r.mu.Unlock()
	return st, nil
}

// Stats 返回最近一轮 flush 统计（/status 用）。
func (r *Relay) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// flush Run 内部包装：flush 出错（如 MySQL 不可达）只记日志，下轮再试。
func (r *Relay) flush(ctx context.Context) {
	st, err := r.FlushOnce(ctx)
	if err != nil {
		r.log.Error("outbox flush", "err", err)
		return
	}
	if st.Polled > 0 {
		r.log.Info("outbox flush", "polled", st.Polled, "sent", st.Sent,
			"failed", st.Failed, "dlqed", st.DLQed)
	}
}
