package comparison

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

// Not an equivalent billing benchmark: compares SETNX admission alone against
// durable SQL claim plus transition to forwarding, with synthetic unique IDs.
func TestSQLGateLoad(t *testing.T) {
	if os.Getenv("IDEM_SQL_LOAD") != "1" {
		t.Skip("explicit isolated load opt-in required")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	g := sqlIdemGate{db: db}
	if e := g.migrate(); e != nil {
		t.Fatal(e)
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	prefix := fmt.Sprintf("gate-load-%d", time.Now().UnixNano())
	var seq atomic.Int64
	defer db.Where("request_id LIKE ?", prefix+"%").Delete(&sqlIdemIntent{})
	for rep := 1; rep <= 3; rep++ {
		for _, workers := range []int{16, 64} {
			modes := []string{"redis", "sql"}
			if rep%2 == 0 {
				modes[0], modes[1] = modes[1], modes[0]
			}
			for _, mode := range modes {
				var wg, ready sync.WaitGroup
				ready.Add(workers)
				start := make(chan struct{})
				var deadline time.Time
				samples := make([][]float64, workers)
				failures := make([]int, workers)
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(w int) {
						defer wg.Done()
						ready.Done()
						<-start
						for time.Now().Before(deadline) {
							request := fmt.Sprintf("%s-%d", prefix, seq.Add(1))
							ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
							began := time.Now()
							var err error
							if mode == "redis" {
								var ok bool
								ok, err = r.SetNX(ctx, "eval-gate:"+request, "test", time.Minute).Result()
								if err == nil && !ok {
									err = fmt.Errorf("duplicate")
								}
							} else {
								var state string
								state, err = g.claim(ctx, request, "synthetic", "owner", time.Minute)
								if err == nil && state != gateOwned {
									err = fmt.Errorf("claim %s", state)
								}
								if err == nil {
									var ok bool
									ok, err = g.beginForward(ctx, request, "owner")
									if err == nil && !ok {
										err = fmt.Errorf("not forwarded")
									}
								}
							}
							samples[w] = append(samples[w], float64(time.Since(began).Microseconds())/1000)
							cancel()
							if err != nil {
								failures[w]++
							}
						}
					}(w)
				}
				ready.Wait()
				before := sqlDB.Stats()
				began := time.Now()
				deadline = began.Add(2 * time.Second)
				close(start)
				wg.Wait()
				elapsed := time.Since(began)
				after := sqlDB.Stats()
				var all []float64
				fail := 0
				for i := range samples {
					all = append(all, samples[i]...)
					fail += failures[i]
				}
				if len(all) == 0 {
					t.Fatal("no samples")
				}
				sort.Float64s(all)
				sum := 0.0
				for _, v := range all {
					sum += v
				}
				t.Logf("SQL_GATE_LOAD rep=%d workers=%d mode=%s requests=%d errors=%d elapsed=%.3f success_per_sec=%.2f mean_ms=%.2f p95_ms=%.2f p99_ms=%.2f pool_waits=%d pool_wait_ms=%.2f", rep, workers, mode, len(all), fail, elapsed.Seconds(), float64(len(all)-fail)/elapsed.Seconds(), sum/float64(len(all)), all[(len(all)-1)*95/100], all[(len(all)-1)*99/100], after.WaitCount-before.WaitCount, float64((after.WaitDuration-before.WaitDuration).Microseconds())/1000)
			}
		}
	}
}
