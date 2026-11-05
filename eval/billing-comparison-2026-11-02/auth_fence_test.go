package comparison

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

func TestFenceBlocksStaleRefill(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "within_fence"
		if expired {
			name = "fence_expired"
		}
		t.Run(name, func(t *testing.T) {
			m := miniredis.RunT(t)
			r := redis.NewClient(&redis.Options{Addr: m.Addr()})
			defer r.Close()
			started, release := make(chan struct{}), make(chan struct{})
			c := fencedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
				close(started)
				<-release
				return &store.APIKey{ID: 1, TenantID: 2, KeyHash: "test"}, nil
			}}
			result := make(chan *store.APIKey, 1)
			go func() { k, _ := c.lookup(context.Background(), "test"); result <- k }()
			<-started
			if err := c.invalidate(context.Background(), "test"); err != nil {
				t.Fatal(err)
			}
			if expired {
				m.FastForward(11 * time.Second)
			}
			close(release)
			if <-result == nil {
				t.Fatal("in-flight stale DB read was unexpectedly rejected")
			}
			exists, err := r.Exists(context.Background(), "auth-fenced:test").Result()
			if err != nil {
				t.Fatal(err)
			}
			if !expired && exists != 0 {
				t.Fatal("old snapshot published during active fence")
			}
			if expired && exists != 1 {
				t.Fatal("expected stale publication after timed fence expiry")
			}
		})
	}
}

func TestFenceRedisFailure(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr(), DialTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond, MaxRetries: -1})
	defer r.Close()
	c := fencedAuthCache{r: r, ttl: time.Second, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) { return nil, nil }}
	m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.invalidate(ctx, "test"); err == nil {
		t.Fatal("expected Redis failure")
	}
	if k, err := c.lookup(ctx, "test"); err != nil || k != nil {
		t.Fatalf("expected DB fallback: %v %v", k, err)
	}
}

func TestFenceStateLossAllowsStaleRefill(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	started, release := make(chan struct{}), make(chan struct{})
	c := fencedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
		close(started)
		<-release
		return &store.APIKey{ID: 1, TenantID: 2, KeyHash: "test"}, nil
	}}
	done := make(chan struct{})
	go func() { _, _ = c.lookup(context.Background(), "test"); close(done) }()
	<-started
	if err := c.invalidate(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	// Emulates loss of volatile Redis state, not a full process failover test.
	m.FlushAll()
	close(release)
	<-done
	exists, err := r.Exists(context.Background(), "auth-fenced:test").Result()
	if err != nil || exists != 1 {
		t.Fatalf("expected stale refill after lost fence, exists=%d err=%v", exists, err)
	}
}

// faultDel simulates a transient partial Redis failure: SET fence succeeds,
// DEL fails, then later reads are available again.
type faultDel struct{}

func (faultDel) DialHook(next redis.DialHook) redis.DialHook { return next }
func (faultDel) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (faultDel) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "del" {
			return fmt.Errorf("injected DEL failure")
		}
		return next(ctx, cmd)
	}
}
func TestFenceBestEffortDeleteFailure(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	ctx := context.Background()
	var current *store.APIKey = &store.APIKey{ID: 2, TenantID: 3, KeyHash: "partial"}
	c := fencedAuthCache{r: r, ttl: time.Minute, fenceTTL: 10 * time.Second, validate: func(context.Context, string) (*store.APIKey, error) { return current, nil }}
	if k, e := c.lookup(ctx, "partial"); e != nil || k == nil {
		t.Fatalf("warm: %v %v", k, e)
	}
	r.AddHook(faultDel{})
	if err := c.invalidate(ctx, "partial"); err == nil {
		t.Fatal("expected DEL error")
	}
	current = nil // Model a best-effort caller which commits revocation after invalidation failure.
	if k, e := c.lookup(ctx, "partial"); e != nil || k == nil {
		t.Fatalf("expected stale cache hit after DEL failure: %v %v", k, e)
	}
}
