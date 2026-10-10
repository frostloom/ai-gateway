// router 路由服务入口：HTTP /route?model=... 返回该模型的可用 provider。
// M2 最小版直接读 MySQL（owns providers 表）；M5 加 Redis 缓存 + 熔断。
package main

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/metrics"
	"github.com/frostloom/ai-gateway/internal/router"
)

func main() {
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	port := config.Getenv("ROUTER_PORT", "9102")
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	token := controlplane.Require(log)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Error("connect mysql", "err", err)
		os.Exit(1)
	}
	reg := router.New(db, log)
	reg.FailThreshold = config.GetenvInt("ROUTER_FAIL_THRESHOLD", 5)
	reg.AutobanAfter = config.GetenvInt("ROUTER_AUTOBAN_AFTER", 20)
	reg.Cooldown = time.Duration(config.GetenvInt("ROUTER_COOLDOWN", 10)) * time.Second

	mux := router.NewHTTPHandler(reg, token, log)
	mux.Handle("/metrics", metrics.Handler())

	addr := config.ListenAddr(port)
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
