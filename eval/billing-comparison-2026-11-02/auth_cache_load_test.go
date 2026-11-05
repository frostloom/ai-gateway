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

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm/logger"
)

func TestAuthCacheRealStoreLoad(t *testing.T) {
	if os.Getenv("AUTH_CACHE_LOAD") != "1" {
		t.Skip("requires isolated services")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	ctx := context.Background()
	hashes := make([]string, 64)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%064x", i+1)
		if err := db.Create(&store.APIKey{TenantID: uint64(i + 1), KeyHash: hashes[i], KeyPrefix: "test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the project's actual ValidateAPIKey against a changed MySQL row.
	probe := fmt.Sprintf("%064x", 9999)
	expiry := time.Now().Add(time.Minute)
	row := store.APIKey{TenantID: 9999, KeyHash: probe, KeyPrefix: "test", ExpiresAt: &expiry}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	p := authCache{r: r, validate: svc.ValidateAPIKey, ttl: 100 * time.Millisecond}
	if k, e := p.lookup(ctx, probe); e != nil || k == nil {
		t.Fatalf("probe initial: %v %v", k, e)
	}
	if err := db.Model(&row).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if k, e := p.lookup(ctx, probe); e != nil || k == nil {
		t.Fatalf("expected stale disabled key within TTL: %v %v", k, e)
	}
	time.Sleep(140 * time.Millisecond)
	if k, e := p.lookup(ctx, probe); e != nil || k != nil {
		t.Fatalf("disabled after TTL: %v %v", k, e)
	}
	t.Log("actual MySQL status change: stale until TTL, then rejected")
	var authorityQueries atomic.Int64
	counted := func(ctx context.Context, hash string) (*store.APIKey, error) {
		authorityQueries.Add(1)
		return svc.ValidateAPIKey(ctx, hash)
	}
	c := authCache{r: r, validate: counted, ttl: time.Minute}
	f := fencedAuthCache{r: r, validate: counted, ttl: time.Minute, fenceTTL: 10 * time.Second}
	g := guardedAuthCache{r: r, validate: counted, ttl: time.Minute, fenceTTL: 10 * time.Second}
	for _, h := range hashes {
		if k, e := c.lookup(ctx, h); e != nil || k == nil {
			t.Fatal(k, e)
		}
	}
	for _, h := range hashes {
		if k, e := f.lookup(ctx, h); e != nil || k == nil {
			t.Fatal(k, e)
		}
	}
	for _, h := range hashes {
		if k, e := g.lookup(ctx, h); e != nil || k == nil {
			t.Fatal(k, e)
		}
	}
	for rep := 1; rep <= 3; rep++ {
		for _, mode := range []string{"mysql", "cache", "fenced", "guarded"} {
			const workers = 32
			var wg, ready sync.WaitGroup
			ready.Add(workers)
			start := make(chan struct{})
			samples := make([][]time.Duration, workers)
			failures := make([]int, workers)
			var end time.Time
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					ready.Done()
					<-start
					i := w
					for time.Now().Before(end) {
						began := time.Now()
						var k *store.APIKey
						var e error
						if mode == "mysql" {
							k, e = svc.ValidateAPIKey(ctx, hashes[i%len(hashes)])
						} else if mode == "cache" {
							k, e = c.lookup(ctx, hashes[i%len(hashes)])
						} else if mode == "fenced" {
							k, e = f.lookup(ctx, hashes[i%len(hashes)])
						} else {
							k, e = g.lookup(ctx, hashes[i%len(hashes)])
						}
						samples[w] = append(samples[w], time.Since(began))
						if e != nil || k == nil || k.ID == 0 {
							failures[w]++
						}
						i++
					}
				}(w)
			}
			ready.Wait()
			authorityBefore := authorityQueries.Load()
			began := time.Now()
			end = began.Add(2 * time.Second)
			close(start)
			wg.Wait()
			elapsed := time.Since(began)
			var all []time.Duration
			fail := 0
			for i := range samples {
				all = append(all, samples[i]...)
				fail += failures[i]
			}
			sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
			var sum time.Duration
			for _, v := range all {
				sum += v
			}
			if len(all) == 0 {
				t.Fatal("no samples")
			}
			t.Logf("AUTH_LOAD rep=%d mode=%s workers=%d keys=%d requests=%d failures=%d elapsed=%.3f rps=%.1f mean_ms=%.3f p95_ms=%.3f p99_ms=%.3f", rep, mode, workers, len(hashes), len(all), fail, elapsed.Seconds(), float64(len(all)-fail)/elapsed.Seconds(), float64(sum.Microseconds())/float64(len(all))/1000, float64(all[(len(all)-1)*95/100].Microseconds())/1000, float64(all[(len(all)-1)*99/100].Microseconds())/1000)
			t.Logf("AUTH_AUTHORITY_QUERIES rep=%d mode=%s queries=%d", rep, mode, authorityQueries.Load()-authorityBefore)
			if fail != 0 {
				t.Errorf("failures=%d", fail)
			}
		}
	}
}
