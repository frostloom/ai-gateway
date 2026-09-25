// billing 计费服务入口：gRPC 控制面（Reserve/GetBalance；Settle/Reverse 在 M3/M4）。
package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/admin"
	"github.com/frostloom/ai-gateway/internal/billing/outbox"
	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/kafka"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
	"github.com/frostloom/ai-gateway/internal/pkg/redisx"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

// brokers 解析逗号分隔的 KAFKA_BROKERS 为 []string（空 → 不启用 Kafka）。
func brokers() []string {
	raw := config.Getenv("KAFKA_BROKERS", "")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func main() {
	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	port := config.Getenv("BILLING_PORT", "9101")
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	redisAddr := config.Getenv("REDIS_ADDR", "127.0.0.1:6381")
	redisPwd := config.Getenv("REDIS_PASSWORD", "")

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Error("connect mysql", "err", err)
		os.Exit(1)
	}
	// 限连接池：压测下若无限流，gorm 默认池会疯狂新建 MySQL 连接，把 Windows
	// 临时端口占满（Only one usage of each socket address）。有上限 + 复用连接。
	// 50→100：压测发现 50 连接下吞吐卡在 ~215 req/s（并发计费排队在 MySQL 连接上），
	// 提到 100 验证瓶颈是否在池子；若无改善可回退（数字见 docs/interview/06-loadtest.md）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(100)
		// idle 必须跟上 open：高并发下查询突降时，池会把超出的空闲连接关掉、下次突发再新建，
		// 每次关→建都产生 TIME_WAIT（压测里 billing→MySQL 4403 个）。100/100 杜绝这种抖动。
		sqlDB.SetMaxIdleConns(100)
		sqlDB.SetConnMaxLifetime(3 * time.Minute)
	}
	rdb := redisx.New(redisAddr, redisPwd)
	if err := redisx.Ping(context.Background(), rdb); err != nil {
		log.Error("connect redis", "err", err)
		os.Exit(1)
	}

	st := store.NewStore(db, rdb)
	if err := st.AutoMigrate(); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}
	// 兜底：商品目录幂等填充（已存在的 model_id 不覆盖 admin 改价）。
	if err := st.SeedModels(context.Background()); err != nil {
		log.Error("seed models", "err", err)
		os.Exit(1)
	}
	// 兜底：为活跃租户补建 balance key（以 tenants.initial_quota 初始化）。
	if err := st.EnsureBalanceKeys(context.Background()); err != nil {
		log.Error("ensure balance keys", "err", err)
		os.Exit(1)
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Error("listen", "port", port, "err", err)
		os.Exit(1)
	}
	gs := grpc.NewServer()
	billingv1.RegisterBillingServiceServer(gs, server.NewService(st, log))

	// 观测 HTTP 端点（面板数据源，只读）。独立端口，不干扰 gRPC 控制面。
	adminPort := config.Getenv("BILLING_ADMIN_PORT", "9103")
	go func() {
		log.Info("billing admin listening", "port", adminPort)
		if err := http.ListenAndServe(":"+adminPort, admin.Handler(st, log)); err != nil {
			log.Error("billing admin serve", "err", err)
		}
	}()

	// 订阅续费器（M7）：扫到期的定期订阅 → 延周期 + 授予下一期额度（topup ref 幂等防重）。
	renewInterval := time.Duration(config.GetenvInt("SUB_RENEW_INTERVAL_SEC", 60)) * time.Second
	go func() {
		log.Info("subscription renewer started", "interval", renewInterval)
		ticker := time.NewTicker(renewInterval)
		defer ticker.Stop()
		for range ticker.C {
			n, err := st.RenewDueSubscriptions(context.Background())
			if err != nil {
				log.Error("renew subscriptions", "err", err)
				continue
			}
			if n > 0 {
				log.Info("renewed subscriptions", "count", n)
			}
		}
	}()

	// M10 Kafka 计费事件流：KAFKA_BROKERS 非空才启动 outbox relay（无 Kafka 环境不受影响）。
	// 记账方法（Reserve/ReportUsage/Settle/Reverse）已把 bill 行 + outbox 行同事务提交，
	// relay 轮询未投行 → produce → mark sent；失败重试，超限改投 DLQ topic。
	if bs := brokers(); len(bs) > 0 {
		topic := config.Getenv("KAFKA_TOPIC", store.TopicBillingEvents)
		dlqTopic := config.Getenv("KAFKA_DLQ_TOPIC", store.TopicBillingDLQ)
		prod := kafka.NewProducer(bs, topic, log)
		dlqProd := kafka.NewProducer(bs, dlqTopic, log)
		relay := outbox.NewRelay(st, prod, dlqProd, outbox.Config{
			Topic:        topic,
			DLQTopic:     dlqTopic,
			PollInterval: time.Duration(config.GetenvInt("OUTBOX_POLL_INTERVAL_MS", 1000)) * time.Millisecond,
			MaxAttempts:  config.GetenvInt("OUTBOX_MAX_ATTEMPTS", 5),
		}, log)
		go relay.Run(context.Background())
	} else {
		log.Info("outbox relay disabled: KAFKA_BROKERS empty")
	}

	log.Info("billing service listening", "port", port)
	if err := gs.Serve(lis); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
