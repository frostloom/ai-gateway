// agent AI 客服第 6 服务入口：POST /chat（Bearer 租户 key）。
// 执行层不信任 LLM：tenant 只来自 billing ValidateAPIKey，工具操作走 billing admin 且租户作用域。
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
)

func main() {
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))

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
	// 调试：AGENT_LLM_DEBUG=1 时把发给上游的请求体打到日志（排查"卡住"用）
	if config.Getenv("AGENT_LLM_DEBUG", "") == "1" {
		agent.DebugDumpRequest = true
		agent.SetDebugDump(func(size int, body string) {
			log.Info("llm request dump", "bytes", size, "body", body)
		})
	}
	// 预算撞顶重试的日志：区分「慢是因为多轮 loop」还是「反复撞顶重试」
	agent.SetBudgetLogger(func(attempt, budget int, spent time.Duration) {
		log.Warn("llm budget exhausted, retrying", "attempt", attempt, "budget", budget, "spent_ms", spent.Milliseconds())
	})
	// 每次 LLM 调用的日志（排查"卡住"用）
	agent.SetCallLogger(func(attempt, budget, msgs, tools int) {
		log.Info("llm call", "attempt", attempt, "budget", budget, "msgs", msgs, "tools", tools)
	})

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

	bc, err := client.NewBilling(billingAddr)
	if err != nil {
		log.Error("connect billing gRPC", "err", err)
		os.Exit(1)
	}
	defer bc.Close()

	tb := agent.NewToolbox(agent.NewAdminClient(adminBase))
	llm := agent.NewLLMClient(llmBase, llmKey, llmModel)
	h := agent.NewHandler(bc, tb, llm, st, rdb, log)

	// P3 Jev 判断引擎：JEV_API_KEY 配置即启用（端点/模型有默认值）。
	//
	// 三层判定与阈值（每轮对话先过一道 JEV，再按置信度分层处置）：
	//	JEV_BLOCK_MIN  合法性低于此值 → 直接拦截，不调 LLM（默认 0.30）
	//	JEV_WARN_MIN   合法性低于此值 → 放行但警告 + 强制确认（默认 0.60）
	//	JEV_INTENT_MIN 意图置信度高于此值 → 注入意图预判提示（默认 0.75）
	//	JEV_GUARD_MIN  写操作执行前的二次守门阈值（默认 0.80）
	//
	// 未配置时全部静默停用，行为回退到未接入 JEV 的状态。
	jevKey := config.Getenv("JEV_API_KEY", "")
	if jevKey != "" {
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
			"endpoint", jevEndpoint, "model", jevModel,
			"block_min", blockMin, "warn_min", warnMin,
			"intent_min", intentMin, "guard_min", guardMin)
	} else {
		log.Warn("JEV 未配置（JEV_API_KEY）：每轮判定、写操作守门与意图预判全部停用")
	}

	// 管理面板代操作：gateway 校验管理员登录态后带内部共享密钥头转发；
	// 未配置 AGENT_INTERNAL_TOKEN 时该路径关闭（只有租户 key 能用客服）。
	if tok := config.Getenv("AGENT_INTERNAL_TOKEN", ""); tok != "" {
		h.SetAdminAccess(tok, uint64(config.GetenvInt64("ADMIN_DEFAULT_TENANT", 1)))
		log.Info("admin access enabled", "default_tenant", config.GetenvInt64("ADMIN_DEFAULT_TENANT", 1))
	} else {
		log.Warn("AGENT_INTERNAL_TOKEN 未配置：管理面板暂不能用 AI 客服（仅租户 key 可用）")
	}

	// /healthz 存活探针（网关 /admin/health 聚合用）；/metrics Prometheus（含 jev 决策指标）。
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"svc":"agent"}`))
	})
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", h)

	log.Info("agent service listening",
		"port", port, "model", llm.Model(), "protocol", llm.Protocol(),
		"llm_base", llmBase, "billing", billingAddr, "admin", adminBase)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("agent serve", "err", err)
		os.Exit(1)
	}
}
