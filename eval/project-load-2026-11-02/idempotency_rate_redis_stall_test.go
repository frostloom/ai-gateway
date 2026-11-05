package loadtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
	"github.com/gin-gonic/gin"
)

func TestRateRejectionCleanupRedisStall(t *testing.T) {
	if os.Getenv("TEST_IDEM_STALL") != "1" {
		t.Skip("isolated only")
	}
	container := os.Getenv("TEST_IDEM_REDIS_CONTAINER")
	addr := os.Getenv("TEST_IDEM_REDIS_ADDR")
	if !strings.HasPrefix(container, "codex-idem-stall-") || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatal("test requires explicitly named isolated Redis")
	}
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", container).Run() })
	rdb := redisx.New(addr, "")
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.ReleaseMode)
	tenantID := time.Now().UnixNano()
	r := gin.New()
	unpaused := make(chan error, 1)
	var pauseErr error
	r.Use(func(c *gin.Context) { c.Set("tenant_id", tenantID); c.Next() }, middleware.Idempotency(rdb), func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() == 429 {
			pauseErr = exec.Command("docker", "pause", container).Run()
			if pauseErr == nil {
				time.AfterFunc(4*time.Second, func() { unpaused <- exec.Command("docker", "unpause", container).Run() })
			}
		}
	}, middleware.RateLimit(rdb, time.Minute, 1))
	calls := 0
	r.POST("/", func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) })
	send := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := send(""); got != 204 {
		t.Fatal(got)
	}
	key := fmt.Sprintf("stall-%d", time.Now().UnixNano())
	start := time.Now()
	status := send(key)
	elapsed := time.Since(start)
	if pauseErr != nil {
		t.Fatalf("pause isolated Redis: %v", pauseErr)
	}
	select {
	case err := <-unpaused:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("unpause timed out")
	}
	if status != 429 || calls != 1 {
		t.Fatalf("status=%d handler_calls=%d", status, calls)
	}
	exists, err := rdb.Exists(context.Background(), "idem:"+key).Result()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status=%d handler_calls=%d middleware_elapsed_ms=%d context_budget_ms=1000 key_exists_after_unpause=%d", status, calls, elapsed.Milliseconds(), exists)
	if elapsed > 2*time.Second {
		t.Fatalf("cleanup exceeded 2 second test threshold: %v", elapsed)
	}
}
