package comparison

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm/logger"
)

func TestAuthBoundedFallbackAfterRedisCrash(t *testing.T) {
	name := os.Getenv("COMPARISON_REDIS_CONTAINER")
	addr := os.Getenv("COMPARISON_REDIS_ADDR")
	if os.Getenv("TEST_AUTH_REDIS_CRASH") != "1" || !regexp.MustCompile(`^auth-cache-test-redis-[0-9]+-[0-9]+$`).MatchString(name) {
		t.Skip("requires runner-owned isolated Redis")
	}
	docker := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	port, err := docker("port", name, "6379/tcp")
	if err != nil || !strings.HasPrefix(addr, "127.0.0.1:") || !strings.Contains(port, addr) {
		t.Fatalf("DSN/container mismatch %q %q %v", addr, port, err)
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("auth-redis-crash-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	keys := []store.APIKey{{TenantID: tenant.ID, KeyHash: id + "-a", KeyPrefix: "test"}, {TenantID: tenant.ID, KeyHash: id + "-b", KeyPrefix: "test"}}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}
	r := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond, PoolTimeout: 100 * time.Millisecond, MaxRetries: -1})
	defer r.Close()
	svc := store.NewStore(db, r)
	ctx := context.Background()
	var active, peak atomic.Int32
	held := make(chan struct{})
	release := make(chan struct{})
	var first sync.Once
	gate := newBoundedAuthBulkhead(8, 8, 50*time.Millisecond, func(ctx context.Context, hash string) (*store.APIKey, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		first.Do(func() { close(held) })
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return svc.ValidateAPIKey(ctx, hash)
	})
	c := guardedAgeCache{r: r, ttl: time.Second, fenceTTL: time.Second, validate: gate.validate}
	if out, err := docker("kill", name); err != nil {
		t.Fatalf("kill: %s %v", out, err)
	}
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_, _ = docker("start", name)
		}
	})
	const n = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			k, err := c.lookup(ctx, keys[i%2].KeyHash)
			if err == nil && k == nil {
				err = errors.New("unexpected denial")
			}
			results <- err
		}(i)
	}
	close(start)
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("fallback did not start")
	}
	time.Sleep(400 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	success, busy, other := 0, 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, errAuthBusy) {
			busy++
		} else {
			other++
		}
	}
	if peak.Load() > 8 || success == 0 || busy == 0 || other != 0 || success+busy != n {
		t.Fatalf("success=%d busy=%d other=%d peak=%d", success, busy, other, peak.Load())
	}
	if err := db.Model(&keys[0]).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if out, err := docker("start", name); err != nil {
		t.Fatalf("start: %s %v", out, err)
	}
	restarted = true
	deadline := time.Now().Add(20 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		err = r.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("redis not ready: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if k, err := c.lookup(ctx, keys[0].KeyHash); err != nil || k != nil {
		t.Fatalf("disabled after recovery=%v %v", k, err)
	}
	if k, err := c.lookup(ctx, keys[1].KeyHash); err != nil || k == nil {
		t.Fatalf("active after recovery=%v %v", k, err)
	}
	t.Logf("redis killed; concurrent=%d success=%d busy=%d other=%d peak_mysql_fallback=%d; restarted redis disabled_rejected=true active_accepted=true", n, success, busy, other, peak.Load())
}
