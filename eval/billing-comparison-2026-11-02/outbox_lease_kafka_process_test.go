package comparison

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/consumer"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	appkafka "github.com/frostloom/ai-gateway/internal/pkg/kafka"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const leaseKafkaID = "isolated-lease-crash-reserve"

func leaseKafkaPayload() []byte {
	b, _ := json.Marshal(store.BillingEvent{EventID: leaseKafkaID, RequestID: leaseKafkaID, Phase: "reserve", TenantID: 1, Funding: "balance", AmountCents: 17})
	return b
}
func leaseKafkaDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ISOLATED_MYSQL_DSN")
	if dsn == "" || os.Getenv("ISOLATED_KAFKA_ADDR") == "" {
		t.Skip("disposable Kafka and MySQL required")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
func TestOutboxLeaseKafkaProcessChild(t *testing.T) {
	if os.Getenv("LEASE_KAFKA_CHILD") != "1" {
		t.Skip("helper only")
	}
	db := leaseKafkaDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	row, err := claimOutboxGeneration(ctx, db, "same")
	if err != nil || row.ID != 1 || row.Generation != 1 {
		t.Fatalf("claim=%+v err=%v", row, err)
	}
	p := appkafka.NewProducer([]string{os.Getenv("ISOLATED_KAFKA_ADDR")}, "offset-repro", logger.New("error"))
	defer p.Close()
	if err := p.Publish(ctx, leaseKafkaID, leaseKafkaPayload()); err != nil {
		t.Fatal(err)
	}
	fmt.Println("LEASE_KAFKA_PUBLISH_ACKNOWLEDGED")
	time.Sleep(time.Minute)
}
func TestOutboxLeaseKafkaKilledAfterPublish(t *testing.T) {
	db := leaseKafkaDB(t)
	for _, q := range []string{"CREATE TABLE IF NOT EXISTS eval_outbox_lease_v2 (id BIGINT UNSIGNED PRIMARY KEY, generation BIGINT NOT NULL DEFAULT 0, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6)) ENGINE=InnoDB", "DELETE FROM eval_outbox_lease_v2", "INSERT INTO eval_outbox_lease_v2(id) VALUES(1)"} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&store.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutboxLeaseKafkaProcessChild$")
	cmd.Env = append(os.Environ(), "LEASE_KAFKA_CHILD=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if strings.TrimSpace(s.Text()) == "LEASE_KAFKA_PUBLISH_ACKNOWLEDGED" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("child exited before publish acknowledgement")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child timed out")
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	err = cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("wait=%v", err)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("wait=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	var stored leaseGenerationRow
	if err := db.Table("eval_outbox_lease_v2").Where("id=1").Take(&stored).Error; err != nil || stored.Generation != 1 || stored.SentAt != nil {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if row, err := claimOutboxGeneration(ctx, db, "same"); err != nil || row.ID != 0 {
		t.Fatalf("before expiry=%+v err=%v", row, err)
	}
	if err := db.Exec("UPDATE eval_outbox_lease_v2 SET lease_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	row, err := claimOutboxGeneration(ctx, db, "same")
	if err != nil || row.ID != 1 || row.Generation != 2 {
		t.Fatalf("replacement=%+v err=%v", row, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", 1); err != nil || ok {
		t.Fatalf("stale=%v err=%v", ok, err)
	}
	log := logger.New("error")
	addr := os.Getenv("ISOLATED_KAFKA_ADDR")
	prod := appkafka.NewProducer([]string{addr}, "offset-repro", log)
	defer prod.Close()
	kc := appkafka.NewConsumer([]string{addr}, "lease-crash-group", []string{"offset-repro"})
	c := consumer.New(store.NewStore(db, nil), kc, consumer.Config{}, log)
	done := make(chan struct{})
	consumeCtx, stop := context.WithCancel(ctx)
	go func() { defer close(done); _ = c.Run(consumeCtx) }()
	defer func() { stop(); <-done; _ = kc.Close() }()
	if err := prod.Publish(ctx, leaseKafkaID, leaseKafkaPayload()); err != nil {
		t.Fatal(err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", 2); err != nil || !ok {
		t.Fatalf("mark=%v err=%v", ok, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", 2); err != nil || ok {
		t.Fatalf("repeat mark=%v err=%v", ok, err)
	}
	for ctx.Err() == nil && c.Stats().Consumed < 2 {
		time.Sleep(100 * time.Millisecond)
	}
	stats := c.Stats()
	if stats.Consumed != 2 || stats.Errors != 0 || stats.DecodeErrs != 0 {
		t.Fatalf("consumer=%+v", stats)
	}
	var count, mismatch int64
	if err := db.Model(&store.AuditEvent{}).Where("event_id=?", leaseKafkaID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM audit_events WHERE event_id=? AND NOT (payload <=> CAST(? AS JSON))", leaseKafkaID, string(leaseKafkaPayload())).Scan(&mismatch).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 || mismatch != 0 {
		t.Fatalf("audit=%d mismatch=%d", count, mismatch)
	}
	t.Logf("SIGKILL after Kafka ack; manual expiry; generation 2 fenced generation 1; consumed=%d audit=%d mismatch=%d", stats.Consumed, count, mismatch)
}
