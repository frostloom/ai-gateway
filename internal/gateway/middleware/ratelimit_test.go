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

func TestRateLimitCountsSameMillisecond(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	ctx := t.Context()
	const now int64 = 1_700_000_000_000
	window := time.Minute.Milliseconds()
	for i := 0; i < 100; i++ {
		allowed, err := rdb.Eval(ctx, rateLimitLua, []string{"rate:win:7", "rate:seq:7"}, now, window, 100).Bool()
		if err != nil || !allowed {
			t.Fatalf("same-ms request %d allowed=%v err=%v", i, allowed, err)
		}
	}
	members, err := rdb.ZRange(ctx, "rate:win:7", 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 100 {
		t.Fatalf("zset members = %d, want 100 distinct hits at one timestamp", len(members))
	}
	allowed, err := rdb.Eval(ctx, rateLimitLua, []string{"rate:win:7", "rate:seq:7"}, now, window, 100).Bool()
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("101st request in the same millisecond was allowed")
	}

	mr.FlushAll()
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
