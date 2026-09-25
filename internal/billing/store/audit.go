// 计费审计明细表（M10）：event-consumer 写，Kafka billing.events 流的下游物化视图。
//
// 幂等设计：uk_event_id 唯一索引 = 幂等锚。Kafka 是 at-least-once，同一事件可能被
// 重投/多副本消费；UpsertAudit 撞键时 UPDATE 只把 id 赋回自己（no-op），落库恰一行，
// 等价 exactly-once。schema.sql 与 gorm AutoMigrate 双写保持。
package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AuditEvent 审计明细行（一事件一行；字段取自 BillingEvent 载荷，自包含）。
type AuditEvent struct {
	ID               uint64    `gorm:"primaryKey"`
	EventID          string    `gorm:"size:128;uniqueIndex:uk_event_id;not null"` // = outbox.event_key
	TenantID         uint64    `gorm:"not null"`
	RequestID        string    `gorm:"size:64;not null"`
	Phase            string    `gorm:"size:16;not null"`
	AmountCents      int64     `gorm:"not null"`
	Model            string    `gorm:"size:64;not null;default:''"`
	Funding          string    `gorm:"size:16;not null;default:balance"`
	PromptTokens     *int64    `gorm:"default:null"`
	CompletionTokens *int64    `gorm:"default:null"`
	Payload          string    `gorm:"type:json"`
	Source           string    `gorm:"size:16;not null;default:kafka"` // kafka|dlq
	KafkaPartition   int32     `gorm:"not null;default:0"`
	KafkaOffset      int64     `gorm:"not null;default:0"`
	ConsumedAt       time.Time `gorm:"not null"`
}

// UpsertAudit 幂等写审计明细：撞 uk_event_id（重投/多副本）→ UPDATE id=id（MySQL no-op）。
func (s *Store) UpsertAudit(ctx context.Context, ev *AuditEvent) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "event_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"id": gorm.Expr("id"),
		}),
	}).Create(ev).Error
}
