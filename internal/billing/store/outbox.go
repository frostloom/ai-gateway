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
	"strconv"
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
	// ApplyLedger 为真时，消费者把这条消息写成账单，而不只是审计。
	// 请求路径只在 Redis 扣款成功后投递；同一条消息重投靠账单唯一键跳过，余额只加一次。
	ApplyLedger   bool    `json:"apply_ledger,omitempty"`
	APIKeyID      uint64  `json:"api_key_id,omitempty"`
	BillStatus    string  `json:"bill_status,omitempty"`
	PreQuota      int64   `json:"pre_quota,omitempty"`
	ActualQuota   *int64  `json:"actual_quota,omitempty"`
	DeltaQuota    *int64  `json:"delta_quota,omitempty"`
	ProviderID    *uint64 `json:"provider_id,omitempty"`
	InPriceCent   *int64  `json:"in_price_cent,omitempty"`
	OutPriceCent  *int64  `json:"out_price_cent,omitempty"`
	LedgerDelta   int64   `json:"ledger_delta,omitempty"`
	UsageReported bool    `json:"usage_reported,omitempty"`
}

// KafkaMessageKey 让同一用户的明细进同一分区。没有租户时退回请求号，再退回事件号。
func KafkaMessageKey(ev *BillingEvent) string {
	if ev == nil {
		return ""
	}
	if ev.TenantID != 0 {
		return strconv.FormatUint(ev.TenantID, 10)
	}
	if ev.RequestID != "" {
		return ev.RequestID
	}
	return ev.EventID
}

// KafkaMessageKeyFromPayload 从已落库的事件正文取分区键。正文没有租户时用 fallback。
func KafkaMessageKeyFromPayload(payload []byte, fallback string) string {
	var ev BillingEvent
	if len(payload) == 0 || json.Unmarshal(payload, &ev) != nil || ev.TenantID == 0 {
		return fallback
	}
	return strconv.FormatUint(ev.TenantID, 10)
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

// insertOutbox 在事务内落 outbox 行。唯一键冲突不能证明已有事件与本次账务一致；
// 返回错误使调用方事务回滚，正常请求重放由各账务操作的状态检查处理。
func insertOutbox(tx *gorm.DB, ev *BillingEvent) error {
	return insertOutboxBatch(tx, []*BillingEvent{ev})
}

func insertOutboxBatch(tx *gorm.DB, events []*BillingEvent) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]OutboxEvent, len(events))
	now := time.Now()
	for i, ev := range events {
		payload, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		rows[i] = OutboxEvent{Topic: TopicBillingEvents, EventKey: ev.EventID, Payload: string(payload), CreatedAt: now}
	}
	return tx.CreateInBatches(&rows, 100).Error
}

// PollOutbox 取一批未标记发送的事件（按 id 升序）。即使 attempts 已达上限也必须
// 返回：并发 relay 或旧版本故障可能让未发送的行跨过上限，relay 会再尝试主 topic/DLQ。
// 兼容现有接口，maxAttempts 由 relay 使用，而不作为筛选条件。
func (s *Store) PollOutbox(ctx context.Context, limit int, _ int) ([]OutboxEvent, error) {
	var rows []OutboxEvent
	err := s.db.WithContext(ctx).
		Where("sent_at IS NULL").
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
