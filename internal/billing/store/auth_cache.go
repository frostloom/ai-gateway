package store

import (
	"context"
	"encoding/json"
	"sync"
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
	memory      sync.Map
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

// EnableAuthCache keeps ordinary keys in this process until the bounded TTL.
// Revocation is not immediate. Browser sessions always use the database.
// The billing process defaults this on for 60 seconds; explicit 0 disables it.
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
	if key, ok := s.loadMemoryAuth(keyHash, started); ok {
		metrics.BillingAuthCacheRead("hit")
		return key, nil
	}
	deadline := started.Add(s.authCacheTTL)
	cacheCtx, cancel := context.WithTimeout(ctx, authCacheIOTimeout)
	data, cacheErr := s.rdb.Get(cacheCtx, authCachePrefix+keyHash).Bytes()
	cancel()
	if cacheErr == nil {
		var entry cachedAPIKey
		if json.Unmarshal(data, &entry) == nil && entry.Key.KeyHash == keyHash && s.authCacheFresh(&entry, time.Now()) {
			s.storeMemoryAuth(keyHash, entry)
			metrics.BillingAuthCacheRead("hit")
			cp := entry.Key
			return &cp, nil
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
		entry := cachedAPIKey{Key: *key, CachedAt: started, Deadline: deadline}
		s.storeMemoryAuth(key.KeyHash, entry)
		if encoded, err := json.Marshal(entry); err == nil {
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

func (s *Store) authCacheFresh(entry *cachedAPIKey, now time.Time) bool {
	if entry == nil || s.authCacheBypassed() {
		return false
	}
	key := entry.Key
	return key.ID > 0 && key.TenantID > 0 && key.Status == 0 && key.Name != browserKeyName && !entry.CachedAt.After(now) && !entry.CachedAt.IsZero() && now.Sub(entry.CachedAt) < s.authCacheTTL && now.Before(entry.Deadline) && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt))
}

func (s *Store) loadMemoryAuth(keyHash string, now time.Time) (*APIKey, bool) {
	if s.authCacheState == nil {
		return nil, false
	}
	value, ok := s.authCacheState.memory.Load(keyHash)
	if !ok {
		return nil, false
	}
	entry := value.(*cachedAPIKey)
	if entry.Key.KeyHash != keyHash || !s.authCacheFresh(entry, now) {
		s.authCacheState.memory.Delete(keyHash)
		return nil, false
	}
	cp := entry.Key
	return &cp, true
}

func (s *Store) storeMemoryAuth(keyHash string, entry cachedAPIKey) {
	if s.authCacheState == nil || keyHash == "" || entry.Key.Name == browserKeyName {
		return
	}
	saved := entry
	s.authCacheState.memory.Store(keyHash, &saved)
}
