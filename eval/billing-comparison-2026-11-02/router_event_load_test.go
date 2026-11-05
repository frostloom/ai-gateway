package comparison

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

// Closed-loop diagnostic of candidate only, not a whole-gateway benchmark.
func TestRouterEventLoad(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENT_LOAD") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetMaxIdleConns(16)
	const total = 600
	const concurrency = 16
	readCounter := func(name string) uint64 {
		t.Helper()
		var raw string
		if err := sqlDB.QueryRow("SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME = ?", name).Scan(&raw); err != nil {
			t.Fatalf("read mysql counter %s: %v", name, err)
		}
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, providers := range []int{1, 32} {
		for round := 1; round <= 3; round++ {
			prefix := fmt.Sprintf("load-%d-%d-%d", providers, round, time.Now().UnixNano())
			ps := make([]store.Provider, providers)
			for i := range ps {
				ps[i] = store.Provider{Name: fmt.Sprintf("%s-p%d", prefix, i), BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
				if err := db.Create(&ps[i]).Error; err != nil {
					t.Fatal(err)
				}
			}
			type measurement struct {
				duration time.Duration
				err      error
			}
			results := make(chan measurement, total)
			jobs := make(chan int)
			var wg sync.WaitGroup
			ctx := context.Background()
			before := sqlDB.Stats()
			lockWaitsBefore := readCounter("Innodb_row_lock_waits")
			lockTimeBefore := readCounter("Innodb_row_lock_time")
			start := time.Now()
			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for job := range jobs {
						began := time.Now()
						err := testRouterApply(ctx, db, fmt.Sprintf("%s-e%d", prefix, job), ps[job%providers].ID, true, false)
						results <- measurement{time.Since(began), err}
					}
				}()
			}
			for i := 0; i < total; i++ {
				jobs <- i
			}
			close(jobs)
			wg.Wait()
			elapsed := time.Since(start)
			close(results)
			after := sqlDB.Stats()
			lockWaitsAfter := readCounter("Innodb_row_lock_waits")
			lockTimeAfter := readCounter("Innodb_row_lock_time")
			lat := make([]float64, 0, total)
			var sum float64
			errors := 0
			for v := range results {
				if v.err != nil {
					errors++
				}
				ms := float64(v.duration.Microseconds()) / 1000
				lat = append(lat, ms)
				sum += ms
			}
			sort.Float64s(lat)
			var count int64
			ids := make([]uint64, len(ps))
			for i := range ps {
				ids[i] = ps[i].ID
			}
			if err := db.Model(&routerTestEvent{}).Where("provider_id IN ?", ids).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if errors != 0 || count != total {
				t.Fatalf("errors=%d events=%d", errors, count)
			}
			t.Logf("providers=%d round=%d operations=%d concurrency=%d rps=%.2f mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f errors=%d timeout=not_configured db_wait_count=%d db_wait_ms=%.3f open_connections=%d mysql_row_waits=%d mysql_row_wait_ms=%d", providers, round, total, concurrency, float64(total)/elapsed.Seconds(), sum/float64(total), lat[569], lat[593], errors, after.WaitCount-before.WaitCount, float64((after.WaitDuration-before.WaitDuration).Microseconds())/1000, after.OpenConnections, lockWaitsAfter-lockWaitsBefore, lockTimeAfter-lockTimeBefore)
		}
	}
}
