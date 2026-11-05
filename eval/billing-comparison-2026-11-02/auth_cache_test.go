package comparison

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

func TestAuthCacheBoundaries(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr(), DialTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond, MaxRetries: -1})
	defer r.Close()
	now := time.Now()
	expires := now.Add(100 * time.Millisecond)
	current := &store.APIKey{ID: 4, TenantID: 7, KeyHash: "hash", ExpiresAt: &expires}
	calls := 0
	validate := func(context.Context, string) (*store.APIKey, error) {
		calls++
		if current == nil {
			return nil, nil
		}
		v := *current
		if v.ExpiresAt != nil && !time.Now().Before(*v.ExpiresAt) {
			return nil, nil
		}
		return &v, nil
	}
	c := authCache{r: r, validate: validate, ttl: time.Second}
	ctx := context.Background()
	if k, e := c.lookup(ctx, "hash"); e != nil || k == nil || calls != 1 {
		t.Fatalf("initial %v %v calls %d", k, e, calls)
	}
	if k, e := c.lookup(ctx, "hash"); e != nil || k == nil || calls != 1 {
		t.Fatalf("cache %v %v calls %d", k, e, calls)
	}
	time.Sleep(120 * time.Millisecond)
	if k, e := c.lookup(ctx, "hash"); e != nil || k != nil {
		t.Fatalf("expired key accepted: %v %v", k, e)
	}
	// No guaranteed revocation without coherent invalidation: bounded stale acceptance is a documented observation.
	current = &store.APIKey{ID: 4, TenantID: 7, KeyHash: "revoked"}
	c.ttl = 100 * time.Millisecond
	if _, e := c.lookup(ctx, "revoked"); e != nil {
		t.Fatal(e)
	}
	current = nil
	if k, e := c.lookup(ctx, "revoked"); e != nil || k == nil {
		t.Fatalf("expected bounded staleness: %v %v", k, e)
	}
	time.Sleep(120 * time.Millisecond)
	if k, e := c.lookup(ctx, "revoked"); e != nil || k != nil {
		t.Fatalf("revoked after TTL: %v %v", k, e)
	}
	current = &store.APIKey{ID: 6, TenantID: 7, KeyHash: "browser", Name: "__browser_session"}
	before := calls
	for i := 0; i < 2; i++ {
		if k, e := c.lookup(ctx, "browser"); e != nil || k == nil {
			t.Fatal(k, e)
		}
	}
	if calls != before+2 {
		t.Fatalf("browser session must bypass cache: %d", calls-before)
	}
	m.Close()
	current = &store.APIKey{ID: 9, TenantID: 7, KeyHash: "offline"}
	if k, e := c.lookup(ctx, "offline"); e != nil || k == nil {
		t.Fatalf("redis down fallback: %v %v", k, e)
	}
	c.validate = func(context.Context, string) (*store.APIKey, error) { return nil, errors.New("authority offline") }
	if k, e := c.lookup(ctx, "offline"); e == nil || k != nil {
		t.Fatalf("authority failure accepted: %v %v", k, e)
	}
}

// A read that began before revocation can refill after invalidation. This test
// documents the unresolved race; it is not a proposed security guarantee.
func TestAuthCacheStaleRefill(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	started, release := make(chan struct{}), make(chan struct{})
	old := &store.APIKey{ID: 10, TenantID: 10, KeyHash: "race"}
	calls := 0
	c := authCache{r: r, ttl: time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
		calls++
		if calls == 1 {
			close(started)
			<-release
			return old, nil
		}
		return nil, nil
	}}
	result := make(chan error, 1)
	go func() { _, err := c.lookup(context.Background(), "race"); result <- err }()
	<-started
	if err := r.Del(context.Background(), "auth-test:race").Err(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	got, err := c.lookup(context.Background(), "race")
	if err != nil || got == nil {
		t.Fatalf("expected to observe stale refill, got=%v err=%v", got, err)
	}
	if calls != 1 {
		t.Fatalf("expected stale cache hit, calls=%d", calls)
	}
}
