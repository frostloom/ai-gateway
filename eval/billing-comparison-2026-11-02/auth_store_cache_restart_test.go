package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

// The instance that observed Redis failure must not reuse a restored snapshot.
// A fresh instance still demonstrates the cross-instance limitation.
func TestStoreAuthCacheRedisOldSnapshot(t *testing.T) {
	name, addr := os.Getenv("COMPARISON_REDIS_CONTAINER"), os.Getenv("COMPARISON_REDIS_ADDR")
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
		t.Fatalf("container mismatch: %s %s %v", name, addr, err)
	}
	db, _ := isolatedDB(t)
	if err := store.NewStore(db, nil).AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("auth-restart-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: tag, Name: "normal"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond, MaxRetries: -1})
	defer rdb.Close()
	st := store.NewStore(db, rdb)
	const ttl = 10 * time.Second
	st.EnableAuthCache(ttl)
	ctx := context.Background()
	if k, err := st.ValidateAPIKey(ctx, tag); err != nil || k == nil {
		t.Fatalf("warm: %v %v", k, err)
	}
	if err := rdb.Save(ctx).Err(); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if out, err := docker("kill", name); err != nil {
		t.Fatalf("kill: %s %v", out, err)
	}
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_, _ = docker("start", name)
		}
	})
	if err := db.Model(&key).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	failureObservedAt := time.Now()
	if k, err := st.ValidateAPIKey(ctx, tag); err != nil || k != nil {
		t.Fatalf("redis-down authority: %v %v", k, err)
	}
	if out, err := docker("start", name); err != nil {
		t.Fatalf("start: %s %v", out, err)
	}
	restarted = true
	ready := false
	for i := 0; i < 100; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		err := rdb.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("isolated redis did not restart")
	}
	if time.Since(failureObservedAt) >= ttl {
		t.Fatal("restart took too long to exercise pre-expiry snapshot")
	}
	if k, err := st.ValidateAPIKey(ctx, tag); err != nil || k != nil {
		t.Fatalf("instance that observed failure accepted stale snapshot: %v %v", k, err)
	}
	fresh := store.NewStore(db, rdb)
	fresh.EnableAuthCache(ttl)
	if k, err := fresh.ValidateAPIKey(ctx, tag); err != nil || k == nil {
		t.Fatalf("expected unprotected fresh instance to observe stale key: %v %v", k, err)
	}
	t.Log("same instance rejected restored stale key; fresh instance still accepted it")
}
