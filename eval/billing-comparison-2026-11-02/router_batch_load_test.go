package comparison

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

// All-success SQL-only experiment, not production router/circuit behavior.
func TestRouterBatchLoad(t *testing.T) {
	if os.Getenv("ROUTER_BATCH_LOAD") != "1" {
		t.Skip("opt-in isolated performance test")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.Exec("CREATE TABLE IF NOT EXISTS eval_router_batch (id BIGINT PRIMARY KEY,status INT NOT NULL,consecutive_fail INT NOT NULL) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM eval_router_batch").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO eval_router_batch VALUES(1,0,0)").Error; e != nil {
		t.Fatal(e)
	}
	for rep := 1; rep <= 3; rep++ {
		for _, workers := range []int{32, 128} {
			order := []string{"baseline", "batch"}
			if rep == 2 {
				order = []string{"batch", "baseline"}
			}
			for _, mode := range order {
				var serial sync.Mutex
				var batch *batchReport
				var workerDone chan struct{}
				if mode == "batch" {
					batch = newBatchReport()
					workerDone = make(chan struct{})
					go func() { defer close(workerDone); batch.run(db) }()
				}
				type result struct {
					lats   []float64
					errors int
				}
				samples := make([]result, workers)
				var wg, ready sync.WaitGroup
				ready.Add(workers)
				start := make(chan struct{})
				var deadline time.Time
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(w int) {
						defer wg.Done()
						ready.Done()
						<-start
						for time.Now().Before(deadline) {
							begin := time.Now()
							var e error
							if mode == "batch" {
								e = <-batch.enqueue(true)
							} else {
								serial.Lock()
								e = db.Table("eval_router_batch").Where("id=? AND status=?", 1, 0).UpdateColumn("consecutive_fail", 0).Error
								serial.Unlock()
							}
							samples[w].lats = append(samples[w].lats, float64(time.Since(begin).Microseconds())/1000)
							if e != nil {
								samples[w].errors++
							}
						}
					}(w)
				}
				ready.Wait()
				begin := time.Now()
				deadline = begin.Add(2 * time.Second)
				close(start)
				wg.Wait()
				elapsed := time.Since(begin).Seconds()
				if batch != nil {
					close(batch.jobs)
					<-workerDone
				}
				var lats []float64
				var errs int
				var sum float64
				for _, v := range samples {
					errs += v.errors
					lats = append(lats, v.lats...)
					for _, lat := range v.lats {
						sum += lat
					}
				}
				sort.Float64s(lats)
				if len(lats) == 0 {
					t.Fatal("no requests")
				}
				updates := int64(len(lats))
				if batch != nil {
					updates = batch.updates.Load()
				}
				var value int
				if e := db.Raw("SELECT consecutive_fail FROM eval_router_batch WHERE id=1").Scan(&value).Error; e != nil || value != 0 {
					t.Fatalf("count=%d err=%v", value, e)
				}
				p := func(n int) float64 { return lats[(len(lats)*n+99)/100-1] }
				out := map[string]any{"rep": rep, "workers": workers, "mode": mode, "elapsed_s": elapsed, "requests": len(lats), "success_per_s": float64(len(lats)-errs) / elapsed, "errors": errs, "sql_updates": updates, "mean_ms": sum / float64(len(lats)), "p95_ms": p(95), "p99_ms": p(99)}
				raw, _ := json.Marshal(out)
				fmt.Println("BATCH_LOAD " + string(raw))
				if errs != 0 {
					t.Fatalf("mode=%s errors=%d", mode, errs)
				}
			}
		}
	}
}
