// reconciler 对账服务入口：周期扫描滞留 pending 账单并收敛（M4）。
//
//	模式 1（默认）：常驻，每 -interval 跑一轮 sweep + rebuild；
//	模式 2（-once）：只跑一轮就退出（场景脚本/手动验证用）。
//
// 读：MySQL bills 只读扫描。写：一律走 billing 幂等 RPC。
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func main() {
	var once, noSweep, noRebuild bool
	var intervalSec, graceSec int
	flag.BoolVar(&once, "once", false, "只跑一轮就退出（场景脚本用）")
	flag.BoolVar(&noSweep, "no-sweep", false, "跳过 sweep（只跑 rebuild）")
	flag.BoolVar(&noRebuild, "no-rebuild", false, "跳过 rebuild（只跑 sweep，压测期间用）")
	flag.IntVar(&intervalSec, "interval", 30, "对账周期（秒）")
	flag.IntVar(&graceSec, "grace", 600, "宽限期（秒）：created_at 早于 now-grace 才处理")
	flag.Parse()

	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	billingAddr := config.Getenv("BILLING_ADDR", "127.0.0.1:9101")

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Error("connect mysql", "err", err)
		os.Exit(1)
	}
	// 对账也是常驻进程，同样限连接池，避免多个进程一起把 MySQL 连接挤爆（端口耗尽）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(10)
		sqlDB.SetMaxIdleConns(5)
	}
	conn, err := grpc.NewClient(billingAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("connect billing", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	r := reconciler.New(db, billingv1.NewBillingServiceClient(conn), log, time.Duration(graceSec)*time.Second)

	// 观测 HTTP：/healthz + /status（端口 9106，供网关 /admin/health 聚合与运维探活）。
	httpPort := config.Getenv("RECONCILER_PORT", "9106")
	go func() {
		log.Info("reconciler http listening", "port", httpPort)
		if err := http.ListenAndServe(":"+httpPort, r.HTTPHandler()); err != nil {
			log.Error("reconciler http serve", "err", err)
		}
	}()

	log.Info("reconciler starting", "once", once, "interval", intervalSec, "grace", graceSec)

	if once {
		var s reconciler.Stats
		var err error
		if !noSweep {
			s, err = r.SweepOnce(context.Background())
			if err != nil {
				log.Error("sweep", "err", err)
			}
		}
		var rb reconciler.Stats
		if !noRebuild {
			rb, err = r.RebuildAll(context.Background())
			if err != nil {
				log.Error("rebuild", "err", err)
			}
		}
		log.Info("one-shot done", "scanned", s.Scanned, "reversed", s.Reversed,
			"settled", s.Settled, "rebuilt", rb.Rebuilt)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r.Run(ctx, time.Duration(intervalSec)*time.Second, noRebuild)
}
