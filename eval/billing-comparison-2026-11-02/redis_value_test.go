package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm/logger"
)

// Service interruption is restricted to the container created by the harness.
func TestRedisOutageValue(t *testing.T) {
	name := os.Getenv("COMPARISON_REDIS_CONTAINER")
	if !strings.HasPrefix(name, "billing-comparison-redis-") {
		t.Skip("requires harness-owned redis container")
	}
	db, _ := isolatedDB(t)
	c := candidate{db: db}
	if e := c.migrate(); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("COMPARISON_REDIS_ADDR"), DialTimeout: 300 * time.Millisecond, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, MaxRetries: -1})
	defer client.Close()
	a := store.NewStore(db, client)
	tenant := store.Tenant{ID: 50001, Name: "redis-outage-a", InitialQuota: 1000}
	if e := db.Create(&tenant).Error; e != nil {
		t.Fatal(e)
	}
	if e := client.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	if e := c.seed(50002, 1000); e != nil {
		t.Fatal(e)
	}
	req := &billingv1.ReserveRequest{RequestId: "outage-before", TenantId: 50001, ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
	if _, e := a.Reserve(ctx, req); e != nil {
		t.Fatal(e)
	}
	if out, e := exec.Command("docker", "stop", "--time", "2", name).CombinedOutput(); e != nil {
		t.Fatalf("stop: %v %s", e, out)
	}
	t.Cleanup(func() { exec.Command("docker", "start", name).Run() })
	unavailable, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, e := a.GetBalance(unavailable, 50001); e == nil {
		t.Error("A balance read succeeded with Redis stopped")
	}
	req.RequestId = "outage-blocked"
	if _, e := a.Reserve(unavailable, req); e == nil {
		t.Error("A reserve succeeded with Redis stopped")
	}
	br := &billingv1.ReserveRequest{RequestId: "outage-candidate", TenantId: 50002, ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
	if r, e := c.reserve(ctx, br); e != nil || r.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("B reserve with Redis stopped: %+v %v", r, e)
	}
	if e := c.usage(ctx, &billingv1.ReportUsageRequest{RequestId: br.RequestId, PromptTokens: 50}); e != nil {
		t.Fatal(e)
	}
	if ok, e := c.settle(ctx, br.RequestId); e != nil || !ok {
		t.Fatalf("B settle: %v %v", ok, e)
	}
	if out, e := exec.Command("docker", "start", name).CombinedOutput(); e != nil {
		t.Fatalf("start: %v %s", e, out)
	}
	portOut, e := exec.Command("docker", "port", name, "6379/tcp").Output()
	if e != nil {
		t.Fatal(e)
	}
	newAddr := strings.TrimSpace(string(portOut))
	if !strings.HasPrefix(newAddr, "127.0.0.1:") || strings.Contains(newAddr, "\n") {
		t.Fatalf("unexpected port: %q", newAddr)
	}
	if e := os.Setenv("COMPARISON_REDIS_ADDR", newAddr); e != nil {
		t.Fatal(e)
	}
	client = redis.NewClient(&redis.Options{Addr: newAddr, DialTimeout: 300 * time.Millisecond, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, MaxRetries: -1})
	defer client.Close()
	a = store.NewStore(db, client)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if client.Ping(ctx).Err() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Redis restart timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, e := a.RebuildBalance(ctx, 50001); e != nil {
		t.Fatal(e)
	}
	live, e := a.GetBalance(ctx, 50001)
	if e != nil || live != 900 {
		t.Fatalf("A recovered balance=%d %v", live, e)
	}
	b, e := c.balance(ctx, 50002)
	if e != nil || b != 950 {
		t.Fatalf("B balance=%d %v", b, e)
	}
	var rows int64
	if e := db.Model(&store.Bill{}).Where("request_id=?", "outage-blocked").Count(&rows).Error; e != nil || rows != 0 {
		t.Fatalf("blocked bills=%d %v", rows, e)
	}
	if e := c.audit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Logf("A rejected reads and reserve during Redis outage; B completed reserve/usage/settle; A rebuilt balance=%d, B=%d", live, b)
}

// Isolates the already-implemented balance read paths; this does not benchmark writes.
func TestBalanceReadValue(t *testing.T) {
	if os.Getenv("BILLING_COMPARE_READ_LOAD") != "1" {
		t.Skip("explicit read load opt-in required")
	}
	db, rdb := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db}
	if e := c.migrate(); e != nil {
		t.Fatal(e)
	}
	a := store.NewStore(db, rdb)
	ctx := context.Background()
	var next uint64 = 60000
	for rep := 1; rep <= 3; rep++ {
		for _, workers := range []int{1, 8, 32} {
			for _, mode := range []string{"A", "B"} {
				next++
				id := next
				if mode == "A" {
					if e := db.Create(&store.Tenant{ID: id, Name: fmt.Sprintf("read-%d", id), InitialQuota: 1000}).Error; e != nil {
						t.Fatal(e)
					}
					if e := rdb.Set(ctx, store.BalanceKey(id), 1000, 0).Err(); e != nil {
						t.Fatal(e)
					}
				} else if e := c.seed(id, 1000); e != nil {
					t.Fatal(e)
				}
				var done, fail atomic.Int64
				latency := make([][]float64, workers)
				var wg sync.WaitGroup
				start := make(chan struct{})
				var ready sync.WaitGroup
				ready.Add(workers)
				var deadline time.Time
				for i := 0; i < workers; i++ {
					wg.Add(1)
					go func(worker int) {
						defer wg.Done()
						ready.Done()
						<-start
						for time.Now().Before(deadline) {
							began := time.Now()
							op, cancel := context.WithTimeout(ctx, 3*time.Second)
							var b int64
							var e error
							if mode == "A" {
								b, e = a.GetBalance(op, id)
							} else {
								b, e = c.balance(op, id)
							}
							cancel()
							latency[worker] = append(latency[worker], float64(time.Since(began))/float64(time.Millisecond))
							if e != nil || b != 1000 {
								fail.Add(1)
							} else {
								done.Add(1)
							}
						}
					}(i)
				}
				ready.Wait()
				beg := time.Now()
				deadline = beg.Add(2 * time.Second)
				close(start)
				wg.Wait()
				elapsed := time.Since(beg).Seconds()
				var all []float64
				var sum float64
				for _, v := range latency {
					all = append(all, v...)
					for _, ms := range v {
						sum += ms
					}
				}
				sort.Float64s(all)
				if len(all) == 0 {
					t.Fatal("no reads")
				}
				t.Logf("BALANCE_READ mode=%s rep=%d workers=%d elapsed=%.6f completed=%d errors=%d reads_per_second=%.2f mean_ms=%.4f p95_ms=%.4f p99_ms=%.4f", mode, rep, workers, elapsed, done.Load(), fail.Load(), float64(done.Load())/elapsed, sum/float64(len(all)), all[(len(all)-1)*95/100], all[(len(all)-1)*99/100])
				if fail.Load() > 0 || done.Load() == 0 {
					t.Error("read failed")
				}
			}
		}
	}
}

func TestMySQLDownCachedRead(t *testing.T) {
	name := os.Getenv("COMPARISON_MYSQL_CONTAINER")
	if !strings.HasPrefix(name, "billing-comparison-mysql-") {
		t.Skip("requires harness-owned mysql container")
	}
	db, rdb := isolatedDB(t)
	c := candidate{db: db}
	if e := c.migrate(); e != nil {
		t.Fatal(e)
	}
	a := store.NewStore(db, rdb)
	ctx := context.Background()
	if e := db.Create(&store.Tenant{ID: 70001, Name: "mysql-outage-a", InitialQuota: 1000}).Error; e != nil {
		t.Fatal(e)
	}
	if e := rdb.Set(ctx, store.BalanceKey(70001), 1000, 0).Err(); e != nil {
		t.Fatal(e)
	}
	if e := c.seed(70002, 1000); e != nil {
		t.Fatal(e)
	}
	if out, e := exec.Command("docker", "stop", "--time", "5", name).CombinedOutput(); e != nil {
		t.Fatalf("stop mysql: %v %s", e, out)
	}
	t.Cleanup(func() { exec.Command("docker", "start", name).Run() })
	readctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	balance, e := a.GetBalance(readctx, 70001)
	if e != nil || balance != 1000 {
		t.Fatalf("cached balance=%d err=%v", balance, e)
	}
	if _, e := c.balance(readctx, 70002); e == nil {
		t.Error("B query succeeded with MySQL stopped")
	}
	t.Logf("A cached balance read=%d succeeded while MySQL unavailable; B balance read failed; cached value freshness was not tested", balance)
}
