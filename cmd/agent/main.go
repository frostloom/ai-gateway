// agent AI 客服第 6 服务入口：POST /chat（Bearer 租户 key）。
// 执行层不信任 LLM：tenant 只来自 billing ValidateAPIKey，工具操作走 billing admin 且租户作用域。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		if err := healthcheck("http://127.0.0.1:" + config.Getenv("AGENT_PORT", "9105") + "/healthz"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	controlToken := controlplane.Require(log)

	port := config.Getenv("AGENT_PORT", "9105")
	llmBase := config.Getenv("AGENT_LLM_BASE_URL", "https://api.deepseek.com")
	llmKey := config.Getenv("AGENT_LLM_API_KEY", "")
	llmModel := config.Getenv("AGENT_LLM_MODEL", "deepseek-v4-flash")
	billingAddr := config.Getenv("BILLING_ADDR", "127.0.0.1:9101")
	adminBase := config.Getenv("BILLING_ADMIN_ADDR", "http://127.0.0.1:9103")
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	redisAddr := config.Getenv("REDIS_ADDR", "127.0.0.1:6381")
	redisPwd := config.Getenv("REDIS_PASSWORD", "")

	if llmKey == "" {
		log.Warn("AGENT_LLM_API_KEY 未配置：AI 客服可用但每次会话会返回明确错误（请在 env 填入密钥）")
	}
	// 协议可由 AGENT_LLM_PROTOCOL 显式指定（openai | anthropic）；
	// 留空则按 base URL 自动推断（含 /anthropic 即 Anthropic Messages）。
	agent.SetProtocolFromEnv(func() string { return config.Getenv("AGENT_LLM_PROTOCOL", "") })
	// 输出预算：带 thinking 的模型推理也计入 max_tokens，给小了会「只有思考、没有输出」。
	agent.SetMaxTokensFromEnv(func() int { return int(config.GetenvInt64("AGENT_LLM_MAX_TOKENS", 0)) })
	// 调试：AGENT_LLM_DEBUG=1 时把发给上游的请求体落盘（排查"卡住"用）
	if config.Getenv("AGENT_LLM_DEBUG", "") == "1" {
		agent.DebugDumpRequest = true
		dir := config.Getenv("AGENT_LLM_DEBUG_DIR", "logs/llm-dump")
		_ = os.MkdirAll(dir, 0o755)
		var seq int
		// 落**原始字节**：日志里的 %q 转义无法可靠还原（真实换行会与字符串
		// 字面量混淆），要原样重放诊断就必须存未转义的 body。
		agent.SetDebugDumpFile(func(raw []byte) {
			seq++
			p := filepath.Join(dir, fmt.Sprintf("req-%03d.json", seq))
			if err := os.WriteFile(p, raw, 0o644); err != nil {
				log.Error("dump request", "err", err)
			}
		})
		log.Warn("LLM debug dump enabled", "dir", dir)
	}
	// 预算撞顶重试的日志：区分「慢是因为多轮 loop」还是「反复撞顶重试」
	agent.SetBudgetLogger(func(attempt, budget int, spent time.Duration) {
		log.Warn("llm budget exhausted, retrying", "attempt", attempt, "budget", budget, "spent_ms", spent.Milliseconds())
	})
	// 每次 LLM 调用的日志（排查"卡住"用）
	agent.SetCallLogger(func(attempt, budget, msgs, tools int) {
		log.Info("llm call", "attempt", attempt, "budget", budget, "msgs", msgs, "tools", tools)
	})

	tb := agent.NewToolbox(agent.NewAdminClient(adminBase))
	if err := configureKnowledge(tb, os.Getenv); err != nil {
		log.Error("knowledge initialization failed", "err", err)
		os.Exit(1)
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Error("connect mysql", "err", err)
		os.Exit(1)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(20)
		sqlDB.SetMaxIdleConns(20)
		sqlDB.SetConnMaxLifetime(3 * time.Minute)
	}
	rdb := redisx.New(redisAddr, redisPwd)
	if err := redisx.Ping(context.Background(), rdb); err != nil {
		log.Error("connect redis", "err", err)
		os.Exit(1)
	}
	st := store.NewStore(db, rdb)

	bc, err := client.NewBilling(billingAddr, controlToken)
	if err != nil {
		log.Error("connect billing gRPC", "err", err)
		os.Exit(1)
	}
	defer bc.Close()

	llm := agent.NewLLMClient(llmBase, llmKey, llmModel)
	h := agent.NewHandler(bc, tb, llm, st, rdb, log)
	routingMode := config.Getenv("AGENT_ROUTING_MODE", string(agent.RoutingJevSelective))
	fastPathEnabled := config.Getenv("JEV_FAST_PATH", "0") == "1"
	fastPathMin := config.GetenvFloat("JEV_FAST_PATH_MIN", 0.90)
	// Legacy flags remain available for historical runs. The primary selective
	// mode always enables read routing; model/hint never execute it.
	if err := configureRouting(h, routingMode, fastPathEnabled, fastPathMin); err != nil {
		log.Error("invalid routing configuration", "err", err)
		os.Exit(1)
	}
	log.Info("agent routing policy", "mode", h.RoutingMode(), "confidence_min", fastPathMin,
		"legacy_fast_path", fastPathEnabled)

	// JEV 只在非 model 路由模式下启用（端点/模型有默认值）。
	//
	// 以下安全/意图阈值仅用于显式 legacy；jev_hint 仅使用 intentMin。
	//	JEV_BLOCK_MIN  合法性低于此值 → 直接拦截，不调 LLM（默认 0.30）
	//	JEV_WARN_MIN   合法性低于此值 → 放行但警告 + 强制确认（默认 0.60）
	//	JEV_INTENT_MIN 意图置信度高于此值 → 注入意图预判提示（默认 0.75）
	//	JEV_GUARD_MIN  写操作执行前的二次守门阈值（默认 0.80）
	//
	// 未配置时显式记录模型回退，不把这类运行当作正常分流实验。
	jevKey := config.Getenv("JEV_API_KEY", "")
	if h.RoutingMode() == agent.RoutingModel {
		log.Info("model routing: JEV disabled regardless of configured API key")
	} else if jevKey != "" {
		jevEndpoint := config.Getenv("JEV_ENDPOINT", jev.DefaultEndpoint)
		jevModel := config.Getenv("JEV_MODEL", jev.DefaultModel)

		guardMin := config.GetenvFloat("JEV_GUARD_MIN", 0.8)
		blockMin := config.GetenvFloat("JEV_BLOCK_MIN", 0.30)
		warnMin := config.GetenvFloat("JEV_WARN_MIN", 0.60)
		intentMin := config.GetenvFloat("JEV_INTENT_MIN", 0.75)

		h.SetJev(jev.NewClient(jevEndpoint, jevKey, jevModel))
		h.SetJevThresholds(blockMin, warnMin, intentMin)
		h.SetJevGuardMin(guardMin)

		log.Info("jev engine enabled",
			"endpoint", jevEndpoint, "model", jevModel, "routing_mode", h.RoutingMode(),
			"block_min", blockMin, "warn_min", warnMin,
			"intent_min", intentMin, "guard_min", guardMin)
	} else {
		log.Warn("JEV 未配置（JEV_API_KEY）：回退模型；此运行不能作为正常 JEV 分流效果证据",
			"routing_mode", h.RoutingMode(), "fallback_reason", "judge_unconfigured")
	}

	// 管理面板代操作：gateway 校验管理员登录态后带内部共享密钥头转发；
	// 未配置 AGENT_INTERNAL_TOKEN 时该路径关闭（只有租户 key 能用客服）。
	if tok := config.Getenv("AGENT_INTERNAL_TOKEN", ""); tok != "" {
		h.SetAdminAccess(tok, uint64(config.GetenvInt64("ADMIN_DEFAULT_TENANT", 1)))
		log.Info("admin access enabled", "default_tenant", config.GetenvInt64("ADMIN_DEFAULT_TENANT", 1))
	} else {
		log.Warn("AGENT_INTERNAL_TOKEN 未配置：管理面板暂不能用 AI 客服（仅租户 key 可用）")
	}

	observer, observeErr := configureObservability(context.Background(), os.Getenv)
	if observeErr != nil {
		log.Warn("Langfuse disabled: invalid observability configuration")
	} else if observer != nil {
		h.SetObserver(observer)
		log.Info("Langfuse observability enabled", "content_capture", config.Getenv("AGENT_LANGFUSE_CAPTURE_CONTENT", "false"))
	}

	// /healthz 存活探针（网关 /admin/health 聚合用）；/metrics Prometheus（含 jev 决策指标）。
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"svc":"agent"}`))
	})
	metrics := promhttp.Handler()
	if observer != nil {
		metrics = promhttp.HandlerFor(prometheus.Gatherers{prometheus.DefaultGatherer, observer.Gatherer()}, promhttp.HandlerOpts{})
	}
	mux.Handle("/metrics", metrics)
	mux.Handle("/", h)

	log.Info("agent service listening",
		"port", port, "model", llm.Model(), "protocol", llm.Protocol(),
		"llm_base", llmBase, "billing", billingAddr, "admin", adminBase)
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: config.ListenAddr(port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe() }()
	var serveErr error
	select {
	case serveErr = <-served:
	case <-stopCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
		cancel()
		serveErr = <-served
	}
	flushCtx, cancelFlush := context.WithTimeout(context.Background(), 5*time.Second)
	if err := observer.Shutdown(flushCtx); err != nil {
		log.Warn("Langfuse shutdown incomplete", "stats", observer.Stats())
	}
	cancelFlush()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Error("agent serve", "err", serveErr)
		os.Exit(1)
	}
}

func configureRouting(h *agent.Handler, mode string, fastPathEnabled bool, fastPathMin float64) error {
	// Select the requested mode before validating its threshold, because the
	// production constructor starts in selective mode, including for legacy runs.
	if err := h.SetRoutingMode(mode); err != nil {
		return err
	}
	return h.SetJevFastPath(fastPathEnabled || h.RoutingMode() == agent.RoutingJevSelective, fastPathMin)
}

func configureObservability(ctx context.Context, get func(string) string) (*observability.Observer, error) {
	cfg, err := observability.ConfigFromEnv(get)
	if err != nil {
		return nil, err
	}
	return observability.New(ctx, cfg)
}
