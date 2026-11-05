package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"gorm.io/gorm/logger"
)

// Fixed-duration, closed-loop diagnostic. Both paths use the same Store inputs,
// synthetic catalog, database, client pool, and query mix. Not production capacity.
func TestComparisonLoad(t *testing.T) {
	if os.Getenv("BILLING_COMPARE_LOAD") != "1" {
		t.Skip("explicit load opt-in required")
	}
	db, rdb := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	a := store.NewStore(db, rdb)
	h := hybrid{candidate: c, redis: rdb, ttl: 10 * time.Second}
	ctx := context.Background()
	counter := uint64(10000)
	tag := "short"
	if os.Getenv("BILLING_COMPARE_SOAK") == "1" {
		counter = 30000
		tag = "soak"
	}
	repetitions, duration := 3, 2*time.Second
	scenarios := []struct{ accounts, workers int }{{1, 1}, {1, 8}, {1, 32}, {8, 1}, {8, 8}, {8, 32}}
	if os.Getenv("BILLING_COMPARE_SOAK") == "1" {
		repetitions, duration = 1, 15*time.Second
		scenarios = []struct{ accounts, workers int }{{8, 8}}
	}
	if os.Getenv("BILLING_COMPARE_STABILITY") == "1" {
		tag = "stability"
		counter = 200000
		repetitions = 2
		duration = 20 * time.Second
		scenarios = []struct{ accounts, workers int }{{1, 32}, {8, 64}}
	}
	modes := []string{"A", "B"}
	hybridLoad := os.Getenv("BILLING_COMPARE_HYBRID_LOAD") == "1"
	if hybridLoad {
		tag = "hybrid"
		counter = 90000
		scenarios = []struct{ accounts, workers int }{{1, 8}, {8, 8}}
		modes = []string{"A", "B", "H"}
	}
	for rep := 0; rep < repetitions; rep++ {
		for _, tc := range scenarios {
			for _, mode := range modes {
				counter += 100
				ids := make([]uint64, tc.accounts)
				for i := range ids {
					ids[i] = counter + uint64(i)
					if mode == "A" {
						if e := db.Create(&store.Tenant{ID: ids[i], Name: fmt.Sprintf("bench-%d", ids[i]), InitialQuota: 10000000}).Error; e != nil {
							t.Fatal(e)
						}
						if e := rdb.Set(ctx, store.BalanceKey(ids[i]), 10000000, 0).Err(); e != nil {
							t.Fatal(e)
						}
					} else if e := c.seed(ids[i], 10000000); e != nil {
						t.Fatal(e)
					}
				}
				prefix := fmt.Sprintf("cmp-%s-%s-%d-%d-%d", tag, mode, rep, tc.accounts, tc.workers)
				var seq, completed, failures, timeouts atomic.Int64
				var mu sync.Mutex
				latencies := []float64{}
				samples := []string{}
				starts := make(chan struct{})
				var ready, wg sync.WaitGroup
				ready.Add(tc.workers)
				sqlDB, _ := db.DB()
				statusSnapshot := func() map[string]int64 {
					values := map[string]int64{}
					rows, e := sqlDB.QueryContext(ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_row_lock_waits','Innodb_row_lock_time','Innodb_data_reads','Innodb_data_writes','Threads_connected','Threads_running')")
					if e != nil {
						return nil
					}
					defer rows.Close()
					for rows.Next() {
						var k, v string
						if rows.Scan(&k, &v) != nil {
							return nil
						}
						n, e := strconv.ParseInt(v, 10, 64)
						if e != nil {
							return nil
						}
						values[k] = n
					}
					if rows.Err() != nil {
						return nil
					}
					return values
				}
				mysqlBefore := statusSnapshot()
				poolBefore := sqlDB.Stats()
				redisBefore, _ := rdb.Info(ctx, "stats").Result()
				redisMemoryBefore, _ := rdb.Info(ctx, "memory").Result()
				var deadline time.Time
				for w := 0; w < tc.workers; w++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						ready.Done()
						<-starts
						for time.Now().Before(deadline) {
							n := seq.Add(1)
							id := ids[int(n-1)%len(ids)]
							rid := fmt.Sprintf("%s-%d", prefix, n)
							beg := time.Now()
							callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
							req := &billingv1.ReserveRequest{RequestId: rid, TenantId: int64(id), ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
							var err error
							var res *billingv1.ReserveResponse
							if mode == "A" {
								res, err = a.Reserve(callCtx, req)
							} else if mode == "H" {
								res, err = h.reserve(callCtx, req)
							} else {
								res, err = c.reserve(callCtx, req)
							}
							if err == nil && res.Code != billingv1.ReserveCode_RESERVE_OK {
								err = fmt.Errorf("reserve code %v", res.Code)
							}
							if err == nil {
								u := &billingv1.ReportUsageRequest{RequestId: rid, PromptTokens: 50}
								if mode == "A" {
									err = a.ReportUsage(callCtx, u)
								} else {
									err = c.usage(callCtx, u)
								}
							}
							if err == nil {
								if mode == "A" {
									var x *billingv1.SettleResponse
									x, err = a.Settle(callCtx, &billingv1.SettleRequest{RequestId: rid})
									if err == nil && !x.Settled {
										err = fmt.Errorf("not settled")
									}
								} else {
									var ok bool
									if mode == "H" {
										ok, err = h.settle(callCtx, rid, id)
									} else {
										ok, err = c.settle(callCtx, rid)
									}
									if err == nil && !ok {
										err = fmt.Errorf("not settled")
									}
								}
							}
							queries := 0
							if hybridLoad {
								queries = 10
							} else if n%4 == 0 {
								queries = 1
							}
							for j := 0; err == nil && j < queries; j++ {
								if mode == "A" {
									_, err = a.GetBalance(callCtx, id)
								} else if mode == "H" {
									_, err = h.read(callCtx, id)
								} else {
									_, err = c.balance(callCtx, id)
								}
							}
							if callCtx.Err() != nil {
								timeouts.Add(1)
							}
							cancel()
							ms := float64(time.Since(beg)) / float64(time.Millisecond)
							mu.Lock()
							latencies = append(latencies, ms)
							if err != nil && len(samples) < 3 {
								samples = append(samples, err.Error())
							}
							mu.Unlock()
							if err != nil {
								failures.Add(1)
							} else {
								completed.Add(1)
							}
						}
					}()
				}
				ready.Wait()
				beg := time.Now()
				deadline = beg.Add(duration)
				close(starts)
				wg.Wait()
				elapsed := time.Since(beg).Seconds()
				poolAfter := sqlDB.Stats()
				mysqlAfter := statusSnapshot()
				redisAfter, _ := rdb.Info(ctx, "stats").Result()
				redisMemoryAfter, _ := rdb.Info(ctx, "memory").Result()
				var issues []string
				for _, id := range ids {
					var live int64
					var e error
					if mode == "A" {
						live, e = rdb.Get(ctx, store.BalanceKey(id)).Int64()
					} else {
						live, e = c.balance(ctx, id)
					}
					ledger, le := store.LedgerBalance(ctx, db, id)
					if e != nil || le != nil || live != ledger {
						issues = append(issues, fmt.Sprintf("tenant %d live=%d ledger=%d err=%v/%v", id, live, ledger, e, le))
					}
				}
				var pending, settled, events int64
				db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=? AND status=?", prefix+"-%", store.PhaseReserve, store.StatusPending).Count(&pending)
				db.Model(&store.Bill{}).Where("request_id LIKE ? AND phase=?", prefix+"-%", store.PhaseSettle).Count(&settled)
				db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?", prefix+"-%").Count(&events)
				if pending != 0 || settled != completed.Load() || events != completed.Load()*3 {
					issues = append(issues, fmt.Sprintf("pending=%d settled=%d outbox=%d completed=%d", pending, settled, events, completed.Load()))
				}
				sort.Float64s(latencies)
				mean := 0.0
				for _, x := range latencies {
					mean += x
				}
				if len(latencies) > 0 {
					mean /= float64(len(latencies))
				}
				pct := func(p float64) float64 {
					if len(latencies) == 0 {
						return 0
					}
					return latencies[int(math.Ceil(p*float64(len(latencies))))-1]
				}
				redisMetric := func(s string) int64 {
					for _, line := range strings.Split(s, "\n") {
						if strings.HasPrefix(line, "total_commands_processed:") {
							var v int64
							fmt.Sscanf(line, "total_commands_processed:%d", &v)
							return v
						}
					}
					return -1
				}
				redisMemory := func(s string) int64 {
					for _, line := range strings.Split(s, "\n") {
						if strings.HasPrefix(line, "used_memory:") {
							n, e := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "used_memory:")), 10, 64)
							if e == nil {
								return n
							}
						}
					}
					return -1
				}
				result := map[string]any{"redis_memory_before_bytes": redisMemory(redisMemoryBefore), "redis_memory_after_bytes": redisMemory(redisMemoryAfter), "query_count_per_cycle": func() string {
					if hybridLoad {
						return "10"
					}
					return "one per four"
				}(), "mode": mode, "rep": rep + 1, "accounts": tc.accounts, "workers": tc.workers, "seconds": elapsed, "completed": completed.Load(), "cycles_per_s": float64(completed.Load()) / elapsed, "errors": failures.Load(), "timeouts": timeouts.Load(), "mean_ms": mean, "p95_ms": pct(.95), "p99_ms": pct(.99), "pending": pending, "settled": settled, "outbox": events, "audit_errors": issues, "error_samples": samples, "pool_wait_count": poolAfter.WaitCount - poolBefore.WaitCount, "pool_open_end": poolAfter.OpenConnections, "redis_commands": redisMetric(redisAfter) - redisMetric(redisBefore), "mysql_status_before": mysqlBefore, "mysql_status_after": mysqlAfter}
				b, _ := json.Marshal(result)
				t.Logf("COMPARISON %s", b)
				if failures.Load() > 0 || len(issues) > 0 {
					t.Errorf("invalid scenario %s %v", prefix, issues)
				}
			}
		}
	}
}
