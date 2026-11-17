// gateway 网关入口：OpenAI 兼容 HTTP 入口。
// 中间件顺序（有讲究）：
//
//	APIKey（先拒绝坏 key）→ Idempotency（生成 request_id）→ RateLimit（按租户）
//	→ AutoRoute（model:"auto" 三态路由：off 拒绝 / shadow 只记录 / on 改写）
package main

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/autoroute"
	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/handler"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/gateway/web"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/observability"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
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
	rateBurst := config.GetenvInt("RATE_LIMIT_BURST", min(ratePerMin, 100))
	// An absent or invalid setting keeps ambiguous upstream outcomes pending.
	// Explicit zero retains the legacy retry/refund behavior for controlled use.
	holdUnknown := config.GetenvInt("GATEWAY_HOLD_UNKNOWN", 1) != 0
	adminToken := config.Getenv("ADMIN_TOKEN", "")
	controlToken := controlplane.Require(log)
	handler.SetControlToken(controlToken)
	log.Info("gateway billing safety mode", "hold_unknown", holdUnknown)

	rdb := redisx.New(redisAddr, redisPwd)
	if err := redisx.Ping(context.Background(), rdb); err != nil {
		log.Error("connect redis", "err", err)
		os.Exit(1)
	}
	bc, err := client.NewBilling(billingAddr, controlToken)
	if err != nil {
		log.Error("connect billing", "err", err)
		os.Exit(1)
	}
	defer bc.Close()
	rc := client.NewRouter(routerAddr, controlToken)

	// model:"auto" 路由：配置默认 off（off 时中间件照挂、selector 为 nil、不构造 JEV client，
	// auto 请求确定性 400）。shadow/on 态 ConfigFromEnv 已 fail-fast 全量校验含 JEV 配置。
	autoCfg, err := autoroute.ConfigFromEnv(os.Getenv)
	if err != nil {
		log.Error("parse auto route config", "err", err)
		os.Exit(1)
	}
	var autoSelector *autoroute.Selector
	if autoCfg.Mode != autoroute.ModeOff {
		jevClient, err := jev.NewEvaluationClient(autoCfg.JEVEndpoint, autoCfg.JEVAPIKey, autoCfg.JEVModel, jev.ProxyTransport())
		if err != nil {
			log.Error("connect jev", "err", err)
			os.Exit(1)
		}
		autoSelector = autoroute.NewSelector(autoCfg, jevClient)
	}
	// auto 决策导出 Langfuse：观测环境变量启用且 auto 非 off 时构造 observer，
	// 每次 auto 决策写 auto.route span（对话状态经既有裁剪/脱敏）；未配置则为 nil。
	var autoRecorder middleware.AutoRouteRecorder
	if lfCfg, lfErr := observability.ConfigFromEnv(os.Getenv); lfErr != nil {
		log.Error("parse langfuse config", "err", lfErr)
		os.Exit(1)
	} else if lfCfg.Enabled && autoCfg.Mode != autoroute.ModeOff {
		observer, obsErr := observability.New(context.Background(), lfCfg)
		if obsErr != nil {
			log.Error("init langfuse observer", "err", obsErr)
			os.Exit(1)
		}
		autoRecorder = observer
	}
	// route 探测与 handler 共用同一 router 客户端实例，只取 error；
	// NewAutoRouteProbe 把 router 的 (nil, nil)=无可用 provider 也映射为失败，触发降级。
	autoRouteMW := middleware.AutoRoute(autoCfg, autoSelector, middleware.NewAutoRouteProbe(rc), log, autoRecorder)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.BrowserSameOrigin())
	r.Use(metrics.GinMiddleware("gateway")) // M5 可观测
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	v1 := r.Group("/v1")
	v1.Use(middleware.APIKey(bc))
	v1.Use(middleware.Idempotency())
	v1.Use(middleware.RateLimit(rdb, time.Minute, ratePerMin, rateBurst))
	v1.Use(autoRouteMW)
	v1.POST("/chat/completions", handler.Chat(bc, rc, log, 2*time.Minute, holdUnknown))
	v1.POST("/messages", handler.Native(bc, rc, log, "messages", holdUnknown))
	v1.POST("/responses", handler.Native(bc, rc, log, "responses", holdUnknown))
	v1.GET("/models", handler.OpenAIModels(billingAdminAddr, controlToken))
	// Codex 的 base_url 不带 /v1，直接请求 /responses。
	// Claude 的 ANTHROPIC_BASE_URL 带 /anthropic，再请求 /v1/messages。
	rootAPI := r.Group("/")
	rootAPI.Use(middleware.APIKey(bc))
	rootAPI.Use(middleware.Idempotency())
	rootAPI.Use(middleware.RateLimit(rdb, time.Minute, ratePerMin, rateBurst))
	rootAPI.Use(autoRouteMW)
	rootAPI.POST("/responses", handler.Native(bc, rc, log, "responses", holdUnknown))
	rootAPI.POST("/anthropic/v1/messages", handler.Native(bc, rc, log, "messages", holdUnknown))
	// DeepSeek 官网的 OpenAI 地址不带 /v1，客户端直接请求 /chat/completions。
	rootAPI.POST("/chat/completions", handler.Chat(bc, rc, log, 2*time.Minute, holdUnknown))
	rootAPI.GET("/models", handler.OpenAIModels(billingAdminAddr, controlToken))
	rootAPI.GET("/anthropic/v1/models", handler.OpenAIModels(billingAdminAddr, controlToken))

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
	portal.Use(middleware.PortalSession(billingAdminAddr, bc, controlToken))
	handler.RegisterPortal(portal, billingAdminAddr, agentAddr, log)
	portal.POST("/completions", middleware.Idempotency(), middleware.RateLimit(rdb, time.Minute, ratePerMin, rateBurst), handler.Chat(bc, rc, log, 2*time.Minute, holdUnknown))
	portal.POST("/responses", middleware.Idempotency(), middleware.RateLimit(rdb, time.Minute, ratePerMin, rateBurst), handler.Native(bc, rc, log, "responses", holdUnknown))
	userAuth := r.Group("/auth")
	userAuth.Use(middleware.AuthRateLimit(rdb))
	handler.RegisterUserAuth(userAuth, billingAdminAddr, log)

	// /admin/auth/*：登录/初始化/登出/会话校验，不套登录中间件（登录前即可访问）。
	// billing 收尾签发 HttpOnly cookie，经 proxy 透传给浏览器。
	auth := r.Group("/admin/auth")
	auth.Use(middleware.AuthRateLimit(rdb))
	handler.RegisterAdminAuth(auth, billingAdminAddr, log)

	// /admin/*：面板数据接口，需登录态（cookie/token/admin-demo 兜底）。
	admin := r.Group("/admin")
	admin.Use(middleware.AdminSession(billingAdminAddr, adminToken, controlToken, log))
	handler.RegisterAdmin(admin, billingAdminAddr, routerAddr, reconcilerAddr, agentAddr, log)
	// 管理面板 AI 客服：管理员没有租户 key，由 gateway 换发内部凭证（含目标租户）给 agent。
	handler.RegisterAdminChat(admin, agentAddr,
		config.Getenv("AGENT_INTERNAL_TOKEN", ""),
		uint64(config.GetenvInt64("ADMIN_DEFAULT_TENANT", 1)), log)

	log.Info("gateway listening", "port", port, "billing", billingAddr, "router", routerAddr)
	srv := &http.Server{Addr: config.ListenAddr(port), Handler: r}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
