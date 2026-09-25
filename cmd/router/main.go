// router 路由服务入口：HTTP /route?model=... 返回该模型的可用 provider。
// M2 最小版直接读 MySQL（owns providers 表）；M5 加 Redis 缓存 + 熔断。
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	"github.com/frostloom/ai-gateway/internal/router"
)

func main() {
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	port := config.Getenv("ROUTER_PORT", "9102")
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Error("connect mysql", "err", err)
		os.Exit(1)
	}
	reg := router.New(db, log)
	reg.FailThreshold = config.GetenvInt("ROUTER_FAIL_THRESHOLD", 5)
	reg.AutobanAfter = config.GetenvInt("ROUTER_AUTOBAN_AFTER", 20)
	reg.Cooldown = time.Duration(config.GetenvInt("ROUTER_COOLDOWN", 10)) * time.Second

	mux := http.NewServeMux()
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		model := r.URL.Query().Get("model")
		if model == "" {
			http.Error(w, `{"error":"model required"}`, http.StatusBadRequest)
			return
		}
		// exclude=1,2：failover 时排除本请求已试过的 provider，避免重试同一坏节点
		var exclude map[uint64]bool
		if q := r.URL.Query().Get("exclude"); q != "" {
			exclude = make(map[uint64]bool)
			for _, s := range strings.Split(q, ",") {
				if id, err := strconv.ParseUint(s, 10, 64); err == nil {
					exclude[id] = true
				}
			}
		}
		prov, err := reg.RouteExclude(model, exclude)
		if err != nil {
			log.Error("route", "model", model, "err", err)
			http.Error(w, `{"error":"route failed"}`, http.StatusInternalServerError)
			return
		}
		if prov == nil {
			http.Error(w, `{"error":"no available provider for model"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider_id": prov.ID,
			"name":        prov.Name,
			"base_url":    prov.BaseURL,
			"api_key":     prov.UpstreamKey,
		})
		log.Info("route", "model", model, "provider", prov.Name, slog.Int64("provider_id", int64(prov.ID)))
	})
	// /report：gateway 回传一次上游调用的成败，驱动熔断状态机 + AutoBan 持久化。
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ProviderID uint64 `json:"provider_id"`
			OK         bool   `json:"ok"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad report"}`, http.StatusBadRequest)
			return
		}
		reg.Report(body.ProviderID, body.OK)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
		log.Info("report", "provider_id", body.ProviderID, "ok", body.OK)
	})

	// /healthz 存活探针（网关 /admin/health 聚合用）。
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"svc":"router"}`))
	})

	mux.HandleFunc("/metrics", metrics.Handler().ServeHTTP)

	// /breakers：观测端点，导出各 provider 熔断状态（压测/场景脚本断言用）。
	mux.HandleFunc("/breakers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.States())
	})

	// /providers：面板端点，全量 provider 行 + 熔断状态。
	mux.HandleFunc("/providers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.Providers())
	})

	addr := ":" + port
	log.Info("router listening", "addr", addr)
	if err := http.ListenAndServe(addr, withMetrics(mux)); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

// withMetrics 包装 net/http mux：计请求量/延迟（router 的可观测）。
func withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		metrics.ReportRequest("router", r.Method, strconv.Itoa(sw.status), r.URL.Path, start)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
