package comparison

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type experimentalQuarantine struct {
	ID        uint64 `gorm:"primaryKey"`
	Topic     string `gorm:"size:120;not null;uniqueIndex:uq_eval_delivery,priority:1"`
	Partition int32  `gorm:"not null;uniqueIndex:uq_eval_delivery,priority:2"`
	Offset    int64  `gorm:"not null;uniqueIndex:uq_eval_delivery,priority:3"`
	EventID   string `gorm:"size:128;not null"`
	Payload   []byte `gorm:"type:longblob;not null"`
}

func (experimentalQuarantine) TableName() string { return "eval_audit_quarantine" }

type experimentalDelivery struct {
	ID          uint64 `gorm:"primaryKey"`
	Topic       string `gorm:"size:120;not null;uniqueIndex:uq_eval_audit_delivery,priority:1"`
	Partition   int32  `gorm:"not null;uniqueIndex:uq_eval_audit_delivery,priority:2"`
	Offset      int64  `gorm:"not null;uniqueIndex:uq_eval_audit_delivery,priority:3"`
	Fingerprint string `gorm:"size:64;not null"`
	Outcome     string `gorm:"size:16;not null"`
}

func (experimentalDelivery) TableName() string { return "eval_audit_delivery" }
func evalDuplicate(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
func evalJSON(payload string) (any, error) {
	d := json.NewDecoder(bytes.NewBufferString(payload))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing JSON")
	}
	return v, nil
}
func evalEqualAudit(a, b store.AuditEvent) bool {
	x, e1 := evalJSON(a.Payload)
	y, e2 := evalJSON(b.Payload)
	return e1 == nil && e2 == nil && a.EventID == b.EventID && a.TenantID == b.TenantID && a.RequestID == b.RequestID && a.Phase == b.Phase && a.AmountCents == b.AmountCents && a.Model == b.Model && a.Funding == b.Funding && reflect.DeepEqual(a.PromptTokens, b.PromptTokens) && reflect.DeepEqual(a.CompletionTokens, b.CompletionTokens) && reflect.DeepEqual(x, y)
}

// Test-only prototype: delivery coordinate, evidence and audit commit together.
func quarantineAudit(ctx context.Context, db *gorm.DB, event store.AuditEvent, topic string, partition int32, offset int64) (string, error) {
	identity, err := json.Marshal([]any{event.EventID, event.TenantID, event.RequestID, event.Phase, event.AmountCents, event.Model, event.Funding, event.PromptTokens, event.CompletionTokens, event.Payload})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(identity)
	delivery := experimentalDelivery{Topic: topic, Partition: partition, Offset: offset, Fingerprint: hex.EncodeToString(sum[:]), Outcome: "pending"}
	outcome := ""
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&delivery).Error; err != nil {
			if !evalDuplicate(err) {
				return err
			}
			var prior experimentalDelivery
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"topic": topic, "partition": partition, "offset": offset}).First(&prior).Error; e != nil {
				return e
			}
			if prior.Topic != topic || prior.Fingerprint != delivery.Fingerprint || prior.Outcome == "pending" {
				return errors.New("delivery coordinate conflict")
			}
			outcome = prior.Outcome
			return nil
		}
		event.ID = 0
		if err := tx.Create(&event).Error; err == nil {
			outcome = "accepted"
		} else {
			if !evalDuplicate(err) {
				return err
			}
			var prior store.AuditEvent
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("event_id=?", event.EventID).First(&prior).Error; err != nil {
				return err
			}
			if evalEqualAudit(prior, event) {
				outcome = "duplicate"
			} else {
				q := experimentalQuarantine{Topic: topic, Partition: partition, Offset: offset, EventID: event.EventID, Payload: []byte(event.Payload)}
				if err := tx.Create(&q).Error; err != nil {
					return err
				}
				outcome = "quarantined"
			}
		}
		return tx.Model(&delivery).Update("outcome", outcome).Error
	})
	if err != nil {
		return "", err
	}
	return outcome, nil
}

func TestAuditQuarantineTransaction(t *testing.T) {
	if os.Getenv("TEST_AUDIT_QUARANTINE") != "1" {
		t.Skip("isolated only")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.AuditEvent{}, &experimentalQuarantine{}, &experimentalDelivery{}, &experimentalAuditGuard{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("quarantine-%d", time.Now().UnixNano())
	topic := "synthetic-" + id
	first := store.AuditEvent{EventID: id, TenantID: 1, RequestID: id, Phase: "settle", AmountCents: 40, Model: "mock", Funding: "balance", Payload: `{"amount_cents":40}`, Source: "kafka", ConsumedAt: time.Now()}
	if got, err := guardedQuarantineAudit(ctx, db, first, topic, 0, 1); err != nil || got != "accepted" {
		t.Fatalf("first=%q err=%v", got, err)
	}
	if got, err := guardedQuarantineAudit(ctx, db, first, topic, 0, 2); err != nil || got != "duplicate" {
		t.Fatalf("duplicate=%q err=%v", got, err)
	}
	changed := first
	changed.AmountCents = 90
	changed.Payload = `{"amount_cents":90}`
	// A failed quarantine insert must not report success or overwrite the audit.
	cb := "eval:quarantine-failure-" + id
	if err := db.Callback().Create().Before("gorm:create").Register(cb, func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_audit_quarantine" {
			tx.AddError(errors.New("synthetic quarantine write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	got, err := guardedQuarantineAudit(ctx, db, changed, topic, 0, 3)
	db.Callback().Create().Remove(cb)
	if err == nil {
		t.Fatalf("failed quarantine acknowledged: %q", got)
	}
	var count int64
	if err := db.Model(&experimentalQuarantine{}).Where("topic=?", topic).Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	for i := 0; i < 2; i++ {
		if got, err := guardedQuarantineAudit(ctx, db, changed, topic, 0, 3); err != nil || got != "quarantined" {
			t.Fatal(got, err)
		}
	}
	different := changed
	different.AmountCents = 91
	different.Payload = `{"amount_cents":91}`
	if got, err := guardedQuarantineAudit(ctx, db, different, topic, 0, 3); err == nil {
		t.Fatalf("same Kafka coordinate different bytes accepted: %q", got)
	}
	var row store.AuditEvent
	if err := db.Where("event_id=?", id).First(&row).Error; err != nil || row.AmountCents != 40 {
		t.Fatal(row.AmountCents, err)
	}
	var held []experimentalQuarantine
	if err := db.Where("topic=?", topic).Find(&held).Error; err != nil || len(held) != 1 || string(held[0].Payload) != changed.Payload {
		t.Fatalf("held=%+v err=%v", held, err)
	}
	t.Log("initial=accepted replay=duplicate injected_write=error conflict=quarantined repeat=one coordinate_mutation=error original_unchanged=true")
}
