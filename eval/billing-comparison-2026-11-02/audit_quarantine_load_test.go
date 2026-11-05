package comparison

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm/logger"
)

func TestAuditQuarantineLoad(t *testing.T) {
	if os.Getenv("TEST_AUDIT_LOAD") != "1" {
		t.Skip("isolated only")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.AuditEvent{}, &experimentalQuarantine{}, &experimentalDelivery{}, &experimentalAuditGuard{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetMaxIdleConns(16)
	readCounter := func(name string) uint64 {
		t.Helper()
		var raw string
		if err := sqlDB.QueryRow("SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME=?", name).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	const total = 120
	const workers = 12
	for _, unique := range []bool{false, true} {
		for round := 1; round <= 3; round++ {
			prefix := fmt.Sprintf("qload-%t-%d-%d", unique, round, time.Now().UnixNano())
			topic := "t-" + prefix
			type item struct {
				duration time.Duration
				out      string
				err      error
			}
			results := make(chan item, total)
			jobs := make(chan int)
			var wg sync.WaitGroup
			var retries atomic.Int64
			before := sqlDB.Stats()
			waitBefore := readCounter("Innodb_row_lock_waits")
			waitTimeBefore := readCounter("Innodb_row_lock_time")
			start := time.Now()
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := range jobs {
						id := prefix
						if unique {
							id = fmt.Sprintf("%s-%d", prefix, j)
						}
						ev := store.AuditEvent{EventID: id, TenantID: 1, RequestID: id, Phase: "settle", AmountCents: 40, Model: "mock", Funding: "balance", Payload: `{"amount_cents":40}`, Source: "kafka", ConsumedAt: time.Now()}
						began := time.Now()
						var out string
						var err error
						for attempt := 0; attempt < 20; attempt++ {
							out, err = guardedQuarantineAudit(context.Background(), db, ev, topic, 0, int64(j))
							var me *mysqldriver.MySQLError
							if !errors.As(err, &me) || me.Number != 1213 || attempt == 19 {
								break
							}
							retries.Add(1)
							time.Sleep(time.Duration((j%5+1)*(attempt+1)) * time.Millisecond)
						}
						results <- item{time.Since(began), out, err}
					}
				}()
			}
			for j := 0; j < total; j++ {
				jobs <- j
			}
			close(jobs)
			wg.Wait()
			elapsed := time.Since(start)
			close(results)
			after := sqlDB.Stats()
			waitAfter := readCounter("Innodb_row_lock_waits")
			waitTimeAfter := readCounter("Innodb_row_lock_time")
			var lats []float64
			counts := map[string]int{}
			failures := 0
			sum := 0.0
			for x := range results {
				ms := float64(x.duration.Microseconds()) / 1000
				lats = append(lats, ms)
				sum += ms
				if x.err != nil {
					failures++
				} else {
					counts[x.out]++
				}
			}
			sort.Float64s(lats)
			var audit, delivery, held int64
			if err := db.Model(&store.AuditEvent{}).Where("event_id LIKE ?", prefix+"%").Count(&audit).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&experimentalDelivery{}).Where("topic=?", topic).Count(&delivery).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&experimentalQuarantine{}).Where("topic=?", topic).Count(&held).Error; err != nil {
				t.Fatal(err)
			}
			wantAudit := int64(1)
			if unique {
				wantAudit = total
			}
			t.Logf("guard=%t unique=%t round=%d operations=%d workers=%d throughput=%.2f mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f errors=%d deadlock_retries=%d outcomes=%v audits=%d deliveries=%d quarantined=%d pool_wait=%d pool_wait_ms=%.3f mysql_waits=%d mysql_wait_ms=%d", os.Getenv("TEST_AUDIT_LOCK_FIRST") == "1", unique, round, total, workers, float64(total)/elapsed.Seconds(), sum/total, lats[113], lats[118], failures, retries.Load(), counts, audit, delivery, held, after.WaitCount-before.WaitCount, float64((after.WaitDuration-before.WaitDuration).Microseconds())/1000, waitAfter-waitBefore, waitTimeAfter-waitTimeBefore)
			if failures != 0 || audit != wantAudit || delivery != total || held != 0 {
				t.Fatalf("audit accounting failed")
			}
		}
	}
}
