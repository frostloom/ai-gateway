package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// 滑动窗口限流：ZSET 记录窗口内每个请求，Lua 原子「清理过期 + 计数 + 放行」。
// member 是「毫秒:序号」。同一毫秒的多次请求不能共用一个 member，否则 ZADD 会覆盖，突发被少计。
// key: rate:win:{tenant_id}；序号 key: rate:seq:{tenant_id}。超过 limit 返回 429。
const rateLimitLua = `
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[1])
local limit = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], 0, now - window)
local count = redis.call('ZCARD', KEYS[1])
if count >= limit then
  return 0
end
local seq = redis.call('INCR', KEYS[2])
redis.call('PEXPIRE', KEYS[2], window)
redis.call('ZADD', KEYS[1], now, now .. ':' .. seq)
redis.call('PEXPIRE', KEYS[1], window)
return 1
`

// RateLimit 按租户限流。limit 为窗口内最大请求数。
func RateLimit(rdb *goredis.Client, window time.Duration, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetInt64("tenant_id")
		key := fmt.Sprintf("rate:win:%d", tenantID)
		seq := fmt.Sprintf("rate:seq:%d", tenantID)
		now := time.Now().UnixMilli()

		allowed, err := rdb.Eval(c.Request.Context(), rateLimitLua, []string{key, seq}, now, window.Milliseconds(), limit).Bool()
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
