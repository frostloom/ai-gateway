// Package tokens 给预扣和本地 mock 计数。
// 预扣走 Count，口径与 New API tokenkit 相同。
// Estimate 仍给 mock-provider，让本地替身的上报和旧口径保持一致。
package tokens

// Estimate 按 rune 数粗略估算 token 数（中文约 2 字符/token，简化按 /2）。
// mock-provider 继续用它上报 usage。真实预扣用 Count。
func Estimate(text string) int64 {
	return int64(len([]rune(text))) / 2
}
