// Command indexwrite measures synthetic outbox writes in an isolated MySQL.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type result struct {
	Round      int     `json:"round"`
	Index      string  `json:"index"`
	Phase      string  `json:"phase"`
	Operations int     `json:"operations"`
	Errors     int64   `json:"errors"`
	Rows       int     `json:"rows"`
	Seconds    float64 `json:"seconds"`
	Rate       float64 `json:"ops_per_second"`
	Mean       float64 `json:"mean_ms"`
	P95        float64 `json:"p95_ms"`
	P99        float64 `json:"p99_ms"`
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	dsn := os.Getenv("ISOLATED_MYSQL_DSN")
	if dsn == "" {
		panic("isolated DSN required")
	}
	db, err := sql.Open("mysql", dsn)
	must(err)
	defer db.Close()
	db.SetMaxOpenConns(32)
	ctx := context.Background()
	must(db.PingContext(ctx))
	_, err = db.Exec(`CREATE TABLE outbox_events (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, topic VARCHAR(64) NOT NULL, event_key VARCHAR(128) NOT NULL, payload JSON NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), sent_at DATETIME(3) NULL, attempts INT NOT NULL DEFAULT 0, last_error VARCHAR(512) NOT NULL DEFAULT '', UNIQUE KEY uk_event_key(event_key), KEY idx_unsent(sent_at,attempts,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	must(err)
	var results []result
	const count = 2000
	const workers = 16
	for round := 1; round <= 3; round++ {
		order := []string{"existing", "candidate"}
		if round%2 == 0 {
			order[0], order[1] = order[1], order[0]
		}
		for _, index := range order {
			_, err = db.Exec("TRUNCATE TABLE outbox_events")
			must(err)
			if index == "candidate" {
				_, err = db.Exec("CREATE INDEX idx_unsent_id ON outbox_events(sent_at,id)")
				must(err)
			}
			for _, phase := range []string{"insert", "mark"} {
				lat := make([]float64, count)
				jobs := make(chan int)
				var failures atomic.Int64
				var wg sync.WaitGroup
				start := time.Now()
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for i := range jobs {
							began := time.Now()
							var e error
							if phase == "insert" {
								_, e = db.ExecContext(ctx, "INSERT INTO outbox_events(topic,event_key,payload) VALUES(?,?,?)", "billing.events", fmt.Sprintf("%d", i), `{"event_id":"synthetic"}`)
							} else {
								var res sql.Result
								res, e = db.ExecContext(ctx, "UPDATE outbox_events SET sent_at=NOW(3) WHERE event_key=? AND sent_at IS NULL", fmt.Sprintf("%d", i))
								if e == nil {
									var n int64
									n, e = res.RowsAffected()
									if e == nil && n != 1 {
										e = fmt.Errorf("affected rows=%d", n)
									}
								}
							}
							lat[i] = float64(time.Since(began).Microseconds()) / 1000
							if e != nil {
								failures.Add(1)
							}
						}
					}()
				}
				for i := 0; i < count; i++ {
					jobs <- i
				}
				close(jobs)
				wg.Wait()
				elapsed := time.Since(start).Seconds()
				var rows int
				if phase == "insert" {
					err = db.QueryRow("SELECT COUNT(*) FROM outbox_events WHERE sent_at IS NULL").Scan(&rows)
				} else {
					err = db.QueryRow("SELECT COUNT(*) FROM outbox_events WHERE sent_at IS NOT NULL").Scan(&rows)
				}
				must(err)
				sum := 0.0
				for _, v := range lat {
					sum += v
				}
				sort.Float64s(lat)
				results = append(results, result{round, index, phase, count, failures.Load(), rows, elapsed, float64(count) / elapsed, sum / count, lat[(count*95+99)/100-1], lat[(count*99+99)/100-1]})
				if failures.Load() != 0 || rows != count {
					_ = json.NewEncoder(os.Stdout).Encode(results)
					os.Exit(1)
				}
			}
			if index == "candidate" {
				_, err = db.Exec("DROP INDEX idx_unsent_id ON outbox_events")
				must(err)
			}
		}
	}
	must(json.NewEncoder(os.Stdout).Encode(results))
}
