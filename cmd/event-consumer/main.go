// event-consumer 计费事件消费端入口（M10）：消费 Kafka billing.events（+ DLQ）→
// 幂等写 audit_events 审计明细 + 指标。
//
//	模式 1（默认）：常驻，直到 SIGTERM/SIGINT 退出。
//	模式 2（-once）：消费 -idle 秒（无新消息）后退出（脚本/CI 验证用）。
//
// 端口 EVENT_CONSUMER_PORT（默认 9107）：/healthz + /status + /metrics。
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/frostloom/ai-gateway/internal/billing/consumer"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/config"
	"github.com/frostloom/ai-gateway/internal/pkg/kafka"
	"github.com/frostloom/ai-gateway/internal/pkg/logger"
)

// brokers 解析逗号分隔的 KAFKA_BROKERS 为 []string。
func brokers() []string {
	raw := config.Getenv("KAFKA_BROKERS", "127.0.0.1:9092")
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
	var once bool
	var idleSec int
	flag.BoolVar(&once, "once", false, "消费到空闲后退出（脚本/CI 验证用）")
	flag.IntVar(&idleSec, "idle", 5, "-once 模式下无新消息多少秒后退出")
	flag.Parse()

	log := logger.New(config.Getenv("LOG_LEVEL", "info"))
	dsn := config.Getenv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3307)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local")
	topic := config.Getenv("KAFKA_TOPIC", store.TopicBillingEvents)
	dlqTopic := config.Getenv("KAFKA_DLQ_TOPIC", store.TopicBillingDLQ)
	groupID := config.Getenv("KAFKA_CONSUMER_GROUP", "billing-audit")
	b := brokers()
	if len(b) == 0 {
		log.Error("KAFKA_BROKERS empty")
		os.Exit(1)
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil {
		log.Error("connect mysql", "err", err)
		os.Exit(1)
	}
	// 消费端也限连接池（与 reconciler 同策略：多个进程别把 MySQL 连接挤爆）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(10)
		sqlDB.SetMaxIdleConns(5)
	}
	st := store.NewStore(db, nil) // 消费端只写 audit_events，不需要 Redis

	kc := kafka.NewConsumer(b, groupID, []string{topic, dlqTopic})
	defer kc.Close()
	c := consumer.New(st, kc, consumer.Config{DLQTopic: dlqTopic}, log)

	// 观测 HTTP：/healthz + /status + /metrics（端口 9107）。
	httpPort := config.Getenv("EVENT_CONSUMER_PORT", "9107")
	go func() {
		log.Info("event-consumer http listening", "port", httpPort)
		if err := http.ListenAndServe(":"+httpPort, c.HTTPHandler()); err != nil {
			log.Error("event-consumer http serve", "err", err)
		}
	}()

	log.Info("event-consumer starting", "brokers", b, "topic", topic, "dlq_topic", dlqTopic, "group", groupID)

	if once {
		// -once：跑到 ctx 超时（-idle 秒无新消息窗口）退出，用于脚本/CI 验证链路。
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(idleSec)*time.Second)
		defer cancel()
		_ = c.Run(ctx)
		log.Info("one-shot done", "stats", c.Stats())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_ = c.Run(ctx)
}
