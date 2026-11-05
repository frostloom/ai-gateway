package comparison

import (
	"context"
	"encoding/json"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

// Test-only bounded-staleness prototype. No durable invalidation or stale-fill fence.
type authCache struct {
	r        *redis.Client
	validate func(context.Context, string) (*store.APIKey, error)
	ttl      time.Duration
}
type authEntry struct {
	Key      store.APIKey
	Deadline time.Time
}

func (c *authCache) lookup(ctx context.Context, hash string) (*store.APIKey, error) {
	cacheCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	data, err := c.r.Get(cacheCtx, "auth-test:"+hash).Bytes()
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
		data, err := json.Marshal(authEntry{Key: *k, Deadline: deadline})
		if err == nil {
			cacheCtx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
			_ = c.r.Set(cacheCtx, "auth-test:"+hash, data, ttl).Err()
			cancel()
		}
	}
	return k, nil
}
