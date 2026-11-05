package loadtest

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// A delayed Redis operation must not resurrect a canceled attempt.
func TestIdemCanceledAttemptCannotArriveLate(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	c := idemCandidate{r: r, markerTTL: time.Second, keyTTL: time.Minute}
	ctx := context.Background()
	if err := c.cancel(ctx, "request", "owner-A"); err != nil {
		t.Fatal(err)
	}
	if v, e := c.acquire(ctx, "request", "owner-A"); e != nil || v != idemCanceled {
		t.Fatalf("late canceled acquire = %v %v", v, e)
	}
	if v, e := c.acquire(ctx, "request", "owner-B"); e != nil || v != idemAcquired {
		t.Fatalf("retry should acquire = %v %v", v, e)
	}
	if err := c.cancel(ctx, "request", "owner-A"); err != nil {
		t.Fatal(err)
	}
	if v, e := c.acquire(ctx, "request", "owner-C"); e != nil || v != idemDuplicate {
		t.Fatalf("old cancel deleted new owner = %v %v", v, e)
	}
}

func TestIdemExpiredCancellationCanArriveLate(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	c := idemCandidate{r: r, markerTTL: time.Second, keyTTL: time.Minute}
	ctx := context.Background()
	if e := c.cancel(ctx, "request", "old"); e != nil {
		t.Fatal(e)
	}
	m.FastForward(2 * time.Second)
	if v, e := c.acquire(ctx, "request", "old"); e != nil || v != idemAcquired {
		t.Fatalf("expected to observe late command after marker expiry = %v %v", v, e)
	}
}

func TestIdemCancelAfterForwardAllowsDuplicate(t *testing.T) {
	m := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer r.Close()
	c := idemCandidate{r: r, markerTTL: time.Minute, keyTTL: time.Minute}
	ctx := context.Background()
	if v, e := c.acquire(ctx, "request", "owner-A"); e != nil || v != idemAcquired {
		t.Fatal(v, e)
	}
	// The first attempt has potentially reached an upstream. Redis alone has no
	// durable proof that it is safe to cancel the logical request.
	if e := c.cancel(ctx, "request", "owner-A"); e != nil {
		t.Fatal(e)
	}
	if v, e := c.acquire(ctx, "request", "owner-B"); e != nil || v != idemAcquired {
		t.Fatalf("expected duplicate execution risk: %v %v", v, e)
	}
}
