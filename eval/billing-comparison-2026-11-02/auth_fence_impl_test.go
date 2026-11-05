package comparison

import (
	"context"
	"encoding/json"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

// Test-only model of the fixed-duration fence; not a production design.
type fencedAuthCache struct {
	r             *redis.Client
	ttl, fenceTTL time.Duration
	validate      func(context.Context, string) (*store.APIKey, error)
}

func (c *fencedAuthCache) invalidate(ctx context.Context, hash string) error {
	if err := c.r.Set(ctx, "auth-fence:"+hash, "1", c.fenceTTL).Err(); err != nil {
		return err
	}
	return c.r.Del(ctx, "auth-fenced:"+hash).Err()
}
func (c *fencedAuthCache) lookup(ctx context.Context, hash string) (*store.APIKey, error) {
	cacheCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	data, err := c.r.Get(cacheCtx, "auth-fenced:"+hash).Bytes()
	cancel()
	if err == nil {
		var e authEntry
		if json.Unmarshal(data, &e) == nil && e.Key.KeyHash == hash && e.Key.ID > 0 && e.Key.TenantID > 0 && e.Key.Status == 0 && time.Now().Before(e.Deadline) && (e.Key.ExpiresAt == nil || time.Now().Before(*e.Key.ExpiresAt)) && e.Key.Name != "__browser_session" {
			return &e.Key, nil
		}
	}
	k, err := c.validate(ctx, hash)
	if err != nil || k == nil {
		return k, err
	}
	if k.Name == "__browser_session" {
		return k, nil
	}
	deadline := time.Now().Add(c.ttl)
	if k.ExpiresAt != nil && k.ExpiresAt.Before(deadline) {
		deadline = *k.ExpiresAt
	}
	ttl := time.Until(deadline)
	if ttl > 0 {
		data, err = json.Marshal(authEntry{Key: *k, Deadline: deadline})
		if err == nil {
			const script = `if redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
if redis.call('EXISTS', KEYS[1]) == 1 then return 2 end
redis.call('PSETEX', KEYS[1], ARGV[2], ARGV[1]); return 1`
			cacheCtx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
			_ = c.r.Eval(cacheCtx, script, []string{"auth-fenced:" + hash, "auth-fence:" + hash}, data, ttl.Milliseconds()).Err()
			cancel()
		}
	}
	return k, nil
}
