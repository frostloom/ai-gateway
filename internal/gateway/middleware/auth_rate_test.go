package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestAuthAttemptsCannotBypassLimitWithForwardedIP(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6381", DB: 2})
	defer rdb.Close()
	key := "auth:attempts:" + sha256Hex("192.0.2.41")
	if err := rdb.Del(context.Background(), key).Err(); err != nil {
		t.Fatal(err)
	}
	defer rdb.Del(context.Background(), key)
	g := gin.New()
	g.POST("/auth/login", AuthRateLimit(rdb), func(c *gin.Context) { c.Status(204) })
	for i := 0; i < 31; i++ {
		r := httptest.NewRequest("POST", "/auth/login", nil)
		r.RemoteAddr = "192.0.2.41:3456"
		r.Header.Set("X-Forwarded-For", "203.0.113.1")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		want := 204
		if i == 30 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d got %d want %d", i+1, w.Code, want)
		}
	}
}
