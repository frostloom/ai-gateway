// billing 计费服务入口：gRPC 控制面（Reserve/GetBalance；Settle/Reverse 在 M3/M4）。
package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
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

// kafkaLedgerSink 把每笔请求日志写入 billing.events。key 用租户，同一用户的明细进同一分区。
type kafkaLedgerSink struct{ p *kafka.Producer }

func (k kafkaLedgerSink) PublishLedger(ctx context.Context, events []*store.BillingEvent) error {
	records := make([]kafka.Record, 0, len(events))
	for _, ev := range events {
		if ev == nil {
			continue
		}
		body, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		records = append(records, kafka.Record{Key: store.KafkaMessageKey(ev), Value: body})
	}
	return k.p.PublishMany(ctx, records)
}

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
	controlToken := controlplane.Require(log)
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	redisAddr := config.Getenv("REDIS_ADDR", "127.0.0.1:6381")
	redisPwd := config.Getenv("REDIS_PASSWORD", "")
	requireReserveV2 := config.Getenv("BILLING_REQUIRE_RESERVE_V2", "0")
	authCacheSeconds, err := strconv.Atoi(config.Getenv("BILLING_AUTH_CACHE_TTL_SECONDS", "60"))
	if err != nil || authCacheSeconds < 0 || authCacheSeconds > 60 {
		log.Error("BILLING_AUTH_CACHE_TTL_SECONDS must be between 0 and 60")
		os.Exit(1)
	}
	batchSeconds, err := strconv.Atoi(config.Getenv("BILLING_LEDGER_BATCH_SECONDS", "5"))
	if err != nil || batchSeconds < 0 || batchSeconds > 60 {
		log.Error("BILLING_LEDGER_BATCH_SECONDS must be between 0 and 60")
		os.Exit(1)
	}
	if requireReserveV2 != "0" && requireReserveV2 != "1" {
		log.Error("BILLING_REQUIRE_RESERVE_V2 must be 0 or 1")
		os.Exit(1)
	}
	if config.Getenv("BILLING_LEDGER_VIA_KAFKA", "0") == "1" {
		log.Error("BILLING_LEDGER_VIA_KAFKA must stay off")
		os.Exit(1)
	}

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
	if batchSeconds > 0 {
		st.EnableLedgerBatch(time.Duration(batchSeconds) * time.Second)
		log.Warn("mysql ledger batch enabled; redis balance stays authoritative until flush", "seconds", batchSeconds)
	}
	if authCacheSeconds > 0 {
		st.EnableAuthCache(time.Duration(authCacheSeconds) * time.Second)
		log.Warn("ordinary API key memory cache enabled; revocation may be delayed", "ttl_seconds", authCacheSeconds)
	}
	if err := st.AutoMigrate(); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}
	// Model pricing is configured explicitly by administrators after an upstream is added.
	// 兜底：为活跃租户补建 balance key（以 tenants.initial_quota 初始化）。
	if err := st.EnsureBalanceKeys(context.Background()); err != nil {
		log.Error("ensure balance keys", "err", err)
		os.Exit(1)
	}

	lis, err := net.Listen("tcp", config.ListenAddr(port))
	if err != nil {
		log.Error("listen", "port", port, "err", err)
		os.Exit(1)
	}
	gs := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer(controlToken)))
	var serviceOptions []server.Option
	if requireReserveV2 == "1" {
		serviceOptions = append(serviceOptions, server.RequireReserveV2())
	}
	log.Info("billing reserve contract", "require_v2", requireReserveV2 == "1")
	billingv1.RegisterBillingServiceServer(gs, server.NewService(st, log, serviceOptions...))

	// 观测 HTTP 端点（面板数据源，只读）。独立端口，不干扰 gRPC 控制面。
	adminPort := config.Getenv("BILLING_ADMIN_PORT", "9103")
	go func() {
		log.Info("billing admin listening", "port", adminPort)
		if err := http.ListenAndServe(config.ListenAddr(adminPort), admin.Handler(st, log, controlToken)); err != nil {
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

	// KAFKA_BROKERS 非空才启用。每笔明细异步送 Kafka，RequireAll 等 broker 确认；outbox 只承接发送失败。
	// 未开批量时，账单事务仍写 outbox，由 relay 轮询投递。失败重试，超限改投 DLQ。
	if bs := brokers(); len(bs) > 0 {
		topic := config.Getenv("KAFKA_TOPIC", store.TopicBillingEvents)
		dlqTopic := config.Getenv("KAFKA_DLQ_TOPIC", store.TopicBillingDLQ)
		prod := kafka.NewProducer(bs, topic, log)
		dlqProd := kafka.NewProducer(bs, dlqTopic, log)
		if batchSeconds > 0 {
			st.AttachLedgerSink(kafkaLedgerSink{p: kafka.NewRequestLogProducer(bs, topic, log)})
			if config.Getenv("BILLING_LEDGER_VIA_KAFKA", "0") == "1" {
				st.EnableLedgerQueue()
				log.Info("redis debit is queued to kafka; the consumer writes mysql")
			} else {
				log.Info("each request log is queued to kafka without waiting; mysql balance still flushes on the interval; outbox is only the fallback")
			}
		}
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
