package store

import (
	"context"
	"encoding/json"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"sync"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
)

const authCachePrefix = "billing:auth:v1:"
const authRevisionPrefix = "billing:auth:revision:"

// Redis 平时远快于这个上限。上限只在 Redis 卡住时生效，到点改查数据库，不关闭缓存。
const authCacheIOTimeout = time.Second

type cachedAPIKey struct {
	Revision string    `json:"revision"`
	Key      APIKey    `json:"key"`
	CachedAt time.Time `json:"cached_at"`
	Deadline time.Time `json:"deadline"`
}

type authLookup func(context.Context, string) (*APIKey, error)

type authCall struct {
	done sync.WaitGroup
	key  *APIKey
	err  error
}

type authCacheState struct {
	memory sync.Map
	mu     sync.Mutex
	calls  map[string]*authCall
}

// EnableAuthCache keeps ordinary keys in this process until the bounded TTL.
// Disable and delete call ForgetAuthCache so the old copy is not reused.
// Browser sessions always use the database.
// The billing process defaults this on for 60 seconds; explicit 0 disables it.
func (s *Store) EnableAuthCache(ttl time.Duration) {
	if ttl > 0 && ttl <= time.Minute && s.rdb != nil {
		s.authCacheTTL = ttl
		s.authCacheState = &authCacheState{calls: map[string]*authCall{}}
	}
}

func (s *Store) ValidateAPIKey(ctx context.Context, keyHash string) (*APIKey, error) {
	if s.authCacheTTL <= 0 || s.rdb == nil || s.authCacheState == nil {
		return s.validateAPIKeyAuthority(ctx, keyHash)
	}
	return s.cachedAPIKey(ctx, keyHash, s.validateAPIKeyAuthority)
}

func (s *Store) cachedAPIKey(ctx context.Context, keyHash string, lookup authLookup) (*APIKey, error) {
	revision, err := s.authRevision(ctx, keyHash)
	if err != nil {
		return lookup(ctx, keyHash)
	}
	callID := keyHash + "\x00" + revision
	if key, ok := s.loadMemoryAuth(keyHash, revision, time.Now()); ok {
		metrics.BillingAuthCacheRead("hit")
		return key, nil
	}
	state := s.authCacheState
	state.mu.Lock()
	if call, ok := state.calls[callID]; ok {
		state.mu.Unlock()
		call.done.Wait()
		return copyAPIKey(call.key), call.err
	}
	call := &authCall{}
	call.done.Add(1)
	state.calls[callID] = call
	state.mu.Unlock()

	call.key, call.err = s.fillAuthCache(ctx, keyHash, revision, lookup)
	if current, err := s.authRevision(ctx, keyHash); err != nil || current != revision {
		call.key = nil
		call.err = err
	}
	call.done.Done()

	state.mu.Lock()
	delete(state.calls, callID)
	state.mu.Unlock()
	return copyAPIKey(call.key), call.err
}

// fillAuthCache 只有同一个钥匙的第一个请求会查数据库。写回内存和 Redis 后，等待中的请求拿到同一份结果。
func (s *Store) fillAuthCache(ctx context.Context, keyHash, revision string, lookup authLookup) (*APIKey, error) {
	started := time.Now()
	if key, ok := s.loadMemoryAuth(keyHash, revision, started); ok {
		metrics.BillingAuthCacheRead("hit")
		return key, nil
	}
	deadline := started.Add(s.authCacheTTL)
	if key, ok := s.loadRedisAuth(ctx, keyHash, revision); ok {
		metrics.BillingAuthCacheRead("hit")
		return key, nil
	}
	key, err := lookup(ctx, keyHash)
	if err != nil || key == nil || key.Name == browserKeyName {
		if err == nil {
			metrics.BillingAuthCacheRead("miss")
		}
		return key, err
	}
	if key.ExpiresAt != nil && key.ExpiresAt.Before(deadline) {
		deadline = *key.ExpiresAt
	}
	ttl := time.Until(deadline)
	if ttl < time.Millisecond {
		metrics.BillingAuthCacheRead("miss")
		return key, nil
	}
	entry := cachedAPIKey{Revision: revision, Key: *key, CachedAt: started, Deadline: deadline}
	s.storeMemoryAuth(key.KeyHash, entry)
	s.storeRedisAuth(ctx, key.KeyHash, entry, ttl)
	metrics.BillingAuthCacheRead("miss")
	return key, nil
}

func (s *Store) loadRedisAuth(ctx context.Context, keyHash, revision string) (*APIKey, bool) {
	cacheCtx, cancel := context.WithTimeout(ctx, authCacheIOTimeout)
	data, err := s.rdb.Get(cacheCtx, authCachePrefix+keyHash).Bytes()
	cancel()
	if err != nil {
		return nil, false
	}
	var entry cachedAPIKey
	if json.Unmarshal(data, &entry) != nil || entry.Key.KeyHash != keyHash || entry.Revision != revision || !s.authCacheFresh(&entry, time.Now()) {
		return nil, false
	}
	s.storeMemoryAuth(keyHash, entry)
	return copyAPIKey(&entry.Key), true
}

func (s *Store) storeRedisAuth(ctx context.Context, keyHash string, entry cachedAPIKey, ttl time.Duration) {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	cacheCtx, cancel := context.WithTimeout(ctx, authCacheIOTimeout)
	_ = s.rdb.Set(cacheCtx, authCachePrefix+keyHash, encoded, ttl).Err()
	cancel()
}

func (s *Store) authCacheFresh(entry *cachedAPIKey, now time.Time) bool {
	if entry == nil {
		return false
	}
	key := entry.Key
	return key.ID > 0 && key.TenantID > 0 && key.Status == 0 && key.Name != browserKeyName && !entry.CachedAt.After(now) && !entry.CachedAt.IsZero() && now.Sub(entry.CachedAt) < s.authCacheTTL && now.Before(entry.Deadline) && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt))
}

func (s *Store) loadMemoryAuth(keyHash, revision string, now time.Time) (*APIKey, bool) {
	if s.authCacheState == nil {
		return nil, false
	}
	value, ok := s.authCacheState.memory.Load(keyHash)
	if !ok {
		return nil, false
	}
	entry := value.(*cachedAPIKey)
	if entry.Key.KeyHash != keyHash || entry.Revision != revision || !s.authCacheFresh(entry, now) {
		s.authCacheState.memory.Delete(keyHash)
		return nil, false
	}
	return copyAPIKey(&entry.Key), true
}

// ForgetAuthCache drops one key from this process and from Redis.
// authRevision is checked on every lookup so all replicas observe completed revocations.
func (s *Store) authRevision(ctx context.Context, keyHash string) (string, error) {
	cacheCtx, cancel := context.WithTimeout(ctx, authCacheIOTimeout)
	defer cancel()
	revision, err := s.rdb.Get(cacheCtx, authRevisionPrefix+keyHash).Result()
	if errors.Is(err, goredis.Nil) {
		return "0", nil
	}
	return revision, err
}

func (s *Store) ForgetAuthCache(ctx context.Context, keyHash string) error {
	if keyHash == "" {
		return nil
	}
	if s.authCacheState != nil {
		s.authCacheState.memory.Delete(keyHash)
	}
	if s.rdb == nil {
		return nil
	}
	cacheCtx, cancel := context.WithTimeout(ctx, authCacheIOTimeout)
	defer cancel()
	_, err := s.rdb.TxPipelined(cacheCtx, func(pipe goredis.Pipeliner) error {
		pipe.Incr(cacheCtx, authRevisionPrefix+keyHash)
		pipe.Del(cacheCtx, authCachePrefix+keyHash)
		return nil
	})
	return err
}

func (s *Store) storeMemoryAuth(keyHash string, entry cachedAPIKey) {
	if s.authCacheState == nil || keyHash == "" || entry.Key.Name == browserKeyName {
		return
	}
	saved := entry
	s.authCacheState.memory.Store(keyHash, &saved)
}

func copyAPIKey(key *APIKey) *APIKey {
	if key == nil {
		return nil
	}
	cp := *key
	return &cp
}
