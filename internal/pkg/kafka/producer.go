// Package kafka 计费事件流的 kafka-go 客户端封装（M10）。
//
// Producer：单 topic 写入。Hash 分区器 + key=request_id → 同一请求的事件落到同一分区
// （同分区保序：reserve → usage → settle 严格先后）；RequiredAcks=All 等 broker 落盘。
// Consumer：消费组模式，at-least-once（FetchMessage + CommitMessages 显式提交）。
package kafka

import (
	"context"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
)

// Producer 单 topic 写入端（billing relay 用）。
type Producer struct {
	w   *kafka.Writer
	log *slog.Logger
}

// NewProducer 建写入端。topic 固定（billing.events 或 billing.events.dlq 各一个实例）。
// RequireAll：relay 补投时要等 broker 确认。
func NewProducer(brokers []string, topic string, log *slog.Logger) *Producer {
	return newProducer(brokers, topic, log, kafka.RequireAll, 0)
}

// NewRequestLogProducer 每条请求日志单独发送，并等待 broker 确认。
func NewRequestLogProducer(brokers []string, topic string, log *slog.Logger) *Producer {
	return newProducer(brokers, topic, log, kafka.RequireAll, 1)
}

// NewCopyProducer 用于已经写进 MySQL 的事件副本。账单事务自己有耐久性，副本用 RequireOne，避免和 MySQL 刷盘抢磁盘。
func NewCopyProducer(brokers []string, topic string, log *slog.Logger) *Producer {
	return newProducer(brokers, topic, log, kafka.RequireOne, 0)
}

func newProducer(brokers []string, topic string, log *slog.Logger, acks kafka.RequiredAcks, batchSize int) *Producer {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{}, // key=request_id → 同分区保序
		RequiredAcks: acks,
		MaxAttempts:  3,
		BatchSize:    batchSize,
		BatchTimeout: 10 * time.Millisecond,
	}
	return &Producer{w: w, log: log}
}

// Publish 发一条事件（key=幂等键 {request_id}:{phase}，value=事件 JSON）。
func (p *Producer) Publish(ctx context.Context, key string, value []byte) error {
	return p.PublishMany(ctx, []Record{{Key: key, Value: value}})
}

// Record 是一条待写 Kafka 的消息。批量刷账时 key 用 request_id，同一请求的预扣和结算进同一分区。
type Record struct {
	Key   string
	Value []byte
}

// PublishMany 一次写入一批消息。空批次直接返回。
func (p *Producer) PublishMany(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	msgs := make([]kafka.Message, len(records))
	for i, rec := range records {
		msgs[i] = kafka.Message{Key: []byte(rec.Key), Value: rec.Value}
	}
	return p.w.WriteMessages(ctx, msgs...)
}

// Close 关闭写入端（刷出缓冲）。Relay 退出时调用。
func (p *Producer) Close() error {
	return p.w.Close()
}
