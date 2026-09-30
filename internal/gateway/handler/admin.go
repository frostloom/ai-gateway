// Package handler gateway 的 OpenAI 兼容 HTTP 处理。
// admin.go：面板聚合路由（/admin/*）——把 billing/router 的观测端点聚成一个入口，
// 鉴权由 middleware.AdminToken 把关，浏览器只有一个 origin，无 CORS 问题。
package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// adminHTTP 面板聚合转发共用 client（复用连接池，别每请求新建）。
var adminHTTP = &http.Client{Timeout: 5 * time.Second}

// RegisterAdmin 注册面板聚合路由。
//
//	billingAdminBase = http://127.0.0.1:9103（billing 观测端点）
//	routerBase      = http://127.0.0.1:9102（router 观测端点）
//	reconcilerBase  = http://127.0.0.1:9106（对账观测端点）
//	agentBase       = http://127.0.0.1:9105（AI 客服观测端点）
//
// RegisterAdminAuth 注册面板管理员认证（登录/初始化/登出/会话校验）转发。
// 这些路由不套 AdminSession 中间件（登录前即可访问），由 billing 收尾签发 cookie。
func RegisterAdminAuth(g *gin.RouterGroup, billingAdminBase string, log *slog.Logger) {
	g.GET("/initialized", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/auth/initialized", log)
	})
	g.POST("/setup", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		proxy(c, http.MethodPost, billingAdminBase+"/admin/auth/setup", body, log)
	})
	g.POST("/login", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		proxy(c, http.MethodPost, billingAdminBase+"/admin/auth/login", body, log)
	})
	g.POST("/logout", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		proxy(c, http.MethodPost, billingAdminBase+"/admin/auth/logout", body, log)
	})
	g.GET("/me", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/auth/me", log)
	})
}

func RegisterAdmin(g *gin.RouterGroup, billingAdminBase, routerBase, reconcilerBase, agentBase string, log *slog.Logger) {
	// 总览：租户余额/消耗 + 全局汇总（转 billing）
	g.GET("/overview", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/overview", log)
	})
	// 账单列表：query 透传（tenant_id/status/limit/offset）
	g.GET("/bills", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/bills?"+c.Request.URL.RawQuery, log)
	})
	// M7 只读观测：订阅 / 入账流水（跨租户管理视角，query 透传 tenant_id）
	g.GET("/subscription", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/subscription?"+c.Request.URL.RawQuery, log)
	})
	g.GET("/topups", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/topups?"+c.Request.URL.RawQuery, log)
	})
	g.GET("/recharge-orders", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/recharge-orders?"+c.Request.URL.RawQuery, log)
	})
	// provider 全量 + 熔断状态（转 router；router 已合并 /breakers）
	g.GET("/providers", func(c *gin.Context) {
		proxyJSON(c, routerBase+"/providers", log)
	})

	// ---------- M10 渠道管理：上游接入 + 成本/毛利（转 billing /admin/providers） ----------
	// 与上面的 /providers（router 熔断视角）路径分离：GET 取渠道列表（含毛利），POST upsert 渠道。
	g.GET("/channels", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/providers", log)
	})
	g.POST("/channels", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		proxy(c, http.MethodPost, billingAdminBase+"/admin/providers", body, log)
	})

	// ---------- 商城：商品目录（卖方管理） ----------
	// GET 列表（query 透传 vendor/status）；POST 新增 SKU（body 透传）。
	g.Any("/models", func(c *gin.Context) {
		var body []byte
		if c.Request.Method == http.MethodPost {
			body, _ = io.ReadAll(c.Request.Body)
		}
		target := billingAdminBase + "/admin/models"
		if c.Request.Method == http.MethodGet {
			target += "?" + c.Request.URL.RawQuery
		}
		proxy(c, c.Request.Method, target, body, log)
	})
	g.POST("/models/status", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		proxy(c, http.MethodPost, billingAdminBase+"/admin/models/status", body, log)
	})
	g.POST("/models/price", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		proxy(c, http.MethodPost, billingAdminBase+"/admin/models/price", body, log)
	})

	// ---------- 销售统计 / 对账 / 审计 / 租户观测 ----------
	g.GET("/sales", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/sales?"+c.Request.URL.RawQuery, log)
	})
	g.GET("/audit", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/audit?"+c.Request.URL.RawQuery, log)
	})
	g.GET("/reconcile-check", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/reconcile-check", log)
	})
	g.GET("/me", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/me?"+c.Request.URL.RawQuery, log)
	})
	g.GET("/consumption", func(c *gin.Context) {
		proxyJSON(c, billingAdminBase+"/admin/consumption?"+c.Request.URL.RawQuery, log)
	})

	// ---------- 服务健康聚合：billing / router / reconciler / agent ----------
	g.GET("/health", func(c *gin.Context) {
		endpoints := map[string]string{
			"billing":    billingAdminBase + "/healthz",
			"router":     routerBase + "/healthz",
			"reconciler": reconcilerBase + "/healthz",
			"agent":      agentBase + "/healthz",
		}
		results := make(map[string]any)
		allOK := true
		for name, target := range endpoints {
			resp, err := adminHTTP.Get(target)
			if err != nil || resp.StatusCode != http.StatusOK {
				results[name] = map[string]any{"ok": false, "error": "unreachable"}
				allOK = false
				continue
			}
			_ = resp.Body.Close()
			results[name] = map[string]any{"ok": true}
		}
		c.JSON(http.StatusOK, map[string]any{"ok": allOK, "services": results})
	})
}

// RegisterAdminChat 管理面板 AI 客服代理：POST /admin/chat。
//
// 管理员没有租户 key，所以由 gateway（已过 AdminSession 中间件）把「已校验的管理员身份」
// 换成 agent 信任的内部凭证：X-Agent-Internal（共享密钥）+ X-Agent-Tenant（目标租户，
// 默认 ADMIN_DEFAULT_TENANT，可在 body 里带 tenant_id 切换视角）。
// agent 内部超时长：DeepSeek 多轮 tool loop 可能远超 5s。
func RegisterAdminChat(g *gin.RouterGroup, agentBase, internalToken string, defaultTenant uint64, log *slog.Logger) {
	g.POST("/chat", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}
		// 目标租户：body.tenant_id > 查询参数 > 默认
		tenant := defaultTenant
		var probe struct {
			TenantID uint64 `json:"tenant_id"`
		}
		_ = json.Unmarshal(body, &probe)
		if probe.TenantID > 0 {
			tenant = probe.TenantID
		} else if q := c.Query("tenant_id"); q != "" {
			if n, err := strconv.ParseUint(q, 10, 64); err == nil {
				tenant = n
			}
		}

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, agentBase+"/chat", bytes.NewReader(body))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "build chat request failed"})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-Internal", internalToken)
		req.Header.Set("X-Agent-Tenant", strconv.FormatUint(tenant, 10))

		resp, err := chatHTTP.Do(req)
		if err != nil {
			log.Error("admin chat", "err", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "agent 不可达"})
			return
		}
		defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", b)
	})
}

func proxyJSON(c *gin.Context, target string, log *slog.Logger) {
	proxy(c, http.MethodGet, target, nil, log)
}

func RegisterUserAuth(g *gin.RouterGroup, base string, log *slog.Logger) {
	g.GET("/me", func(c *gin.Context) { proxyJSON(c, base+"/auth/me", log) })
	for _, action := range []string{"register", "login", "logout"} {
		g.POST("/"+action, func(c *gin.Context) {
			body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
			if err != nil {
				c.JSON(400, gin.H{"error": "请求过大"})
				return
			}
			proxy(c, http.MethodPost, base+"/auth/"+action, body, log)
		})
	}
}

// proxy 转发到上游（billing/router 观测端点）。body 非空时为 POST + application/json。
func proxy(c *gin.Context, method, target string, body []byte, log *slog.Logger) {
	if _, err := url.Parse(target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bad upstream url"})
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, target, bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "build upstream request failed"})
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// 透传鉴权凭证到上游：/admin/auth/me、/logout 需要 billing 读到 HttpOnly cookie；
	// 脚本/curl 用 X-Admin-Token 头。两者都转发，避免上游误判未登录。
	if ck, err := c.Cookie("agw_admin"); err == nil && ck != "" {
		req.AddCookie(&http.Cookie{Name: "agw_admin", Value: ck})
	}
	if ck, err := c.Cookie("agw_user"); err == nil && ck != "" {
		req.AddCookie(&http.Cookie{Name: "agw_user", Value: ck})
	}
	if c.Request.TLS != nil || os.Getenv("AUTH_COOKIE_SECURE") == "1" {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	if strings.Contains(c.Request.URL.Path, "/auth/") {
		c.Header("Cache-Control", "no-store")
	}
	if h := c.GetHeader("X-Admin-Token"); h != "" {
		req.Header.Set("X-Admin-Token", h)
	}
	resp, err := adminHTTP.Do(req)
	if err != nil {
		log.Error("admin proxy", "target", target, "err", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream unreachable"})
		return
	}
	defer resp.Body.Close()
	// 透传 Set-Cookie（billing 登录/退出签发的 HttpOnly 会话 cookie 要交给浏览器）。
	for _, k := range []string{"Set-Cookie"} {
		for _, v := range resp.Header.Values(k) {
			c.Writer.Header().Add(k, v)
		}
	}
	b, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", b)
}
