package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/router"
	"gorm.io/gorm/logger"
)

func TestDurableBreakerPermitLoad(t *testing.T) {
	if os.Getenv("DURABLE_BREAKER_LOAD") != "1" {
		t.Skip("isolated opt-in load")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL,failures INT NOT NULL,open_until DATETIME(6) NULL,probe_token VARBINARY(64) NULL,probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)"} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("breaker-load-%d", time.Now().UnixNano())
	p := store.Provider{Name: name, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, name), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	redisKey := fmt.Sprintf("eval:breaker:bench:%d", time.Now().UnixNano())
	if err := r.HSet(context.Background(), redisKey, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Del(context.Background(), redisKey).Err() })
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if got, e := reg.Route(name); e != nil || got == nil || got.ID != p.ID {
		t.Fatal(got, e)
	}
	type sample struct {
		count, errCount, timeouts int
		sum                       float64
		lats                      []float64
	}
	for rep := 1; rep <= 2; rep++ {
		for _, workers := range []int{16, 64} {
			modes := []string{"memory_route", "db_permit", "redis_permit"}
			if rep == 2 {
				modes = []string{"redis_permit", "db_permit", "memory_route"}
			}
			for _, mode := range modes {
				rows := make([]sample, workers)
				start := make(chan struct{})
				var ready, done sync.WaitGroup
				ready.Add(workers)
				done.Add(workers)
				var deadline time.Time
				for i := 0; i < workers; i++ {
					go func(i int) {
						defer done.Done()
						ready.Done()
						<-start
						for time.Now().Before(deadline) {
							at := time.Now()
							var e error
							if mode == "memory_route" {
								got, err := reg.Route(name)
								e = err
								if e == nil && (got == nil || got.ID != p.ID) {
									e = fmt.Errorf("unexpected route")
								}
							} else {
								ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
								if mode == "redis_permit" {
									var permit int
									permit, e = projectionPermit.Run(ctx, r, []string{redisKey}).Int()
									if e == nil && permit != 1 {
										e = fmt.Errorf("redis permit=%d", permit)
									}
								} else {
									var ok bool
									ok, e = durableBreakerPermit(ctx, db, "bench")
									if e == nil && !ok {
										e = fmt.Errorf("permit denied")
									}
								}
								cancel()
							}
							ms := float64(time.Since(at).Microseconds()) / 1000
							row := &rows[i]
							row.count++
							row.sum += ms
							if row.count%32 == 1 {
								row.lats = append(row.lats, ms)
							}
							if e != nil {
								row.errCount++
								if errors.Is(e, context.DeadlineExceeded) {
									row.timeouts++
								}
							}
						}
					}(i)
				}
				ready.Wait()
				started := time.Now()
				deadline = started.Add(time.Second)
				close(start)
				done.Wait()
				elapsed := time.Since(started).Seconds()
				var n, errs, timeouts int
				var sum float64
				var lats []float64
				for _, v := range rows {
					n += v.count
					errs += v.errCount
					timeouts += v.timeouts
					sum += v.sum
					lats = append(lats, v.lats...)
				}
				if n == 0 || len(lats) == 0 {
					t.Fatal("no measurements")
				}
				sort.Float64s(lats)
				pct := func(v float64) float64 { return lats[int(float64(len(lats)-1)*v)] }
				out, _ := json.Marshal(map[string]any{"rep": rep, "workers": workers, "mode": mode, "operations": n, "errors": errs, "timeouts": timeouts, "elapsed_seconds": elapsed, "completed_per_second": float64(n) / elapsed, "mean_ms": sum / float64(n), "sampled_p95_ms": pct(.95), "sampled_p99_ms": pct(.99), "sample_count": len(lats)})
				t.Log(string(out))
				if errs != 0 {
					t.Errorf("mode=%s errors=%d", mode, errs)
				}
			}
		}
	}
}
