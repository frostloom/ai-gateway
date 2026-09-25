// Package quota 按模型单价计价。内部货币单位 = 分（0.01 元），价格表存 分/百万 token，
// 全链路 int64 整数定点运算，避免浮点在 SQL/Redis 里漂移（金额/额度一律定点，不做浮点）。
//
// 成本公式：
//
//	cost(分) = (prompt_tokens×in_price + completion_tokens×out_price) / 1_000_000
//
// 例：deepseek-v4-flash 输入 100 分/M、输出 200 分/M，调用 500 prompt + 500 completion token
//
//	→ (500×100 + 500×200) / 1_000_000 = 0 分（demo 需 MOCK_COMPLETION_TOKENS 加大才能看到扣费）
package quota

const (
	// CentPerYuan 1 元 = 100 分。
	CentPerYuan = 100
	// TokensPerM 百万 token（价格单位的分母）。
	TokensPerM = 1_000_000
	// ReserveMargin 预扣安全系数 ×1.2（预扣带裕量，settle 时多退少补）。
	ReserveMargin = 12 // ×1.2 = 12/10
)

// CostCent 按实际用量与单价计算成本（分，向下取整）。
// inCent/outCent 为 分/百万 token。
func CostCent(promptTokens, completionTokens, inCent, outCent int64) int64 {
	return (promptTokens*inCent + completionTokens*outCent) / TokensPerM
}

// ReserveCostCent 计算预扣金额（分）：按估算用量计，乘 1.2 安全系数。
// 输出按估算值预扣（settle 时按实际补扣或退回）。
func ReserveCostCent(estPrompt, estCompletion, inCent, outCent int64) int64 {
	return CostCent(estPrompt, estCompletion, inCent, outCent) * ReserveMargin / 10
}
