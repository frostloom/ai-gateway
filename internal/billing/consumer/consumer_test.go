package consumer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	appkafka "github.com/frostloom/ai-gateway/internal/pkg/kafka"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
)

// 集成测试（真 MySQL + 真 Kafka，skip-if-unreachable）。
// 用独立测试库 ai_gateway_consumer_test（与 store 包的 ai_gateway_test 隔离，避免包级并行互踩）。
const (
	testMySQLHost = "root:root@tcp(127.0.0.1:3307)/"
	testMySQLDB   = "ai_gateway_consumer_test"
	testKafkaAddr = "127.0.0.1:29092" // 宿主 → PLAINTEXT_HOST（.env 的 KAFKA_BROKERS）
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	admin, err := sql.Open("mysql", testMySQLHost)
	if err != nil {
		t.Skipf("mysql not reachable: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + testMySQLDB + " CHARACTER SET utf8mb4"); err != nil {
		t.Skipf("create test db: %v", err)
	}
	admin.Close()
	dsn := testMySQLHost + testMySQLDB + "?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Skipf("open mysql: %v", err)
	}
	st := store.NewStore(db, nil)
	if err := st.AutoMigrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, tbl := range []string{"audit_events", "outbox_events"} {
		if err := db.Exec("DELETE FROM " + tbl).Error; err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}
	return db
}

// ensureTopics 幂等建 topic（kafka 不可达 → skip）。
func ensureTopics(t *testing.T, topics ...string) {
	t.Helper()
	client := &kafka.Client{Addr: kafka.TCP(testKafkaAddr), Timeout: 3 * time.Second}
	cfg := make([]kafka.TopicConfig, 0, len(topics))
	for _, tp := range topics {
		cfg = append(cfg, kafka.TopicConfig{Topic: tp, NumPartitions: 1, ReplicationFactor: 1})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: cfg})
	if err != nil {
		t.Skipf("kafka not reachable at %s: %v", testKafkaAddr, err)
	}
	for _, tp := range topics {
		if e := resp.Errors[tp]; e != nil && !errors.Is(e, kafka.TopicAlreadyExists) {
			t.Fatalf("create topic %s: %v", tp, e)
		}
	}
}

func waitAuditCount(t *testing.T, db *gorm.DB, requestID string, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var n int64
		if err := db.Model(&store.AuditEvent{}).Where("request_id = ?", requestID).Count(&n).Error; err != nil {
			t.Fatalf("count audit: %v", err)
		}
		if n == want {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	var n int64
	db.Model(&store.AuditEvent{}).Where("request_id = ?", requestID).Count(&n)
	t.Fatalf("audit rows = %d, want %d（超时未达成；见 logs/event-consumer.log）", n, want)
}

func waitConsumerProcessed(t *testing.T, c *EventConsumer, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.Stats().Consumed >= want {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("consumer processed = %d, want >= %d", c.Stats().Consumed, want)
}

// E2E：真实 Kafka 生产 → 消费 → audit_events 落行；重投同 event_key → 消费端幂等吸收，
// 总数仍 3（uk_event_id 唯一索引 no-op）。
func TestConsumerKafkaE2E(t *testing.T) {
	db := openTestDB(t)
	runID := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	topic, dlq := "billing.events."+runID, "billing.events.dlq."+runID
	ensureTopics(t, topic, dlq)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client := &kafka.Client{Addr: kafka.TCP(testKafkaAddr), Timeout: 3 * time.Second}
		resp, err := client.DeleteTopics(ctx, &kafka.DeleteTopicsRequest{Topics: []string{topic, dlq}})
		if err != nil {
			t.Errorf("delete isolated topics: %v", err)
			return
		}
		for name, err := range resp.Errors {
			if err != nil {
				t.Errorf("delete topic %s: %v", name, err)
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := store.NewStore(db, nil)
	prod := appkafka.NewProducer([]string{testKafkaAddr}, topic, logger.New("error"))
	defer prod.Close()

	// 独立消费组：每次测试全新偏移，从最旧开始消费。
	group := "test-" + runID
	kc := appkafka.NewConsumer([]string{testKafkaAddr}, group, []string{topic, dlq})
	defer kc.Close()
	c := New(st, kc, Config{DLQTopic: dlq}, logger.New("error"))
	stopped := make(chan struct{})
	go func() { defer close(stopped); c.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("consumer did not stop after cancellation")
		}
	}()

	publish := func(phase string) {
		ev := store.BillingEvent{
			EventID: runID + ":" + phase, TenantID: 7, RequestID: runID,
			Phase: phase, AmountCents: 100, Model: "mock-model", Funding: store.FundingBalance,
			CreatedAt: time.Now(),
		}
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := prod.Publish(ctx, ev.EventID, b); err != nil {
			t.Fatalf("publish %s: %v", phase, err)
		}
	}

	for _, phase := range []string{store.PhaseReserve, store.PhaseUsage, store.PhaseSettle} {
		publish(phase)
	}
	waitAuditCount(t, db, runID, 3, 20*time.Second)

	// 重投同 event_key（reserve）：消费端应处理 4 条，但落库仍 3 行。
	publish(store.PhaseReserve)
	waitConsumerProcessed(t, c, 4, 20*time.Second)
	var n int64
	if err := db.Model(&store.AuditEvent{}).Where("request_id = ?", runID).Count(&n).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 3 {
		t.Fatalf("audit rows after replay = %d, want 3（幂等吸收失败）", n)
	}

	// 字段抽查：reserve 行 source=kafka、带上游 partition/offset、payload 自包含。
	var row store.AuditEvent
	if err := db.Where("event_id = ?", runID+":reserve").First(&row).Error; err != nil {
		t.Fatalf("get audit row: %v", err)
	}
	if row.Source != "kafka" || row.KafkaPartition < 0 || row.KafkaOffset < 0 || row.Payload == "" {
		t.Fatalf("audit row 字段异常: %+v", row)
	}
}
