// 卖方销售统计（M4）：每商品/每厂商/按天卖了多少、调用几次、几个买家。
// 口径：消耗 = settle 行的 actual_quota 求和（分）；充值 = topups 的 recharge/subscription 正项。
package store

import (
	"context"
	"time"
)

// SalesModel 单商品销售（bills.model JOIN models）。
type SalesModel struct {
	ModelID       string `json:"model_id"`
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	ConsumedCent  int64  `json:"consumed_cent"` // 销售额（分）
	Calls         int64  `json:"calls"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	ActiveTenants int64  `json:"active_tenants"` // 用过该模型的买家数
}

// SalesVendor 厂商汇总。
type SalesVendor struct {
	Vendor        string `json:"vendor"`
	ConsumedCent  int64  `json:"consumed_cent"`
	Calls         int64  `json:"calls"`
	ActiveTenants int64  `json:"active_tenants"`
}

// SalesDay 按天趋势（消耗 + 充值）。
type SalesDay struct {
	Day          string `json:"day"`
	ConsumedCent int64  `json:"consumed_cent"`
	TopupCent    int64  `json:"topup_cent"`
}

// SalesStats 销售统计整体。
type SalesStats struct {
	Summary  SalesSummary  `json:"summary"`
	ByModel  []SalesModel  `json:"by_model"`
	ByVendor []SalesVendor `json:"by_vendor"`
	ByDay    []SalesDay    `json:"by_day"`
}

// SalesSummary 汇总卡。
type SalesSummary struct {
	TopupCent     int64 `json:"topup_cent"`     // 充值总额（分）
	ConsumedCent  int64 `json:"consumed_cent"`  // 消耗总额（分）
	SettledBills  int64 `json:"settled_bills"`  // 结算单量
	ActiveTenants int64 `json:"active_tenants"` // 有消耗的活跃买家
}

// SalesStats 按时间窗统计。from/to 为空则近 30 天。
func (s *Store) SalesStats(ctx context.Context, from, to time.Time) (*SalesStats, error) {
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() || from.After(to) {
		from = to.Add(-30 * 24 * time.Hour)
	}
	out := &SalesStats{}

	// 按模型
	if err := s.db.WithContext(ctx).Model(&Bill{}).
		Select("bills.model AS model_id, COALESCE(models.name,'') AS name, COALESCE(models.vendor,'') AS vendor, "+
			"COALESCE(SUM(bills.actual_quota),0) AS consumed_cent, COUNT(*) AS calls, "+
			"COALESCE(SUM(bills.prompt_tokens),0) AS input_tokens, COALESCE(SUM(bills.completion_tokens),0) AS output_tokens, "+
			"COUNT(DISTINCT bills.tenant_id) AS active_tenants").
		Joins("LEFT JOIN models ON models.model_id = bills.model").
		Where("bills.phase = ? AND bills.status = ? AND bills.created_at >= ? AND bills.created_at <= ?",
			PhaseSettle, StatusSettled, from, to).
		Group("bills.model, models.name, models.vendor").
		Order("consumed_cent DESC").
		Scan(&out.ByModel).Error; err != nil {
		return nil, err
	}

	// 按厂商
	if err := s.db.WithContext(ctx).Model(&Bill{}).
		Select("COALESCE(models.vendor,'') AS vendor, COALESCE(SUM(bills.actual_quota),0) AS consumed_cent, "+
			"COUNT(*) AS calls, COUNT(DISTINCT bills.tenant_id) AS active_tenants").
		Joins("LEFT JOIN models ON models.model_id = bills.model").
		Where("bills.phase = ? AND bills.status = ? AND bills.created_at >= ? AND bills.created_at <= ?",
			PhaseSettle, StatusSettled, from, to).
		Group("models.vendor").
		Order("consumed_cent DESC").
		Scan(&out.ByVendor).Error; err != nil {
		return nil, err
	}

	// 汇总
	row := s.db.WithContext(ctx).Model(&Bill{}).
		Select("COALESCE(SUM(actual_quota),0) AS consumed_cent, COUNT(*) AS settled_bills, COUNT(DISTINCT tenant_id) AS active_tenants").
		Where("phase = ? AND status = ? AND created_at >= ? AND created_at <= ?",
			PhaseSettle, StatusSettled, from, to)
	if err := row.Scan(&out.Summary).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&Topup{}).
		Where("amount > 0 AND created_at >= ? AND created_at <= ?", from, to).
		Select("COALESCE(SUM(amount),0)").Scan(&out.Summary.TopupCent).Error; err != nil {
		return nil, err
	}

	// 按天（消耗）
	//
	// 关键：分桶必须用「应用侧时区」而不是 MySQL 会话时区。
	// DATE_FORMAT(created_at) 用的是数据库服务器时区（本地 compose 的 MySQL 容器
	// 跑在 UTC），而写入时 DSN 带 loc=Local，值是本地时间字面量。两者错开 8 小时后，
	// 分桶键会比补齐循环生成的本地日期早一天，真实数据全部匹配不上 —— 前端趋势恒为 0。
	// 这里把 created_at 先减去连接时区偏移再格式化，使分桶键与 from/to 同一口径。
	type dayRow struct {
		Day    string
		Amount int64
	}
	byDay := map[string]*SalesDay{}
	_, offsetSec := from.Zone()
	dayExpr := "DATE_FORMAT(DATE_SUB(created_at, INTERVAL ? SECOND), '%Y-%m-%d')"
	rows, err := s.db.WithContext(ctx).Model(&Bill{}).
		Select(dayExpr+" AS day, COALESCE(SUM(actual_quota),0) AS amount", offsetSec).
		Where("phase = ? AND status = ? AND created_at >= ? AND created_at <= ?",
			PhaseSettle, StatusSettled, from, to).
		Group("day").Order("day ASC").Rows()
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r dayRow
		if err := rows.Scan(&r.Day, &r.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		byDay[r.Day] = &SalesDay{Day: r.Day, ConsumedCent: r.Amount}
	}
	rows.Close()

	// 按天（充值）叠加
	rows, err = s.db.WithContext(ctx).Model(&Topup{}).
		Select(dayExpr+" AS day, COALESCE(SUM(amount),0) AS amount", offsetSec).
		Where("amount > 0 AND created_at >= ? AND created_at <= ?", from, to).
		Group("day").Order("day ASC").Rows()
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r dayRow
		if err := rows.Scan(&r.Day, &r.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		d := byDay[r.Day]
		if d == nil {
			d = &SalesDay{Day: r.Day}
			byDay[r.Day] = d
		}
		d.TopupCent = r.Amount
	}
	rows.Close()

	// 补齐缺失天：按 from 所在时区的自然日逐个生成 key，
	// 与上面的 dayExpr 保持同一口径，避免真实桶落到占位之外。
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	out.ByDay = make([]SalesDay, 0, 31)
	for d := start; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if dd, ok := byDay[key]; ok {
			out.ByDay = append(out.ByDay, *dd)
		} else {
			out.ByDay = append(out.ByDay, SalesDay{Day: key})
		}
	}
	return out, nil
}
