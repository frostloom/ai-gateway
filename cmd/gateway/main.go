// gateway 网关入口：OpenAI 兼容 HTTP 入口。
// 中间件顺序（有讲究）：
//
//	APIKey（先拒绝坏 key）→ Idempotency（生成 request_id/挡重复提交）→ RateLimit（按租户）
package main

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/gateway/web"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
)

func main() {
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	port := config.Getenv("GATEWAY_PORT", "8080")
	billingAddr := config.Getenv("BILLING_ADDR", "127.0.0.1:9101")
	billingAdminAddr := config.Getenv("BILLING_ADMIN_ADDR", "http://127.0.0.1:9103")
	routerAddr := config.Getenv("ROUTER_ADDR", "http://127.0.0.1:9102")
	agentAddr := config.Getenv("AGENT_ADDR", "http://127.0.0.1:9105")
	reconcilerAddr := config.Getenv("RECONCILER_ADDR", "http://127.0.0.1:9106")
	redisAddr := config.Getenv("REDIS_ADDR", "127.0.0.1:6381")
	redisPwd := config.Getenv("REDIS_PASSWORD", "")
	ratePerMin := config.GetenvInt("RATE_LIMIT_PER_MIN", 600)
	adminToken := config.Getenv("ADMIN_TOKEN", "admin-demo")

	rdb := redisx.New(redisAddr, redisPwd)
	if err := redisx.Ping(context.Background(), rdb); err != nil {
		log.Error("connect redis", "err", err)
		os.Exit(1)
	}
	bc, err := client.NewBilling(billingAddr)
	if err != nil {
		log.Error("connect billing", "err", err)
		os.Exit(1)
	}
	defer bc.Close()
	rc := client.NewRouter(routerAddr)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(metrics.GinMiddleware("gateway")) // M5 可观测
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	v1 := r.Group("/v1")
	v1.Use(middleware.APIKey(bc))
	v1.Use(middleware.Idempotency(rdb))
	v1.Use(middleware.RateLimit(rdb, time.Minute, ratePerMin))
	v1.POST("/chat/completions", handler.Chat(bc, rc, log, 2*time.Minute))

	// 面板：独立前端工程产物（web/ → dist），无 CDN 离线可用。
	//   /            → dashboard.html（管理面板）
	//   /portal      → portal.html（用户商城，Bearer 租户 key）
	//   /assets/*    → Vite 哈希产物（JS/CSS/字体）
	webFS := web.Dist()
	serveHTML := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			b, err := fs.ReadFile(webFS, name)
			if err != nil {
				c.String(http.StatusNotFound, "page not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", b)
		}
	}
	r.GET("/", serveHTML("dashboard.html"))
	r.GET("/portal", serveHTML("portal.html"))
	r.Any("/assets/*filepath", gin.WrapH(http.FileServer(http.FS(webFS))))
	portal := r.Group("/portal")
	portal.Use(middleware.APIKey(bc)) // tenant 由 key 派生
	handler.RegisterPortal(portal, billingAdminAddr, agentAddr, log)

	// /admin/auth/*：登录/初始化/登出/会话校验，不套登录中间件（登录前即可访问）。
	// billing 收尾签发 HttpOnly cookie，经 proxy 透传给浏览器。
	auth := r.Group("/admin/auth")
	handler.RegisterAdminAuth(auth, billingAdminAddr, log)

	// /admin/*：面板数据接口，需登录态（cookie/token/admin-demo 兜底）。
	admin := r.Group("/admin")
	admin.Use(middleware.AdminSession(billingAdminAddr, adminToken, log))
	handler.RegisterAdmin(admin, billingAdminAddr, routerAddr, reconcilerAddr, agentAddr, log)
	// 管理面板 AI 客服：管理员没有租户 key，由 gateway 换发内部凭证（含目标租户）给 agent。
	handler.RegisterAdminChat(admin, agentAddr,
		config.Getenv("AGENT_INTERNAL_TOKEN", ""),
		uint64(config.GetenvInt64("ADMIN_DEFAULT_TENANT", 1)), log)

	log.Info("gateway listening", "port", port, "billing", billingAddr, "router", routerAddr)
	srv := &http.Server{Addr: ":" + port, Handler: r}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
