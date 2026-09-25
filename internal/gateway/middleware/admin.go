// Package middleware gateway 中间件：API Key 鉴权 · 租户限流 · 幂等键 · 面板 admin 鉴权。
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// AdminSession 校验面板 admin 请求的登录态（HttpOnly cookie agw_admin，newapi 式登录）。
//
// 优先级：cookie（浏览器登录后的 HttpOnly）→ X-Admin-Token 头（脚本/curl 传已拿到的 token，
// 未来可去掉）→ ADMIN_TOKEN 环境变量兜底（仅本机演示；前端不使用）。
// 有 token 时转发到 billing /admin/auth/me 校验，401 则引导前端跳登录页。
func AdminSession(billingAdminBase, svcToken string, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := requestToken(c)
		if tok == "" {
			abortAdmin(c, http.StatusUnauthorized, "unauthorized")
			return
		}
		// 与 env 固定值相等 → 直接放行（本机 curl 演示；正式环境 svcToken 置空即关闭）。
		if svcToken != "" && tok == svcToken {
			c.Next()
			return
		}
		if !verifySession(billingAdminBase+"/admin/auth/me", tok) {
			abortAdmin(c, http.StatusUnauthorized, "session invalid")
			return
		}
		c.Next()
	}
}

// requestToken 从 cookie 或 X-Admin-Token 头取 token。
func requestToken(c *gin.Context) string {
	if ck, err := c.Cookie("agw_admin"); err == nil && ck != "" {
		return ck
	}
	return c.GetHeader("X-Admin-Token")
}

// verifySession 调 billing /admin/auth/me 校验 token，成功返回 true。
// 命中 401/无效即失败。用独立 client 避免与面板聚合 proxy 的 client 互相干扰。
func verifySession(target, token string) bool {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Admin-Token", token)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// abortAdmin 与 apikey.go 的 abort 格式不同：面板前端读 d.error（字符串），
// 这里保持 {error:"..."}；apikey 中间件用 {error:{message,type}}（OpenAI 风格）。
func abortAdmin(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}
