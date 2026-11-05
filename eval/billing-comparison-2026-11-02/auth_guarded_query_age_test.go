package comparison

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm/logger"
)

// A lookup that began before a change may return to its caller, but it must
// not populate a fresh full-TTL cache after its own TTL has elapsed.
func TestGuardedSlowLookupDoesNotRefreshStaleTTL(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	c := guardedAgeCache{r: r, ttl: 100 * time.Millisecond, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return &store.APIKey{ID: 1, TenantID: 2, KeyHash: "delayed"}, nil
		}
		return nil, nil
	}}
	first := make(chan error, 1)
	go func() { _, err := c.lookup(context.Background(), "delayed"); first <- err }()
	<-started
	time.Sleep(150 * time.Millisecond)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if exists, err := r.Exists(context.Background(), "auth-guard:delayed").Result(); err != nil || exists != 0 {
		t.Fatalf("old lookup populated cache after TTL: exists=%d err=%v", exists, err)
	}
	got, err := c.lookup(context.Background(), "delayed")
	if err != nil || got != nil || calls.Load() != 2 {
		t.Fatalf("subsequent lookup got=%v err=%v calls=%d", got, err, calls.Load())
	}
}

func TestGuardedSlowLookupUsesRemainingTTL(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	var calls atomic.Int32
	c := guardedAgeCache{r: r, ttl: 1500 * time.Millisecond, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
		calls.Add(1)
		time.Sleep(300 * time.Millisecond)
		return &store.APIKey{ID: 1, TenantID: 2, KeyHash: "remaining"}, nil
	}}
	if k, err := c.lookup(context.Background(), "remaining"); err != nil || k == nil {
		t.Fatalf("initial=%v err=%v", k, err)
	}
	ttl, err := r.PTTL(context.Background(), "auth-guard:remaining").Result()
	if err != nil || ttl <= 0 || ttl > 1300*time.Millisecond {
		t.Fatalf("remaining ttl=%v err=%v", ttl, err)
	}
	if k, err := c.lookup(context.Background(), "remaining"); err != nil || k == nil || calls.Load() != 1 {
		t.Fatalf("cache hit=%v err=%v calls=%d", k, err, calls.Load())
	}
	after, err := r.PTTL(context.Background(), "auth-guard:remaining").Result()
	if err != nil || after > ttl {
		t.Fatalf("hit refreshed TTL: before=%v after=%v err=%v", ttl, after, err)
	}
}

func TestGuardedAgedLookupRealStoreAfterDisable(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("age-real-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: id, KeyPrefix: "test"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	c := guardedAgeCache{r: r, ttl: 100 * time.Millisecond, fenceTTL: time.Second, validate: func(ctx context.Context, hash string) (*store.APIKey, error) {
		got, err := svc.ValidateAPIKey(ctx, hash)
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return got, err
	}}
	first := make(chan error, 1)
	go func() {
		k, err := c.lookup(ctx, id)
		if err == nil && k == nil {
			err = fmt.Errorf("first read unexpectedly denied")
		}
		first <- err
	}()
	<-started
	if err := db.Model(&key).Update("status", 1).Error; err != nil {
		close(release)
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if n, err := r.Exists(ctx, "auth-guard:"+id).Result(); err != nil || n != 0 {
		t.Fatalf("stale cache exists=%d err=%v", n, err)
	}
	k, err := c.lookup(ctx, id)
	if err != nil || k != nil || calls.Load() != 2 {
		t.Fatalf("after disable=%v err=%v calls=%d", k, err, calls.Load())
	}
	t.Log("first lookup read before disable, released after TTL; subsequent lookup rejected and no stale cache inserted")
}

// PASS reproduces a stale authorization after the fence expires before commit.
func TestGuardedAgeLongMutationCanRepublishStaleReal(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("age-mutation-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: id, KeyPrefix: "test"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	c := guardedAgeCache{r: r, ttl: time.Second, fenceTTL: 150 * time.Millisecond, validate: svc.ValidateAPIKey}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		t.Fatalf("warm=%v err=%v", k, err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.mutate(ctx, id, func() error { close(started); <-release; return db.Model(&key).Update("status", 1).Error })
	}()
	<-started
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := r.Exists(ctx, "auth-guard-fence:"+id).Result()
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("fence did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		close(release)
		t.Fatalf("old read=%v err=%v", k, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if k, err := svc.ValidateAPIKey(ctx, id); err != nil || k != nil {
		t.Fatalf("authority=%v err=%v", k, err)
	}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		t.Fatalf("expected stale cache=%v err=%v", k, err)
	}
	expired := time.Now().Add(3 * time.Second)
	for {
		k, err := c.lookup(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if k == nil {
			break
		}
		if time.Now().After(expired) {
			t.Fatal("stale key still accepted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Log("fence expired before disable commit; new request accepted stale cached key after commit; after cache expiry rejected")
}

// Verifies a test-only post-commit invalidation after a long write.
func TestGuardedPostCommitInvalidationReal(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("age-postcommit-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: id, KeyPrefix: "test"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	c := guardedPostCommitCache{guardedAgeCache{r: r, ttl: time.Second, fenceTTL: 150 * time.Millisecond, validate: svc.ValidateAPIKey}}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		t.Fatalf("warm=%v err=%v", k, err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.mutate(ctx, id, func() error { close(started); <-release; return db.Model(&key).Update("status", 1).Error })
	}()
	<-started
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := r.Exists(ctx, "auth-guard-fence:"+id).Result()
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("fence did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		close(release)
		t.Fatalf("old read=%v err=%v", k, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if k, err := svc.ValidateAPIKey(ctx, id); err != nil || k != nil {
		t.Fatalf("authority=%v err=%v", k, err)
	}
	if k, err := c.lookup(ctx, id); err != nil || k != nil {
		t.Fatalf("post-commit stale cache accepted=%v err=%v", k, err)
	}
	t.Log("long write completed; post-commit invalidation removed stale cache and lookup rejected")
}

// PASS means the controlled commit-to-invalidation race was reproduced.
func TestGuardedPostCommitGapStillAcceptsStaleReal(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.APIKey{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("age-postgap-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: id, KeyPrefix: "test"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	svc := store.NewStore(db, r)
	c := guardedPostCommitCache{guardedAgeCache{r: r, ttl: time.Second, fenceTTL: 150 * time.Millisecond, validate: svc.ValidateAPIKey}}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		t.Fatalf("warm=%v err=%v", k, err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	committed, postRelease := make(chan struct{}), make(chan struct{})
	go func() {
		done <- c.mutate(ctx, id, func() error {
			close(started)
			<-release
			if err := db.Model(&key).Update("status", 1).Error; err != nil {
				close(committed)
				return err
			}
			close(committed)
			<-postRelease
			return nil
		})
	}()
	<-started
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := r.Exists(ctx, "auth-guard-fence:"+id).Result()
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("fence did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		close(release)
		t.Fatalf("old read=%v err=%v", k, err)
	}
	close(release)
	<-committed
	if k, err := svc.ValidateAPIKey(ctx, id); err != nil || k != nil {
		close(postRelease)
		t.Fatalf("authority=%v err=%v", k, err)
	}
	if k, err := c.lookup(ctx, id); err != nil || k == nil {
		close(postRelease)
		t.Fatalf("expected stale during post-commit gap=%v err=%v", k, err)
	}
	close(postRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if k, err := c.lookup(ctx, id); err != nil || k != nil {
		t.Fatalf("post-commit stale cache accepted=%v err=%v", k, err)
	}
	t.Log("commit-to-invalidation gap accepted stale key; after invalidation rejected")
}
