package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestAuthCacheMissFillsRedisOnce(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	s := NewStore(nil, rdb)
	s.EnableAuthCache(time.Minute)
	var calls atomic.Int32
	lookup := func(context.Context, string) (*APIKey, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &APIKey{ID: 7, TenantID: 3, KeyHash: "hash-1", Name: "isolated", Status: 0}, nil
	}

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	var bad atomic.Int32
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			key, err := s.cachedAPIKey(context.Background(), "hash-1", lookup)
			if err != nil || key == nil || key.ID != 7 {
				bad.Add(1)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 || bad.Load() != 0 {
		t.Fatalf("calls=%d bad=%d", calls.Load(), bad.Load())
	}
	s.ForgetAuthCache(context.Background(), "hash-1")
	if _, err := s.cachedAPIKey(context.Background(), "hash-1", lookup); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d, want a fresh lookup after the cache drop", calls.Load())
	}
	if !mr.Exists(authCachePrefix + "hash-1") {
		t.Fatal("redis was not filled again")
	}
	s.authCacheState.memory.Delete("hash-1")
	key, err := s.cachedAPIKey(context.Background(), "hash-1", lookup)
	if err != nil || key == nil || key.TenantID != 3 {
		t.Fatalf("redis refill key=%v err=%v", key, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("redis hit looked up mysql again: %d", calls.Load())
	}
}
