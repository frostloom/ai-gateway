package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// Bound password attempts by the actual peer IP; never trust client X-Forwarded-For.
func AuthRateLimit(rdb *goredis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || strings.HasSuffix(c.Request.URL.Path, "/logout") {
			c.Next()
			return
		}
		ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			ip = c.Request.RemoteAddr
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		n, err := rdb.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],300) end; return n`, []string{"auth:attempts:" + sha256Hex(ip)}).Int64()
		if err != nil {
			abort(c, 503, "登录服务暂不可用")
			return
		}
		if n > 30 {
			c.Header("Retry-After", "300")
			abort(c, 429, "登录或注册过于频繁，请 5 分钟后重试")
			return
		}
		c.Next()
	}
}
