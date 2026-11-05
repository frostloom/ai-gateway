package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Test-only direct Kafka reader: uses synchronous commits, not production consumer.Run.
func TestAuditQuarantineKafkaReplay(t *testing.T) {
	dsn, addr := os.Getenv("ISOLATED_MYSQL_DSN"), os.Getenv("ISOLATED_KAFKA_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("requires disposable MySQL and Kafka")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&store.AuditEvent{}, &experimentalQuarantine{}, &experimentalDelivery{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	topic := "audit-quarantine-test"
	group := fmt.Sprintf("eval-%d", time.Now().UnixNano())
	id := fmt.Sprintf("event-%d", time.Now().UnixNano())
	first := store.BillingEvent{EventID: id, TenantID: 1, RequestID: id, Phase: store.PhaseSettle, AmountCents: 40, Model: "mock", Funding: store.FundingBalance, CreatedAt: time.Now().UTC()}
	conflict := first
	conflict.AmountCents = 90
	after := first
	after.EventID = id + "-after"
	after.RequestID = id + "-after"
	writer := &kafka.Writer{Addr: kafka.TCP(addr), Topic: topic, RequiredAcks: kafka.RequireAll, BatchTimeout: time.Millisecond}
	defer writer.Close()
	for _, e := range []store.BillingEvent{first, conflict, after} {
		v, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteMessages(ctx, kafka.Message{Key: []byte(e.EventID), Value: v}); err != nil {
			t.Fatal(err)
		}
	}
	reader := func() *kafka.Reader {
		return kafka.NewReader(kafka.ReaderConfig{Brokers: []string{addr}, GroupID: group, Topic: topic, MinBytes: 1, MaxBytes: 1 << 20, StartOffset: kafka.FirstOffset, CommitInterval: 0})
	}
	apply := func(m kafka.Message) (string, error) {
		var e store.BillingEvent
		if err := json.Unmarshal(m.Value, &e); err != nil {
			return "", err
		}
		a := store.AuditEvent{EventID: e.EventID, TenantID: e.TenantID, RequestID: e.RequestID, Phase: e.Phase, AmountCents: e.AmountCents, Model: e.Model, Funding: e.Funding, Payload: string(m.Value), Source: "kafka", KafkaPartition: int32(m.Partition), KafkaOffset: m.Offset, ConsumedAt: time.Now()}
		return quarantineAudit(ctx, db, a, m.Topic, int32(m.Partition), m.Offset)
	}
	fetch := func(r *kafka.Reader) kafka.Message {
		t.Helper()
		m, err := r.FetchMessage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	r1 := reader()
	m1 := fetch(r1)
	if got, err := apply(m1); err != nil || got != "accepted" {
		t.Fatal(got, err)
	}
	if err := r1.CommitMessages(ctx, m1); err != nil {
		t.Fatal(err)
	}
	m2 := fetch(r1)
	if err := db.Callback().Create().Before("gorm:create").Register("eval:kafka-quarantine-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_audit_quarantine" {
			tx.AddError(errors.New("synthetic quarantine write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, failed := apply(m2)
	db.Callback().Create().Remove("eval:kafka-quarantine-fail")
	if failed == nil {
		t.Fatal("quarantine failure unexpectedly acknowledged")
	}
	var beforeHeld, beforeDeliveries int64
	if err := db.Model(&experimentalQuarantine{}).Count(&beforeHeld).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experimentalDelivery{}).Count(&beforeDeliveries).Error; err != nil {
		t.Fatal(err)
	}
	if beforeHeld != 0 || beforeDeliveries != 1 {
		t.Fatalf("failed transaction held=%d deliveries=%d", beforeHeld, beforeDeliveries)
	}
	if got, err := apply(m2); err != nil || got != "quarantined" {
		t.Fatal(got, err)
	}
	// Stop after durable quarantine and deliberately before committing offset 2.
	if err := r1.Close(); err != nil {
		t.Fatal(err)
	}
	r2 := reader()
	defer r2.Close()
	again := fetch(r2)
	if again.Offset != m2.Offset || again.Partition != m2.Partition {
		t.Fatalf("replay got=%d/%d expected=%d/%d", again.Partition, again.Offset, m2.Partition, m2.Offset)
	}
	if got, err := apply(again); err != nil || got != "quarantined" {
		t.Fatal(got, err)
	}
	if err := r2.CommitMessages(ctx, again); err != nil {
		t.Fatal(err)
	}
	m3 := fetch(r2)
	if got, err := apply(m3); err != nil || got != "accepted" {
		t.Fatal(got, err)
	}
	if err := r2.CommitMessages(ctx, m3); err != nil {
		t.Fatal(err)
	}
	var auditRows, held, deliveries int64
	if err := db.Model(&store.AuditEvent{}).Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experimentalQuarantine{}).Count(&held).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experimentalDelivery{}).Count(&deliveries).Error; err != nil {
		t.Fatal(err)
	}
	var original store.AuditEvent
	if err := db.Where("event_id=?", id).First(&original).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 2 || held != 1 || deliveries != 3 || original.AmountCents != 40 {
		t.Fatalf("audit=%d held=%d deliveries=%d amount=%d", auditRows, held, deliveries, original.AmountCents)
	}
	t.Logf("offsets=%d,%d,%d failed_write=rolled_back replay_offset=%d audit=%d quarantined=%d deliveries=%d original_amount=%d", m1.Offset, m2.Offset, m3.Offset, again.Offset, auditRows, held, deliveries, original.AmountCents)
}
