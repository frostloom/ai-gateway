// Package trace 请求追踪：trace_id 在 context 中透传，日志/错误统一携带。
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type ctxKey struct{}

// WithID 把 trace_id 写入 context。
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// GetID 取出 trace_id；没有则返回空串。
func GetID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// NewID 生成 trace_id（128 bit hex）。
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "trace_" + hex.EncodeToString(b)
}
