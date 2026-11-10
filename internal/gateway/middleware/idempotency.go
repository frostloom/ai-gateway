package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/pkg/xid"
)

// Idempotency 给这次调用生成 request_id。明细按这个值把预扣和结算记在一起。
// 客户端的 Idempotency-Key 暂时不参与去重。
func Idempotency() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("request_id", xid.New())
		c.Next()
	}
}
