package store

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	"github.com/redis/go-redis/v9"
)

const authCachePrefix = "billing:auth:v1:"
const authCacheIOTimeout = 30 * time.Millisecond

type cachedAPIKey struct {
	Key      APIKey    `json:"key"`
	CachedAt time.Time `json:"cached_at"`
	Deadline time.Time `json:"deadline"`
}

type authCacheState struct {
	bypassUntil atomic.Pointer[time.Time]
}

func (s *Store) authCacheBypassed() bool {
	if s.authCacheState == nil {
		return false
	}
	until := s.authCacheState.bypassUntil.Load()
	return until != nil && time.Now().Before(*until)
}

func (s *Store) bypassAuthCacheAfterError() {
	if s.authCacheState == nil {
		return
	}
	until := time.Now().Add(s.authCacheTTL)
	for {
		previous := s.authCacheState.bypassUntil.Load()
		if previous != nil && !previous.Before(until) {
			return
		}
		if s.authCacheState.bypassUntil.CompareAndSwap(previous, &until) {
			return
		}
	}
}

// EnableAuthCache is a startup-only opt-in. Revocation is not immediate:
// cached ordinary keys can remain valid until the bounded TTL expires.
// Browser sessions always use the database.
func (s *Store) EnableAuthCache(ttl time.Duration) {
	if ttl > 0 && ttl <= time.Minute && s.rdb != nil {
		s.authCacheTTL = ttl
		s.authCacheState = &authCacheState{}
	}
}

func (s *Store) ValidateAPIKey(ctx context.Context, keyHash string) (*APIKey, error) {
	if s.authCacheTTL <= 0 || s.rdb == nil {
		return s.validateAPIKeyAuthority(ctx, keyHash)
	}
	if s.authCacheBypassed() {
		metrics.BillingAuthCacheRead("bypass")
		return s.validateAPIKeyAuthority(ctx, keyHash)
	}
	// Start the TTL before the authoritative read so a slow read cannot
	// extend the cache lifetime after a concurrent state change.
	started := time.Now()
	deadline := started.Add(s.authCacheTTL)
	cacheCtx, cancel := context.WithTimeout(ctx, authCacheIOTimeout)
	data, cacheErr := s.rdb.Get(cacheCtx, authCachePrefix+keyHash).Bytes()
	cancel()
	if cacheErr == nil {
		var entry cachedAPIKey
		now := time.Now()
		if json.Unmarshal(data, &entry) == nil && entry.Key.KeyHash == keyHash && entry.Key.ID > 0 && entry.Key.TenantID > 0 && entry.Key.Status == 0 && entry.Key.Name != browserKeyName && !entry.CachedAt.After(now) && !entry.CachedAt.IsZero() && now.Sub(entry.CachedAt) < s.authCacheTTL && now.Before(entry.Deadline) && (entry.Key.ExpiresAt == nil || now.Before(*entry.Key.ExpiresAt)) && !s.authCacheBypassed() {
			metrics.BillingAuthCacheRead("hit")
			return &entry.Key, nil
		}
		metrics.BillingAuthCacheRead("invalid")
	} else if cacheErr == redis.Nil {
		metrics.BillingAuthCacheRead("miss")
	} else {
		metrics.BillingAuthCacheRead("error")
		if ctx.Err() == nil {
			s.bypassAuthCacheAfterError()
		}
	}
	key, err := s.validateAPIKeyAuthority(ctx, keyHash)
	if err != nil || key == nil || key.Name == browserKeyName {
		return key, err
	}
	if key.ExpiresAt != nil && key.ExpiresAt.Before(deadline) {
		deadline = *key.ExpiresAt
	}
	if ttl := time.Until(deadline); ttl >= time.Millisecond && !s.authCacheBypassed() {
		if encoded, err := json.Marshal(cachedAPIKey{Key: *key, CachedAt: started, Deadline: deadline}); err == nil {
			cacheCtx, cancel = context.WithTimeout(ctx, authCacheIOTimeout)
			// Avoid extending an existing entry when concurrent misses finish.
			setErr := s.rdb.SetNX(cacheCtx, authCachePrefix+keyHash, encoded, ttl).Err()
			cancel()
			if setErr != nil && ctx.Err() == nil {
				s.bypassAuthCacheAfterError()
			}
		}
	}
	return key, nil
}
