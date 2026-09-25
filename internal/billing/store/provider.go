// 渠道（Provider）管理：聚合 API 式上游接入 + 成本/毛利统计。
// 渠道 = 一个真实上游（base_url + 上游 key + 支持模型 + 上游成本）。
// 毛利 = 该渠道累计售价（Σactual_quota）− 上游成本（token 数 × 分/百万token）。
package store

import (
	"context"

	"gorm.io/gorm/clause"
)

// UpsertProvider 按 name 幂等 upsert 渠道（重复名则更新转发参数/成本/权重/启停）。
// models 为 JSON 数组字符串（如 ["deepseek-chat"] 或 ["*"]）。
// OnConflict 更新时 gorm 不回填已存在行的 ID，这里按 name 取回，保证调用方拿到真实 ID。
func (s *Store) UpsertProvider(ctx context.Context, p *Provider) error {
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"base_url", "upstream_key", "cost_in_cent", "cost_out_cent", "models", "weight", "status"}),
	}).Create(p).Error; err != nil {
		return err
	}
	var got Provider
	if err := s.db.WithContext(ctx).Where("name = ?", p.Name).First(&got).Error; err != nil {
		return err
	}
	p.ID = got.ID
	return nil
}

// ProviderMargin 渠道 + 累计营收/成本/毛利。
type ProviderMargin struct {
	Provider
	SellingCent int64 `json:"selling_cent"` // 累计售价（分，Σactual_quota）
	CostCent    int64 `json:"cost_cent"`    // 累计上游成本（分）
	MarginCent  int64 `json:"margin_cent"`  // 毛利（分）= 售价 − 成本
}

// ListProvidersWithMargin 全量渠道 + 每渠道累计毛利（settle 已结算账单为准）。
func (s *Store) ListProvidersWithMargin(ctx context.Context) ([]ProviderMargin, error) {
	type agg struct {
		ProviderID uint64
		Selling    int64
		InTok      int64
		OutTok     int64
	}
	var aggs []agg
	if err := s.db.WithContext(ctx).Table("bills").
		Select("provider_id, COALESCE(SUM(actual_quota),0) AS selling, "+
			"COALESCE(SUM(prompt_tokens),0) AS in_tok, COALESCE(SUM(completion_tokens),0) AS out_tok").
		Where("phase = ? AND status = ? AND provider_id IS NOT NULL", PhaseSettle, StatusSettled).
		Group("provider_id").Scan(&aggs).Error; err != nil {
		return nil, err
	}
	byProv := make(map[uint64]agg, len(aggs))
	for _, a := range aggs {
		byProv[a.ProviderID] = a
	}

	var ps []Provider
	if err := s.db.WithContext(ctx).Order("id").Find(&ps).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderMargin, 0, len(ps))
	for _, p := range ps {
		a, ok := byProv[p.ID]
		if !ok {
			out = append(out, ProviderMargin{Provider: p})
			continue
		}
		// 成本（分）= in_tok×cost_in/1e6 + out_tok×cost_out/1e6（整型截断，演示够用）
		cost := a.InTok*p.CostInCent/1_000_000 + a.OutTok*p.CostOutCent/1_000_000
		out = append(out, ProviderMargin{Provider: p, SellingCent: a.Selling, CostCent: cost, MarginCent: a.Selling - cost})
	}
	return out, nil
}
