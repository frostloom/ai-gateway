package comparison

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestAuthSingleQueryConcurrentMixed(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	st := store.NewStore(db, r)
	if err := st.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("auth-concurrent-%d", time.Now().UnixNano())
	keys := make([]store.APIKey, 0, 32)
	var firstUser store.PortalUser
	for i := 0; i < 16; i++ {
		tenant := store.Tenant{Name: fmt.Sprintf("%s-%d", tag, i)}
		if err := db.Create(&tenant).Error; err != nil {
			t.Fatal(err)
		}
		user := store.PortalUser{Username: tenant.Name, PasswordHash: "synthetic", TenantID: tenant.ID}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstUser = user
		}
		for j := 0; j < 2; j++ {
			name := "normal"
			if j == 1 {
				name = "__browser_session"
			}
			key := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("%s-%d-%d", tag, i, j), Name: name}
			if err := db.Create(&key).Error; err != nil {
				t.Fatal(err)
			}
			if j == 1 {
				if err := db.Create(&store.PortalSession{UserID: user.ID, APIKeyID: key.ID}).Error; err != nil {
					t.Fatal(err)
				}
			}
			keys = append(keys, key)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetMaxIdleConns(16)
	type sample struct {
		lat     time.Duration
		bad     bool
		timeout bool
	}
	const workers, perWorker = 32, 40
	for _, variant := range []string{"baseline", "candidate", "candidate", "baseline"} {
		before := sqlDB.Stats()
		out := make(chan sample, workers*perWorker)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var maxOpen atomic.Int64
		var maxUse atomic.Int64
		stop := make(chan struct{})
		sampled := make(chan struct{})
		go func() {
			defer close(sampled)
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					stats := sqlDB.Stats()
					for _, x := range []struct {
						p *atomic.Int64
						v int64
					}{{&maxOpen, int64(stats.OpenConnections)}, {&maxUse, int64(stats.InUse)}} {
						for {
							old := x.p.Load()
							if x.v <= old || x.p.CompareAndSwap(old, x.v) {
								break
							}
						}
					}
				}
			}
		}()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				<-start
				for i := 0; i < perWorker; i++ {
					key := keys[(w+i)%len(keys)]
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					begin := time.Now()
					var got *store.APIKey
					var err error
					if variant == "baseline" {
						got, err = st.ValidateAPIKey(ctx, key.KeyHash)
					} else {
						got, err = singleQueryAuth(ctx, db, key.KeyHash)
					}
					elapsed := time.Since(begin)
					timeout := ctx.Err() != nil
					cancel()
					out <- sample{elapsed, err != nil || got == nil || got.ID != key.ID, timeout}
				}
			}(w)
		}
		begin := time.Now()
		close(start)
		wg.Wait()
		elapsed := time.Since(begin)
		close(stop)
		<-sampled
		close(out)
		after := sqlDB.Stats()
		delays := make([]time.Duration, 0, workers*perWorker)
		var total time.Duration
		bad, timeouts := 0, 0
		for s := range out {
			delays = append(delays, s.lat)
			total += s.lat
			if s.bad {
				bad++
			}
			if s.timeout {
				timeouts++
			}
		}
		sort.Slice(delays, func(i, j int) bool { return delays[i] < delays[j] })
		n := len(delays)
		t.Logf("variant=%s n=%d workers=%d rps=%.1f mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f errors=%d timeouts=%d pool_wait_count=%d pool_wait_ms=%.3f sampled_max_open=%d sampled_max_inuse=%d", variant, n, workers, float64(n)/elapsed.Seconds(), float64(total.Microseconds())/1000/float64(n), float64(delays[n*95/100-1].Microseconds())/1000, float64(delays[n*99/100-1].Microseconds())/1000, bad, timeouts, after.WaitCount-before.WaitCount, float64((after.WaitDuration-before.WaitDuration).Microseconds())/1000, maxOpen.Load(), maxUse.Load())
		if bad != 0 || timeouts != 0 {
			t.Fatalf("invalid results")
		}
	}
	// Subsequent calls begin only after each commit, not while it is in progress.
	if err := db.Model(&keys[0]).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&firstUser).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := keys[i%2]
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var got *store.APIKey
			var err error
			if i%4 < 2 {
				got, err = st.ValidateAPIKey(ctx, key.KeyHash)
			} else {
				got, err = singleQueryAuth(ctx, db, key.KeyHash)
			}
			if err != nil || got != nil {
				results <- fmt.Errorf("disabled key %d accepted=%v err=%v", i, got != nil, err)
			}
		}(i)
	}
	wg.Wait()
	close(results)
	for e := range results {
		t.Error(e)
	}
	t.Log("after commits: 64 concurrent calls rejected disabled normal key or browser user")
}
