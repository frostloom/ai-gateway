// Package tokens 粗略 token 估算（估算即可，结算以 provider 上报的 usage 为准）。
// 关键是口径一致：gateway 预占估算 与 mock-provider 上报必须用同一个函数，
// 否则 settle 的 delta 会系统性偏大/偏小。
package tokens

// Estimate 按 rune 数粗略估算 token 数（中文约 2 字符/token，简化按 /2）。
// 真实场景用 tiktoken 等分词器；这里只保证「估算与上报同口径」。
func Estimate(text string) int64 {
	return int64(len([]rune(text))) / 2
}
