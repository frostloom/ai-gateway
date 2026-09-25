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
func NewProducer(brokers []string, topic string, log *slog.Logger) *Producer {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{}, // key=request_id → 同分区保序
		RequiredAcks: kafka.RequireAll,
		MaxAttempts:  3,
		BatchTimeout: 10 * time.Millisecond,
	}
	return &Producer{w: w, log: log}
}

// Publish 发一条事件（key=幂等键 {request_id}:{phase}，value=事件 JSON）。
func (p *Producer) Publish(ctx context.Context, key string, value []byte) error {
	if err := p.w.WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: value}); err != nil {
		return err
	}
	return nil
}

// Close 关闭写入端（刷出缓冲）。Relay 退出时调用。
func (p *Producer) Close() error {
	return p.w.Close()
}
