package comparison

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type experimentalAuditGuard struct {
	ID string `gorm:"primaryKey;size:64"`
}

func (experimentalAuditGuard) TableName() string { return "eval_audit_event_guard" }

// Test-only serialization before any delivery or audit row is modified.
func guardedQuarantineAudit(ctx context.Context, db *gorm.DB, ev store.AuditEvent, topic string, partition int32, offset int64) (out string, err error) {
	if os.Getenv("TEST_AUDIT_LOCK_FIRST") != "1" {
		return quarantineAudit(ctx, db, ev, topic, partition, offset)
	}
	key := sha256.Sum256([]byte(ev.EventID))
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&experimentalAuditGuard{ID: fmt.Sprintf("%x", key)}).Error; err != nil {
			return err
		}
		var e error
		out, e = quarantineAudit(ctx, tx, ev, topic, partition, offset)
		return e
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

func TestAuditQuarantineRaceAndCommitAck(t *testing.T) {
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
	base := fmt.Sprintf("q-race-%d", time.Now().UnixNano())
	topic := "t-" + base
	a := store.AuditEvent{EventID: base, TenantID: 1, RequestID: base, Phase: "settle", AmountCents: 40, Model: "mock", Funding: "balance", Payload: `{"amount_cents":40}`, Source: "kafka", ConsumedAt: time.Now()}
	b := a
	b.AmountCents = 90
	b.Payload = `{"amount_cents":90}`
	const n = 12
	type result struct {
		out string
		err error
	}
	results := make(chan result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var retries atomic.Int64
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ev := a
			if i >= n/2 {
				ev = b
			}
			for attempt := 0; attempt < 20; attempt++ {
				out, err := guardedQuarantineAudit(ctx, db, ev, topic, 0, int64(i))
				var mysqlErr *mysqldriver.MySQLError
				if os.Getenv("TEST_AUDIT_LOCK_FIRST") == "1" || !errors.As(err, &mysqlErr) || mysqlErr.Number != 1213 || attempt == 19 {
					results <- result{out, err}
					return
				}
				retries.Add(1)
				time.Sleep(time.Duration((i%5+1)*(attempt+1)) * time.Millisecond)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	counts := map[string]int{}
	for res := range results {
		if res.err != nil {
			t.Fatal(res.err)
		}
		counts[res.out]++
	}
	if counts["accepted"] != 1 || counts["duplicate"] != 5 || counts["quarantined"] != 6 {
		t.Fatalf("counts=%v", counts)
	}
	var original store.AuditEvent
	if err := db.Where("event_id=?", base).First(&original).Error; err != nil {
		t.Fatal(err)
	}
	var held, delivery int64
	if err := db.Model(&experimentalQuarantine{}).Where("topic=?", topic).Count(&held).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experimentalDelivery{}).Where("topic=?", topic).Count(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if held != 6 || delivery != 12 || (original.AmountCents != 40 && original.AmountCents != 90) {
		t.Fatalf("held=%d delivery=%d original=%d", held, delivery, original.AmountCents)
	}
	for i, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		id := fmt.Sprintf("%s-ack-%d", base, i)
		ev := a
		ev.EventID = id
		ev.RequestID = id
		if got, err := guardedQuarantineAudit(ctx, db, ev, topic, 1, int64(i*2)); err != nil || got != "accepted" {
			t.Fatal(got, err)
		}
		ev.AmountCents = 90
		ev.Payload = `{"amount_cents":90}`
		broken := recoveryDBWithCommitError(t, injected)
		if _, err := guardedQuarantineAudit(ctx, broken, ev, topic, 1, int64(i*2+1)); !errors.Is(err, injected) {
			t.Fatalf("ack expected=%v got=%v", injected, err)
		}
		got, err := guardedQuarantineAudit(ctx, db, ev, topic, 1, int64(i*2+1))
		if err != nil || got != "quarantined" {
			t.Fatalf("retry=%q err=%v", got, err)
		}
		var c int64
		if err := db.Model(&experimentalQuarantine{}).Where("event_id=?", id).Count(&c).Error; err != nil || c != 1 {
			t.Fatalf("held=%d err=%v", c, err)
		}
	}
	t.Logf("initial_race=%v deadlock_retries=%d original=%d held=%d deliveries=%d commit_ack_errors=2 retries_single_evidence=true", counts, retries.Load(), original.AmountCents, held, delivery)
}

func TestAuditGuardCaseEquivalentRace(t *testing.T) {
	if os.Getenv("TEST_AUDIT_QUARANTINE") != "1" || os.Getenv("TEST_AUDIT_LOCK_FIRST") != "1" {
		t.Skip("guarded isolated test")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.AuditEvent{}, &experimentalQuarantine{}, &experimentalDelivery{}, &experimentalAuditGuard{}); err != nil {
		t.Fatal(err)
	}
	var coll string
	if err := db.Raw("SELECT COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='audit_events' AND COLUMN_NAME='event_id'").Scan(&coll).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(coll, "_ci") {
		t.Skipf("requires case-insensitive event_id collation, got %s", coll)
	}
	ctx := context.Background()
	base := fmt.Sprintf("qcase-%d", time.Now().UnixNano())
	topic := "t-" + base
	const n = 12
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id := base + "A"
			if i%2 == 1 {
				id = base + "a"
			}
			ev := store.AuditEvent{EventID: id, TenantID: 1, RequestID: base, Phase: "settle", AmountCents: 40, Model: "mock", Funding: "balance", Payload: `{"amount_cents":40}`, Source: "kafka", ConsumedAt: time.Now()}
			_, err := guardedQuarantineAudit(ctx, db, ev, topic, 0, int64(i))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("case-equivalent concurrent IDs failed: %v", err)
		}
	}
	var audits, held, deliveries int64
	if err := db.Model(&store.AuditEvent{}).Where("event_id IN ?", []string{base + "A", base + "a"}).Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experimentalQuarantine{}).Where("topic=?", topic).Count(&held).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experimentalDelivery{}).Where("topic=?", topic).Count(&deliveries).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 1 || held != 6 || deliveries != 12 {
		t.Fatalf("audit=%d held=%d delivery=%d", audits, held, deliveries)
	}
	t.Logf("collation=%s case_equivalent_guard_race=passed audits=%d held=%d deliveries=%d", coll, audits, held, deliveries)
}
