package loadtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
	"github.com/gin-gonic/gin"
)

func TestRateFirstConcurrentRedisStall(t *testing.T) {
	if os.Getenv("TEST_IDEM_STALL") != "1" {
		t.Skip("isolated only")
	}
	container, addr := os.Getenv("TEST_IDEM_REDIS_CONTAINER"), os.Getenv("TEST_IDEM_REDIS_ADDR")
	if !strings.HasPrefix(container, "codex-idem-stall-") || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatal("requires isolated Redis")
	}
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", container).Run() })
	rdb := redisx.New(addr, "")
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	tenant := time.Now().UnixNano()
	const n = 64
	var blocked, calls atomic.Int32
	release := make(chan struct{})
	pauseResult := make(chan error, 1)
	unpaused := make(chan error, 1)
	r.Use(func(c *gin.Context) { c.Set("tenant_id", tenant); c.Next() }, func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() == 429 {
			if blocked.Add(1) == n {
				err := exec.Command("docker", "pause", container).Run()
				pauseResult <- err
				if err == nil {
					time.AfterFunc(4*time.Second, func() { unpaused <- exec.Command("docker", "unpause", container).Run() })
				}
				close(release)
			}
			<-release
		}
	}, middleware.RateLimit(rdb, time.Minute, 1), middleware.Idempotency(rdb))
	r.POST("/", func(c *gin.Context) { calls.Add(1); c.Status(http.StatusNoContent) })
	warm := httptest.NewRecorder()
	r.ServeHTTP(warm, httptest.NewRequest(http.MethodPost, "/", nil))
	if warm.Code != 204 {
		t.Fatal(warm.Code)
	}
	type result struct {
		status   int
		duration time.Duration
	}
	results := make(chan result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	prefix := fmt.Sprintf("concurrent-stall-%d-", time.Now().UnixNano())
	before := *rdb.PoolStats()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set("Idempotency-Key", fmt.Sprintf("%s%d", prefix, i))
			w := httptest.NewRecorder()
			<-start
			began := time.Now()
			r.ServeHTTP(w, req)
			results <- result{w.Code, time.Since(began)}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	after := *rdb.PoolStats()
	if err := <-pauseResult; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-unpaused:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Redis unpause timed out")
	}
	durations := make([]float64, 0, n)
	var sum float64
	for result := range results {
		if result.status != 429 {
			t.Fatalf("status=%d", result.status)
		}
		ms := float64(result.duration.Microseconds()) / 1000
		durations = append(durations, ms)
		sum += ms
	}
	sort.Float64s(durations)
	if blocked.Load() != n || calls.Load() != 1 {
		t.Fatalf("blocked=%d calls=%d", blocked.Load(), calls.Load())
	}
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		keys = append(keys, fmt.Sprintf("idem:%s%d", prefix, i))
	}
	remaining, err := rdb.Exists(context.Background(), keys...).Result()
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("rate rejected requests created %d keys", remaining)
	}
	if err := rdb.Del(context.Background(), fmt.Sprintf("rate:win:%d", tenant), fmt.Sprintf("rate:seq:%d", tenant)).Err(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("%s%d", prefix, 0))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 204 || calls.Load() != 2 {
		t.Fatalf("retry=%d calls=%d", w.Code, calls.Load())
	}
	t.Logf("requests=%d status429=%d downstream_initial=1 mean_ms=%.2f p95_ms=%.2f p99_ms=%.2f max_ms=%.2f redis_pool_waits=%d redis_pool_wait_ms=%.2f keys_remaining_after_resume=%d retry=%d", n, n, sum/n, durations[60], durations[63], durations[63], after.WaitCount-before.WaitCount, float64(after.WaitDurationNs-before.WaitDurationNs)/1e6, remaining, w.Code)

}
