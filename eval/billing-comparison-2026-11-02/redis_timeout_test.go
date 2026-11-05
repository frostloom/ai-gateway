package comparison

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/redis/go-redis/v9"
)

// Loopback-only socket accepts but never replies. It does not connect to any
// existing Redis service. Timings are observations, not hard real-time SLOs.
func TestRedisContextTimeoutObservation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "enabled"}[enabled], func(t *testing.T) {
			ln, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			var wg sync.WaitGroup
			stop := make(chan struct{})
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					conn, e := ln.Accept()
					if e != nil {
						return
					}
					wg.Add(1)
					go func() { defer wg.Done(); defer conn.Close(); <-stop }()
				}
			}()
			c := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), DialTimeout: time.Second, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, PoolTimeout: time.Second, MaxRetries: -1, ContextTimeoutEnabled: enabled})
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := c.Get(ctx, "test").Err()
			elapsed := time.Since(start)
			if err == nil {
				t.Error("expected timeout from nonresponsive test server")
			}
			t.Logf("context_enabled=%v requested_ms=30 elapsed_ms=%.1f err=%v", enabled, float64(elapsed.Microseconds())/1000, err)
			close(stop)
			_ = ln.Close()
			wg.Wait()
		})
	}
}

func TestGuardedFallbackOnUnresponsiveRedis(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, e := ln.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() { defer wg.Done(); defer conn.Close(); <-stop }()
		}
	}()
	defer func() { close(stop); _ = ln.Close(); wg.Wait() }()
	r := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), DialTimeout: time.Second, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, PoolTimeout: time.Second, MaxRetries: -1, ContextTimeoutEnabled: true})
	defer r.Close()
	calls := 0
	c := guardedAuthCache{r: r, ttl: time.Minute, fenceTTL: time.Second, validate: func(context.Context, string) (*store.APIKey, error) {
		calls++
		return &store.APIKey{ID: 1, TenantID: 2, KeyHash: "offline"}, nil
	}}
	start := time.Now()
	k, err := c.lookup(context.Background(), "offline")
	elapsed := time.Since(start)
	if err != nil || k == nil || calls != 1 {
		t.Fatalf("fallback failed key=%v err=%v calls=%d", k, err, calls)
	}
	t.Logf("unresponsive Redis fallback calls=%d elapsed_ms=%.1f", calls, float64(elapsed.Microseconds())/1000)
}
