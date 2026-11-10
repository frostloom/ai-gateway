package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

func TestTokenBucketBurstAndRefill(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	ctx := t.Context()
	const now int64 = 1_700_000_000_000
	check := func(at int64, want bool) {
		t.Helper()
		got, err := rdb.Eval(ctx, rateLimitLua, []string{"rate:bucket:7", "rate:seq:7"}, at, 60000, 600, 2).Bool()
		if err != nil || got != want {
			t.Fatalf("at %d: allowed=%v, want %v, err=%v", at, got, want, err)
		}
	}
	check(now, true)
	check(now, true)
	check(now, false)
	check(now+99, false)
	check(now+100, true)
	check(now+100, false)
	check(now+1000, true)
	check(now+1000, true)
	check(now+1000, false)
}

func TestRateLimitCountsSameMillisecond(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", int64(7)) })
	r.Use(RateLimit(rdb, time.Minute, 100))
	r.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("middleware request %d status = %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit status = %d, want 429", w.Code)
	}
}
