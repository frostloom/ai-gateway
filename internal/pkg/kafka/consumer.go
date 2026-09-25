// Consumer：消费组读取（event-consumer 用）。at-least-once 语义由上层控制：
// FetchMessage 拉一条 → 处理成功 CommitMessages 提交 → 处理失败不提交（下次重投）。
// 同一 groupID 多副本时按分区瓜分，broker 负责 rebalance。
package kafka

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
)

// Consumer 消费组读取端。
type Consumer struct {
	r *kafka.Reader
}

// NewConsumer 建读取端。topics 为订阅的 topic 列表（计费主 topic + DLQ topic），
// 消费端按 msg.Topic 区分来源。StartOffset=FirstOffset：消费组新建立时从最旧开始
// （配合 audit uk_event_id 幂等，重放无害）。
func NewConsumer(brokers []string, groupID string, topics []string) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		GroupID:        groupID,
		GroupTopics:    topics,
		MinBytes:       1,
		MaxBytes:       10e6, // 10MB
		CommitInterval: time.Second,
		StartOffset:    kafka.FirstOffset,
	})
	return &Consumer{r: r}
}

// FetchMessage 拉一条消息（阻塞）。返回的 Message 含 Topic/Partition/Offset/Key/Value。
func (c *Consumer) FetchMessage(ctx context.Context) (kafka.Message, error) {
	return c.r.FetchMessage(ctx)
}

// CommitMessages 显式提交已处理消息的 offset。
func (c *Consumer) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	return c.r.CommitMessages(ctx, msgs...)
}

// Lag 消费落后量（本 group 未消费消息数）。供 /metrics 的 lag gauge。
func (c *Consumer) Lag(ctx context.Context) (int64, error) {
	return c.r.Lag(), nil
}

// Close 关闭读取端（触发 rebalance，offset 已提交的不会重复）。
func (c *Consumer) Close() error {
	return c.r.Close()
}
