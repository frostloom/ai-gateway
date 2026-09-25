// Package middleware gateway 中间件：API Key 鉴权 · 租户限流 · 幂等键。
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
)

// APIKey 校验请求的 API Key。支持 Authorization: Bearer sk-xxx 与 api-key 头。
// 明文 key 只在内存算 SHA-256 后传给 billing 校验，永远不落日志/存储。
func APIKey(b *client.Billing) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := extractAPIKey(c)
		if key == "" {
			abort(c, http.StatusUnauthorized, "missing api key")
			return
		}
		hash := sha256Hex(key)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		resp, err := b.ValidateAPIKey(ctx, hash)
		if err != nil || resp == nil || !resp.Valid {
			abort(c, http.StatusUnauthorized, "invalid api key")
			return
		}
		c.Set("tenant_id", resp.TenantId)
		c.Set("api_key_id", resp.ApiKeyId)
		c.Next()
	}
}

func extractAPIKey(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if k := c.GetHeader("api-key"); k != "" {
		return k
	}
	return c.GetHeader("x-api-key")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func abort(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": gin.H{"message": msg, "type": "authentication_error"}})
}
