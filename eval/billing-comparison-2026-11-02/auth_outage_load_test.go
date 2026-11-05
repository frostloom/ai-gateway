package comparison

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm/logger"
)

// This probes authentication only, with actual isolated MySQL and an artificial
// unresponsive loopback Redis endpoint. It never touches the application Redis.
func TestAuthOutageLoad(t *testing.T) {
	if os.Getenv("AUTH_OUTAGE_LOAD") != "1" {
		t.Skip("explicit isolated load opt-in required")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.AutoMigrate(&store.APIKey{}); e != nil {
		t.Fatal(e)
	}
	hashes := make([]string, 64)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%064x", i+5000)
		if e := db.Create(&store.APIKey{TenantID: uint64(i + 1), KeyHash: hashes[i], KeyPrefix: "test"}).Error; e != nil {
			t.Fatal(e)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, nil)
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	stop := make(chan struct{})
	var serverWG sync.WaitGroup
	serverWG.Add(1)
	go func() {
		defer serverWG.Done()
		for {
			conn, e := ln.Accept()
			if e != nil {
				return
			}
			serverWG.Add(1)
			go func() { defer serverWG.Done(); defer conn.Close(); <-stop }()
		}
	}()
	defer func() { close(stop); _ = ln.Close(); serverWG.Wait() }()
	blackhole := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), DialTimeout: time.Second, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, PoolTimeout: 300 * time.Millisecond, MaxRetries: -1, ContextTimeoutEnabled: true, PoolSize: 32})
	defer blackhole.Close()
	var authorityCalls atomic.Int64
	validate := func(ctx context.Context, hash string) (*store.APIKey, error) {
		authorityCalls.Add(1)
		return svc.ValidateAPIKey(ctx, hash)
	}
	c := guardedAuthCache{r: blackhole, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: validate}
	bulkhead := newAuthBulkhead(16, validate)
	bounded := guardedAuthCache{r: blackhole, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: bulkhead.validate}
	waiter := newWaitingAuthBulkhead(16, 50*time.Millisecond, validate)
	waiting := guardedAuthCache{r: blackhole, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: waiter.validate}
	queue := newBoundedAuthBulkhead(16, 32, 50*time.Millisecond, validate)
	boundedQueue := guardedAuthCache{r: blackhole, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: queue.validate}
	for rep := 1; rep <= 3; rep++ {
		for _, workers := range []int{16, 64} {
			modes := []string{"direct", "redis_blackhole", "blackhole_bulkhead", "blackhole_wait50", "blackhole_queue32"}
			if rep%2 == 0 {
				for i, j := 0, len(modes)-1; i < j; i, j = i+1, j-1 {
					modes[i], modes[j] = modes[j], modes[i]
				}
			}
			for _, mode := range modes {
				samples := make([][]float64, workers)
				errCounts := make([]int, workers)
				timeoutCounts := make([]int, workers)
				busyCounts := make([]int, workers)
				otherTypes := make([]map[string]int, workers)
				for i := range otherTypes {
					otherTypes[i] = map[string]int{}
				}
				var ready, wg sync.WaitGroup
				ready.Add(workers)
				start := make(chan struct{})
				var deadline time.Time
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func(w int) {
						defer wg.Done()
						ready.Done()
						<-start
						for i := w; time.Now().Before(deadline); i++ {
							ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
							began := time.Now()
							var k *store.APIKey
							var e error
							if mode == "direct" {
								k, e = validate(ctx, hashes[i%len(hashes)])
							} else if mode == "redis_blackhole" {
								k, e = c.lookup(ctx, hashes[i%len(hashes)])
							} else if mode == "blackhole_bulkhead" {
								k, e = bounded.lookup(ctx, hashes[i%len(hashes)])
							} else if mode == "blackhole_wait50" {
								k, e = waiting.lookup(ctx, hashes[i%len(hashes)])
							} else {
								k, e = boundedQueue.lookup(ctx, hashes[i%len(hashes)])
							}
							elapsed := time.Since(began)
							cancel()
							samples[w] = append(samples[w], float64(elapsed.Microseconds())/1000)
							if e != nil || k == nil || k.ID == 0 {
								errCounts[w]++
								if errors.Is(e, errAuthBusy) {
									busyCounts[w]++
								}
								if errors.Is(e, context.DeadlineExceeded) {
									timeoutCounts[w]++
								} else if !errors.Is(e, errAuthBusy) {
									otherTypes[w][fmt.Sprintf("%T", e)]++
								}
							}
						}
					}(w)
				}
				ready.Wait()
				before := authorityCalls.Load()
				beforePool := sqlDB.Stats()
				began := time.Now()
				deadline = began.Add(3 * time.Second)
				close(start)
				wg.Wait()
				elapsed := time.Since(began)
				var all []float64
				fail, timeouts, busy := 0, 0, 0
				others := map[string]int{}
				for i := range samples {
					all = append(all, samples[i]...)
					fail += errCounts[i]
					timeouts += timeoutCounts[i]
					busy += busyCounts[i]
					for k, v := range otherTypes[i] {
						others[k] += v
					}
				}
				if len(all) == 0 {
					t.Fatal("no samples")
				}
				sort.Float64s(all)
				sum := 0.0
				for _, v := range all {
					sum += v
				}
				afterPool := sqlDB.Stats()
				t.Logf("AUTH_POOL rep=%d workers=%d mode=%s waits=%d wait_ms=%.2f open=%d in_use=%d", rep, workers, mode, afterPool.WaitCount-beforePool.WaitCount, float64((afterPool.WaitDuration-beforePool.WaitDuration).Microseconds())/1000, afterPool.OpenConnections, afterPool.InUse)
				t.Logf("AUTH_ERROR_TYPES rep=%d workers=%d mode=%s other=%v", rep, workers, mode, others)
				t.Logf("AUTH_OUTAGE rep=%d workers=%d mode=%s requests=%d errors=%d timeouts=%d overload=%d backend_queries=%d elapsed=%.3f success_per_sec=%.2f mean_ms=%.2f p95_ms=%.2f p99_ms=%.2f", rep, workers, mode, len(all), fail, timeouts, busy, authorityCalls.Load()-before, elapsed.Seconds(), float64(len(all)-fail)/elapsed.Seconds(), sum/float64(len(all)), all[(len(all)-1)*95/100], all[(len(all)-1)*99/100])
			}
		}
	}
}
