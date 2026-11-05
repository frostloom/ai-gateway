package loadtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestRateRejectedKeyCanRetry(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", int64(1)); c.Next() }, middleware.Idempotency(rdb), middleware.RateLimit(rdb, time.Second, 1))
	calls := 0
	r.POST("/", func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) })
	request := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := request("first"); got != 204 {
		t.Fatal(got)
	}
	if got := request("retryable"); got != 429 {
		t.Fatal(got)
	}
	mr.FastForward(2 * time.Second)
	if got := request("retryable"); got != 204 {
		t.Fatalf("retry after limiter expiry=%d want 204 (calls=%d)", got, calls)
	}
	if calls != 2 {
		t.Fatalf("handler calls=%d", calls)
	}
}

func TestHandler429DoesNotReleaseKey(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", int64(1)); c.Next() }, middleware.Idempotency(rdb), middleware.RateLimit(rdb, time.Second, 10))
	calls := 0
	r.POST("/", func(c *gin.Context) { calls++; c.Status(http.StatusTooManyRequests) })
	request := func() int {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Idempotency-Key", "upstream-429")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := request(); got != 429 {
		t.Fatal(got)
	}
	if got := request(); got != 409 {
		t.Fatalf("replay=%d want 409", got)
	}
	if calls != 1 {
		t.Fatalf("handler calls=%d", calls)
	}
}

func TestRateRejectionDoesNotDeleteReplacementOwner(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", int64(1)); c.Next() }, middleware.Idempotency(rdb), func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() == 429 {
			if err := rdb.Set(context.Background(), "idem:replacement", "new-owner", time.Minute).Err(); err != nil {
				t.Error(err)
			}
		}
	}, middleware.RateLimit(rdb, time.Minute, 1))
	r.POST("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := request("first-owner"); got != 204 {
		t.Fatal(got)
	}
	if got := request("replacement"); got != 429 {
		t.Fatal(got)
	}
	value, err := rdb.Get(context.Background(), "idem:replacement").Result()
	if err != nil || value != "new-owner" {
		t.Fatalf("owner=%q err=%v", value, err)
	}
}
