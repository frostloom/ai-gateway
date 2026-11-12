package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// ListSupportedModels intersects the published price catalog with enabled upstreams.
func (s *Store) ListSupportedModels(ctx context.Context, vendor string) ([]Model, error) {
	enabled := int8(0)
	return s.ListConfiguredModels(ctx, vendor, &enabled)
}

// ListConfiguredModels keeps management and public catalogs bound to explicit upstream models.
func (s *Store) ListConfiguredModels(ctx context.Context, vendor string, status *int8) ([]Model, error) {
	models, err := s.ListModels(ctx, vendor, status)
	if err != nil {
		return nil, err
	}
	return s.configuredModels(ctx, models)
}

func (s *Store) configuredModels(ctx context.Context, models []Model) ([]Model, error) {
	var providers []Provider
	if err := s.db.WithContext(ctx).Where("status = ?", 0).Find(&providers).Error; err != nil {
		return nil, err
	}
	return supportedCatalog(models, providers), nil
}

// ModelConfigured requires an explicit model ID, never a routing wildcard.
func (s *Store) ModelConfigured(ctx context.Context, modelID string) (bool, error) {
	rows, err := s.configuredModels(ctx, []Model{{ModelID: modelID}})
	return len(rows) > 0, err
}

func supportedCatalog(models []Model, providers []Provider) []Model {
	supported := map[string]bool{}
	for _, p := range providers {
		if p.Status != 0 || p.Weight <= 0 || p.Weight > 1000000 || strings.TrimSpace(p.BaseURL) == "" {
			continue
		}
		var names []string
		if json.Unmarshal([]byte(p.Models), &names) != nil {
			continue
		}
		for _, name := range names {
			supported[name] = true
		}
	}
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if m.ModelID != "*" && supported[m.ModelID] {
			out = append(out, m)
		}
	}
	return out
}

func ValidateConsolePlan(p *Plan) error {
	ids, err := p.ModelScope()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		raw, _ := json.Marshal(ids)
		p.AllowedModels = string(raw)
	} else {
		p.AllowedModels = "[]"
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len([]rune(p.Name)) > 64 || p.PlanType != PlanTypeRecurring || p.PriceMoney < 0 || p.PriceMoney > 1000000 || p.ValidityDays < 1 || p.ValidityDays > 366 || p.RefreshHours < 1 || p.RefreshHours > 744 || (p.Status != 0 && p.Status != 1) {
		return errors.New("请检查名称、价格、周期、刷新小时和状态")
	}
	if p.WeekQuotaCent < 0 || p.WeekQuotaCent > 100000000 || (p.WeekQuotaCent > 0 && p.WindowQuotaCent > p.WeekQuotaCent) || (p.CycleQuotaCent > 0 && p.WeekQuotaCent > p.CycleQuotaCent) || p.WindowQuotaCent < 0 || p.CycleQuotaCent < 0 || p.WindowQuotaCent > 100000000 || p.CycleQuotaCent > 100000000 || (p.WindowQuotaCent > 0 && p.CycleQuotaCent > 0 && p.WindowQuotaCent > p.CycleQuotaCent) {
		return errors.New("请检查窗口与周期金额额度，窗口额度不得超过周期额度")
	}
	if p.WindowQuotaCent > 0 || p.WeekQuotaCent > 0 || p.CycleQuotaCent > 0 {
		p.TierQuota = "{}"
		return nil
	}
	var quotas map[string]int64
	if json.Unmarshal([]byte(p.TierQuota), &quotas) != nil || len(quotas) == 0 {
		return errors.New("请配置有效的模型档次额度")
	}
	for tier, quota := range quotas {
		if strings.TrimSpace(tier) == "" || quota <= 0 || quota > 1000000000000 {
			return errors.New("档次名称必填，额度应为 1 到 1000000000000 的整数")
		}
	}
	return nil
}

// SaveConsolePlan keeps saved terms immutable, including during concurrent subscription creation.
func (s *Store) SaveConsolePlan(ctx context.Context, p *Plan) error {
	if err := ValidateConsolePlan(p); err != nil {
		return err
	}
	if p.ID == 0 {
		if p.WindowQuotaCent == 0 && p.WeekQuotaCent == 0 && p.CycleQuotaCent == 0 {
			return errors.New("新套餐请配置金额权益")
		}
		ids, _ := p.ModelScope()
		if len(ids) == 0 {
			return errors.New("新套餐请至少选择一个适用模型")
		}
		if len(ids) > 0 {
			models, err := s.ListSupportedModels(ctx, "")
			if err != nil {
				return err
			}
			available := make(map[string]bool, len(models))
			for _, m := range models {
				available[m.ModelID] = true
			}
			for _, id := range ids {
				if !available[id] {
					return errors.New("适用模型必须已上架且配置了可用上游：" + id)
				}
			}
		}
		return s.db.WithContext(ctx).Create(p).Error
	}
	old, err := s.GetPlan(ctx, p.ID)
	if err != nil {
		return err
	}
	if old == nil {
		return errors.New("套餐不存在")
	}
	oldScope, err := old.ModelScope()
	if err != nil {
		return err
	}
	newScope, _ := p.ModelScope()
	if !reflect.DeepEqual(oldScope, newScope) && !(len(oldScope) == 0 && len(newScope) == 0) {
		return errors.New("已保存套餐的适用模型保持固定，请复制为新套餐调整权益")
	}
	if old.WeekQuotaCent != p.WeekQuotaCent || old.WindowQuotaCent != p.WindowQuotaCent || old.CycleQuotaCent != p.CycleQuotaCent || old.Name != p.Name || old.PriceMoney != p.PriceMoney || old.ValidityDays != p.ValidityDays || old.RefreshHours != p.RefreshHours || !reflect.DeepEqual(old.TierQuotaMap(), p.TierQuotaMap()) {
		return errors.New("已保存套餐的计费条款保持固定，请复制为新套餐调整权益；原套餐可启用或停用")
	}
	return s.db.WithContext(ctx).Model(&Plan{}).Where("id = ?", p.ID).Update("status", p.Status).Error
}

func (s *Store) PayTenantRecharge(ctx context.Context, tenantID uint64, orderNo string) (*RechargeOrder, error) {
	order, err := s.rechargeByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil || tenantID == 0 || order.TenantID != tenantID {
		return nil, errors.New("充值申请不存在")
	}
	return s.PayRecharge(ctx, orderNo)
}
