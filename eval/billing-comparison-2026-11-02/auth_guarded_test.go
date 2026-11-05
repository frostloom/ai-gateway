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

func TestGuardedCacheMutationFailureDoesNotCommit(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	ctx := context.Background()
	current := &store.APIKey{ID: 1, TenantID: 2, KeyHash: "guard"}
	c := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) { return current, nil }}
	if _, e := c.lookup(ctx, "guard"); e != nil {
		t.Fatal(e)
	}
	r.AddHook(faultGuardEval{})
	commits := 0
	err := c.mutate(ctx, "guard", func() error { commits++; current = nil; return nil })
	if err == nil || commits != 0 || current == nil {
		t.Fatalf("failed invalidation must prevent DB mutation: %v commits=%d", err, commits)
	}
}

type faultGuardEval struct{}

func (faultGuardEval) DialHook(next redis.DialHook) redis.DialHook { return next }
func (faultGuardEval) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (faultGuardEval) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "eval" {
			return errors.New("injected redis failure")
		}
		return next(ctx, cmd)
	}
}

func TestGuardedCacheFenceRejectsExistingValue(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	ctx := context.Background()
	current := &store.APIKey{ID: 1, TenantID: 2, KeyHash: "guard"}
	calls := 0
	c := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) { calls++; return current, nil }}
	if _, e := c.lookup(ctx, "guard"); e != nil {
		t.Fatal(e)
	}
	// A surviving fence combined with a surviving cache entry can result from
	// a previous invalidation implementation's separate SET and failed DEL.
	if e := r.Set(ctx, "auth-guard-fence:guard", "1", time.Second).Err(); e != nil {
		t.Fatal(e)
	}
	current = nil
	k, e := c.lookup(ctx, "guard")
	if e != nil || k != nil || calls != 2 {
		t.Fatalf("fenced entry accepted: %v %v calls=%d", k, e, calls)
	}
}

func TestGuardedSlowMutationCanRepublishStale(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	ctx := context.Background()
	current := &store.APIKey{ID: 1, TenantID: 2, KeyHash: "slow"}
	c := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) { return current, nil }}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.mutate(ctx, "slow", func() error { close(started); <-release; current = nil; return nil })
	}()
	<-started
	m.FastForward(2 * time.Second)
	if k, e := c.lookup(ctx, "slow"); e != nil || k == nil {
		t.Fatalf("expected old DB state during delayed mutation: %v %v", k, e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	k, e := c.lookup(ctx, "slow")
	if e != nil || k == nil {
		t.Fatalf("expected stale cache after delayed DB commit: %v %v", k, e)
	}
}

type countFailedEval struct{ calls *int }

func (h countFailedEval) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h countFailedEval) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h countFailedEval) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "eval" {
			*h.calls++
			return errors.New("redis unavailable")
		}
		return next(ctx, cmd)
	}
}
func TestGuardedUnavailableSkipsRefill(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	calls := 0
	r.AddHook(countFailedEval{calls: &calls})
	c := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
		return &store.APIKey{ID: 1, TenantID: 2, KeyHash: "offline"}, nil
	}}
	if k, e := c.lookup(context.Background(), "offline"); e != nil || k == nil {
		t.Fatal(k, e)
	}
	if calls != 1 {
		t.Fatalf("failed Redis read should not attempt refill: calls=%d", calls)
	}
}
