package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/frostloom/ai-gateway/internal/pkg/xid"
)

// Idempotency 双重职责：
//  1. 生成 request_id 放进 context（下游 billing 的幂等键，网关重试复用同一个）；
//  2. 若客户端带 Idempotency-Key 头，用 Redis SETNX 原子挡住重复提交 → 409。
//
// 幂等键语义：同一 key 短时间内重复提交直接 409，不重复计费不重复出结果。
// 注意这是「提交去重」而非「响应重放」——重放需要缓存响应体，超出本 demo 范围，
// 面试可讲这是取舍（409 vs 200-replay）。
func Idempotency(rdb *goredis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := xid.New()
		if key := c.GetHeader("Idempotency-Key"); key != "" {
			ok, err := rdb.SetNX(c.Request.Context(), "idem:"+key, rid, 5*time.Minute).Result()
			if err != nil {
				abort(c, http.StatusInternalServerError, "idempotency unavailable")
				return
			}
			if !ok {
				abort(c, http.StatusConflict, "duplicate Idempotency-Key")
				return
			}
			rid = "idem:" + key
		}
		c.Set("request_id", rid)
		c.Next()
	}
}
