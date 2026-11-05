package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

// Test-only query-age variant; preserves the other guarded-cache limitations.
type guardedAgeCache struct {
	r             *redis.Client
	ttl, fenceTTL time.Duration
	validate      func(context.Context, string) (*store.APIKey, error)
}

func (c *guardedAgeCache) mutate(ctx context.Context, hash string, write func() error) error {
	const invalidate = `redis.call('PSETEX', KEYS[2], ARGV[1], '1'); redis.call('DEL', KEYS[1]); return 1`
	if err := c.r.Eval(ctx, invalidate, []string{"auth-guard:" + hash, "auth-guard-fence:" + hash}, c.fenceTTL.Milliseconds()).Err(); err != nil {
		return err
	}
	return write()
}
func (c *guardedAgeCache) lookup(ctx context.Context, hash string) (*store.APIKey, error) {
	const read = `if redis.call('EXISTS', KEYS[2]) == 1 then return false end; return redis.call('GET', KEYS[1])`
	cacheCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	value, err := c.r.Eval(cacheCtx, read, []string{"auth-guard:" + hash, "auth-guard-fence:" + hash}).Text()
	data := []byte(value)
	cancel()
	if err == nil {
		var e authEntry
		if json.Unmarshal(data, &e) == nil && e.Key.KeyHash == hash && e.Key.ID > 0 && e.Key.TenantID > 0 && e.Key.Status == 0 && time.Now().Before(e.Deadline) && (e.Key.ExpiresAt == nil || time.Now().Before(*e.Key.ExpiresAt)) && e.Key.Name != "__browser_session" {
			return &e.Key, nil
		}
	}
	// A failed cache read is already evidence of an outage; avoid another
	// bounded Redis operation before returning the authoritative result.
	if err != nil && !errors.Is(err, redis.Nil) {
		return c.validate(ctx, hash)
	}
	queryStarted := time.Now()
	k, err := c.validate(ctx, hash)
	if err != nil || k == nil {
		return k, err
	}
	if k.Name == "__browser_session" {
		return k, nil
	}
	deadline := queryStarted.Add(c.ttl)
	if k.ExpiresAt != nil && k.ExpiresAt.Before(deadline) {
		deadline = *k.ExpiresAt
	}
	ttl := time.Until(deadline)
	if ttl > 0 {
		data, err = json.Marshal(authEntry{Key: *k, Deadline: deadline})
		if err == nil {
			const fill = `if redis.call('EXISTS', KEYS[2]) == 1 then return 0 end; if redis.call('EXISTS', KEYS[1]) == 1 then return 2 end; redis.call('PSETEX', KEYS[1], ARGV[2], ARGV[1]); return 1`
			cacheCtx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
			_ = c.r.Eval(cacheCtx, fill, []string{"auth-guard:" + hash, "auth-guard-fence:" + hash}, data, ttl.Milliseconds()).Err()
			cancel()
		}
	}
	return k, nil
}
