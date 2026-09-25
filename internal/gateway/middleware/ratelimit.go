package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// 滑动窗口限流：ZSET 记录窗口内每个请求的时间戳，Lua 原子「清理过期 + 计数 + 放行」。
// key: rate:win:{tenant_id}；超过 limit 返回 429。
const rateLimitLua = `
local window = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], 0, ARGV[1] - window)
local count = redis.call('ZCARD', KEYS[1])
if count >= tonumber(ARGV[3]) then
  return 0
end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[1])
redis.call('PEXPIRE', KEYS[1], window)
return 1
`

// RateLimit 按租户限流。limit 为窗口内最大请求数。
func RateLimit(rdb *goredis.Client, window time.Duration, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetInt64("tenant_id")
		key := fmt.Sprintf("rate:win:%d", tenantID)
		now := time.Now().UnixMilli()

		allowed, err := rdb.Eval(c.Request.Context(), rateLimitLua, []string{key}, now, window.Milliseconds(), limit).Bool()
		if err != nil {
			abort(c, http.StatusInternalServerError, "rate limit unavailable")
			return
		}
		if !allowed {
			abort(c, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		c.Next()
	}
}
