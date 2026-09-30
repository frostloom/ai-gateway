package middleware

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/gin-gonic/gin"
)

// Browser sessions take precedence over legacy API keys, so an old browser key
// cannot silently switch a newly signed-in user to another tenant.
func PortalSession(base string, billing *client.Billing) gin.HandlerFunc {
	legacy := APIKey(billing)
	hc := &http.Client{Timeout: 3 * time.Second}
	return func(c *gin.Context) {
		token, err := c.Cookie("agw_user")
		if err != nil || token == "" {
			legacy(c)
			return
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, base+"/auth/me", nil)
		if err != nil {
			abort(c, 503, "authentication unavailable")
			return
		}
		req.AddCookie(&http.Cookie{Name: "agw_user", Value: token})
		resp, err := hc.Do(req)
		if err != nil {
			abort(c, 503, "authentication unavailable")
			return
		}
		defer resp.Body.Close()
		var me struct {
			TenantID int64  `json:"tenant_id"`
			APIKeyID int64  `json:"api_key_id"`
			Role     string `json:"role"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&me) != nil || me.Role != "user" || me.TenantID <= 0 || me.APIKeyID <= 0 {
			abort(c, 401, "登录已过期，请重新登录")
			return
		}
		c.Set("tenant_id", me.TenantID)
		c.Set("api_key_id", me.APIKeyID)
		// A time-limited, revocable hashed credential allows existing agent auth to
		// independently verify the tenant. Never forward caller-supplied internal headers.
		c.Request.Header.Set("Authorization", "Bearer "+token)
		c.Next()
	}
}

// Reject cross-origin browser mutations before either login or business handlers.
func BrowserSameOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		browser := strings.HasPrefix(path, "/auth/") || strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/portal/")
		if browser && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			if origin := c.GetHeader("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != c.Request.Host || (u.Scheme != "http" && u.Scheme != "https") {
					abort(c, 403, "cross-origin request rejected")
					return
				}
			}
			if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
				abort(c, 403, "cross-site request rejected")
				return
			}
		}
		c.Next()
	}
}
