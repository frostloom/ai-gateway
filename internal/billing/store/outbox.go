// Kafka 计费事件流的事务性 outbox（M10）。
//
// 记账一致性设计：bill 行与 outbox 行在同一 MySQL 事务提交（Reserve/ReportUsage/
// Settle/Reverse 的 DB 写都包进 s.db.WithContext(ctx).Transaction）；relay goroutine
// 轮询 outbox 未投行 → produce 到 Kafka → mark sent。投递失败 bump attempts，超
// OUTBOX_MAX_ATTEMPTS 改投 DLQ topic（billing.events.dlq）。
//
// 为什么带 outbox 而不是直接异步写 Kafka：Kafka 是独立系统，记账事务提交后它可能
// 挂/断——先把事件与账目原子落库，再由 relay 兜底投递，事件不丢不重
// （uk_event_key 唯一索引 + relay 单写者 + 消费端 uk_event_id 幂等）。
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Kafka topic 常量（与 docker-compose kafka-init 建的 topic 保持一致）。
const (
	TopicBillingEvents = "billing.events"
	TopicBillingDLQ    = "billing.events.dlq"
)

// OutboxEvent 待投递计费事件（事务内写入，relay 轮询投递）。
type OutboxEvent struct {
	ID        uint64     `gorm:"primaryKey;index:idx_unsent,priority:3"`
	Topic     string     `gorm:"size:64;not null"`
	EventKey  string     `gorm:"size:128;uniqueIndex:uk_event_key;not null"` // {request_id}:{phase} 幂等键
	Payload   string     `gorm:"type:json;not null"`                         // 事件体（BillingEvent JSON）
	CreatedAt time.Time  `gorm:"not null"`
	SentAt    *time.Time `gorm:"index:idx_unsent,priority:1"` // NULL=未投
	Attempts  int        `gorm:"not null;default:0;index:idx_unsent,priority:2"`
	LastError string     `gorm:"size:512;not null;default:''"`
}

// BillingEvent 计费事件载荷（outbox payload / Kafka 消息 value / audit_events 来源，三端同构）。
// 自包含：消费端只依赖本条消息即可还原审计明细，不回查 bills。
type BillingEvent struct {
	EventID          string    `json:"event_id"` // {request_id}:{phase} 幂等键
	TenantID         uint64    `json:"tenant_id"`
	RequestID        string    `json:"request_id"`
	Phase            string    `json:"phase"` // reserve|usage|settle|reverse
	AmountCents      int64     `json:"amount_cents"`
	Model            string    `json:"model"`
	Funding          string    `json:"funding"`
	PromptTokens     *int64    `json:"prompt_tokens,omitempty"`
	CompletionTokens *int64    `json:"completion_tokens,omitempty"`
	BillID           uint64    `json:"bill_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// newBillingEvent 由账目行构造事件。amount 为本次金额变动（分）：reserve 预扣、
// settle delta、reverse 退款(-pre)；usage 为 0（字段由调用方覆盖）。
func newBillingEvent(row *Bill, amount int64, phase string) *BillingEvent {
	return &BillingEvent{
		EventID:          fmt.Sprintf("%s:%s", row.RequestID, phase),
		TenantID:         row.TenantID,
		RequestID:        row.RequestID,
		Phase:            phase,
		AmountCents:      amount,
		Model:            row.Model,
		Funding:          row.Funding,
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		BillID:           row.ID,
		CreatedAt:        time.Now(),
	}
}

// insertOutbox 在事务内落 outbox 行。撞 uk_event_key（并发/重试另一事务已落同一事件）
// → 吞掉 no-op：同一事件只入一行，重复投递由唯一索引收敛。
func insertOutbox(tx *gorm.DB, ev *BillingEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	err = tx.Create(&OutboxEvent{
		Topic:     TopicBillingEvents,
		EventKey:  ev.EventID,
		Payload:   string(payload),
		CreatedAt: time.Now(),
	}).Error
	if err != nil && isDupKey(err) {
		return nil
	}
	return err
}

// PollOutbox 取一批待投递事件：sent_at IS NULL 且 attempts < maxAttempts（按 id 升序，
// 先入先投）。relay 单写者轮询，无需加锁——撞 uk_event_key 由唯一索引兜底。
func (s *Store) PollOutbox(ctx context.Context, limit int, maxAttempts int) ([]OutboxEvent, error) {
	var rows []OutboxEvent
	err := s.db.WithContext(ctx).
		Where("sent_at IS NULL AND attempts < ?", maxAttempts).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// MarkOutboxSent 投递成功：sent_at 置位（relay 不再轮询该行）。
func (s *Store) MarkOutboxSent(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&OutboxEvent{}).
		Where("id = ?", id).
		Update("sent_at", time.Now()).Error
}

// BumpOutboxAttempt 投递失败：attempts+1 并记录错误，等下一轮重试。
func (s *Store) BumpOutboxAttempt(ctx context.Context, id uint64, errMsg string) error {
	return s.db.WithContext(ctx).Model(&OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": errMsg,
		}).Error
}

// MarkOutboxDLQ 投递超限 → 改投 DLQ：topic 改为 billing.events.dlq 且 sent_at 置位
// （relay 不再轮询；DLQ 消费端按 source=dlq 落 audit 明细）。
func (s *Store) MarkOutboxDLQ(ctx context.Context, id uint64, errMsg string) error {
	return s.db.WithContext(ctx).Model(&OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"topic":      TopicBillingDLQ,
			"sent_at":    time.Now(),
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": errMsg,
		}).Error
}
