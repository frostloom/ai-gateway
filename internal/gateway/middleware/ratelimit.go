package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
)

// 令牌桶：Redis 服务端时间和 Lua 原子更新使多个 gateway 共享同一租户的限额。
const rateLimitLua = `
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[1])
local limit = tonumber(ARGV[3])
local capacity = tonumber(ARGV[4])
if now == 0 then
  local t = redis.call('TIME')
  now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
end
local state = redis.call('HMGET', KEYS[1], 'tokens', 'time')
local tokens = tonumber(state[1]) or capacity
local last = tonumber(state[2]) or now
now = math.max(now, last)
tokens = math.min(capacity, tokens + (now - last) * limit / window)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'time', now)
redis.call('PEXPIRE', KEYS[1], math.max(1, math.ceil(capacity * window / limit)))
return allowed
`

// RateLimit 按租户限制平均速率，burst 为桶容量。
func RateLimit(rdb *goredis.Client, window time.Duration, limit int, burst ...int) gin.HandlerFunc {
	capacity := min(limit, 100)
	if len(burst) > 0 {
		capacity = burst[0]
	}
	if limit <= 0 || capacity <= 0 || window.Milliseconds() <= 0 {
		panic("invalid rate limit configuration")
	}
	return func(c *gin.Context) {
		tenantID := c.GetInt64("tenant_id")
		key := fmt.Sprintf("rate:bucket:v1:%d", tenantID)

		limitStart := time.Now()
		allowed, err := rdb.Eval(c.Request.Context(), rateLimitLua, []string{key}, 0, window.Milliseconds(), limit, capacity).Bool()
		metrics.RequestHop("ratelimit", time.Since(limitStart))
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
