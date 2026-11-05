package loadtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestIdemRealRedisConcurrent(t *testing.T) {
	addr, name := os.Getenv("IDEM_TEST_REDIS_ADDR"), os.Getenv("IDEM_TEST_REDIS_CONTAINER")
	if !strings.HasPrefix(addr, "127.0.0.1:") || !strings.HasPrefix(name, "idem-test-redis-") {
		t.Skip("requires explicit fresh isolated Redis")
	}
	r := redis.NewClient(&redis.Options{Addr: addr})
	defer r.Close()
	ctx := context.Background()
	if err := r.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	c := idemCandidate{r: r, markerTTL: time.Minute, keyTTL: time.Minute}
	for i := 0; i < 100; i++ {
		request := fmt.Sprintf("real-%d-%d", time.Now().UnixNano(), i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var result int64
		var acqErr, cancelErr error
		go func() { defer wg.Done(); <-start; result, acqErr = c.acquire(ctx, request, "A") }()
		go func() { defer wg.Done(); <-start; cancelErr = c.cancel(ctx, request, "A") }()
		close(start)
		wg.Wait()
		if acqErr != nil || cancelErr != nil || (result != idemAcquired && result != idemCanceled) {
			t.Fatalf("first: result=%d acquire=%v cancel=%v", result, acqErr, cancelErr)
		}
		if v, e := c.acquire(ctx, request, "B"); e != nil || v != idemAcquired {
			t.Fatalf("retry %d: %v %v", i, v, e)
		}
		k, m := c.keys(request, "A")
		if e := r.Del(ctx, k, m).Err(); e != nil {
			t.Fatal(e)
		}
	}
}
