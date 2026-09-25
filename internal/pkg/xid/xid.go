// Package xid 生成全局唯一 ID（request_id / 幂等键）。
package xid

import (
	"crypto/rand"
	"encoding/hex"
)

// New 生成全局唯一 request_id。
// 用 crypto/rand 的 16 字节（128 bit）随机数——无需时钟同步即可在分布式下保证唯一，
// 作为幂等键时不会因时间回拨/多机同秒产生碰撞。
func New() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 在主流平台不失败；失败属系统级问题，直接 panic 暴露。
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "req_" + hex.EncodeToString(b)
}
