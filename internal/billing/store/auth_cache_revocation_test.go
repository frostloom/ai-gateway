package store

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func TestAuthCacheRevocationAcrossReplicas(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	first, second := NewStore(nil, rdb), NewStore(nil, rdb)
	first.EnableAuthCache(time.Minute)
	second.EnableAuthCache(time.Minute)
	active := true
	lookup := func(context.Context, string) (*APIKey, error) {
		if !active {
			return nil, nil
		}
		return &APIKey{ID: 1, TenantID: 1, KeyHash: "revoked", Name: "test"}, nil
	}
	if key, err := second.cachedAPIKey(context.Background(), "revoked", lookup); err != nil || key == nil {
		t.Fatal("initial fill failed")
	}
	active = false
	first.ForgetAuthCache(context.Background(), "revoked")
	if key, err := second.cachedAPIKey(context.Background(), "revoked", lookup); err != nil || key != nil {
		t.Fatalf("revoked key returned by replica: %v %v", key, err)
	}
}

func TestAuthCacheRevocationDuringFill(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	s := NewStore(nil, rdb)
	s.EnableAuthCache(time.Minute)
	started, resume := make(chan struct{}), make(chan struct{})
	result := make(chan *APIKey)
	go func() {
		key, _ := s.cachedAPIKey(context.Background(), "racing", func(context.Context, string) (*APIKey, error) {
			close(started)
			<-resume
			return &APIKey{ID: 1, TenantID: 1, KeyHash: "racing", Name: "test"}, nil
		})
		result <- key
	}()
	<-started
	s.ForgetAuthCache(context.Background(), "racing")
	close(resume)
	if key := <-result; key != nil {
		t.Fatal("stale in-flight lookup reauthorized revoked key")
	}
}
