// Package redisx Redis 客户端工厂（连接池配置统一收敛）。
// key 前缀规范：billing: / rate: / cache: / usage: / sse:
package redisx

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// New 构建带连接池配置的 Redis 客户端。
func New(addr, password string) *goredis.Client {
	return goredis.NewClient(&goredis.Options{
		Addr:         addr,
		Password:     password,
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})
}

// Ping 就绪检查。
func Ping(ctx context.Context, c *goredis.Client) error {
	return c.Ping(ctx).Err()
}
