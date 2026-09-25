// 商品目录（Model = 一个模型 SKU）。目录是计费的权威来源：billing 在 reserve/settle 时
// 按目录单价把 token 用量折算成人民币（分）扣款。无库存（数字商品），销售额 = 该模型
// 累计消耗金额。定价是 2026 快照，admin 可在面板改价/启停。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Model 商品目录中的一个模型 SKU。ModelID 即调用 /v1/chat/completions 时的 model 名，
// 全站唯一；价格单位为 分/百万 token。
type Model struct {
	ID              uint64    `gorm:"primaryKey" json:"id"`
	Vendor          string    `gorm:"size:32;not null;index:idx_vendor_status,priority:1" json:"vendor"` // 厂商：DeepSeek/通义千问/...
	ModelID         string    `gorm:"size:64;not null;uniqueIndex:uk_model_id" json:"model_id"`          // 调用名，计费键
	Name            string    `gorm:"size:128;not null" json:"name"`                                     // 展示名
	InputPriceCent  int64     `gorm:"not null;default:0" json:"input_price_cent"`                        // 输入价 分/百万 token
	OutputPriceCent int64     `gorm:"not null;default:0" json:"output_price_cent"`                       // 输出价 分/百万 token
	ContextLen      int       `gorm:"not null;default:0" json:"context_len"`                             // 上下文窗口（token）
	Tags            string    `gorm:"type:json" json:"tags"`                                             // JSON 数组：["文本","推理","旗舰"]
	Status          int8      `gorm:"not null;default:0" json:"status"`                                  // 0=上架 1=下架
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TableName 与 schema.sql 的 models 表一致。
func (Model) TableName() string { return "models" }

// TagsList 解析 Tags JSON 为字符串切片（读路径用；空串/坏 JSON 返回空）。
func (m *Model) TagsList() []string {
	if m.Tags == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(m.Tags), &out); err != nil {
		return nil
	}
	return out
}

// Enabled 是否上架（可被调用/可被买家看到）。
func (m *Model) Enabled() bool { return m != nil && m.Status == 0 }

// tierOrder 模型档次的贵贱优先级（下标越小越贵）。tierFromTags 取最贵档；
// 免费=0 价模型，唯一可能绕过额度检查的档。
var tierOrder = []string{"旗舰", "推理", "视觉", "长文本", "文本", "轻量", "免费"}

// TierFromTags 从模型 tags 解析最贵档次（GPT Plus 式额度按档次配额）。
// 例：["文本","轻量"]→文本；["旗舰"]→旗舰；["免费"]→免费；无标签/未知标签→""（计为免费，不限量）。
func TierFromTags(tags []string) string {
	best := len(tierOrder) // 初始为「无档」（比免费还靠后）
	for _, t := range tags {
		for i, known := range tierOrder {
			if t == known && i < best {
				best = i
			}
		}
	}
	if best >= len(tierOrder) {
		return ""
	}
	return tierOrder[best]
}

// TierNames 返回档次顺序（贵→便宜），供展示层按固定顺序排版额度。
func TierNames() []string {
	return append([]string(nil), tierOrder...)
}

// ListModels 目录列表。status=nil 返回全部；status 指定则按 0=上架/1=下架 过滤。按厂商、id 排序。
func (s *Store) ListModels(ctx context.Context, vendor string, status *int8) ([]Model, error) {
	q := s.db.WithContext(ctx).Model(&Model{})
	if vendor != "" {
		q = q.Where("vendor = ?", vendor)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var rows []Model
	err := q.Order("vendor ASC, id ASC").Find(&rows).Error
	return rows, err
}

// ListEnabledModels 买家可见目录（仅上架）。
func (s *Store) ListEnabledModels(ctx context.Context) ([]Model, error) {
	on := int8(0)
	return s.ListModels(ctx, "", &on)
}

// GetModelByID 按主键取模型（管理端点用；nil=不存在）。
func (s *Store) GetModelByID(ctx context.Context, id uint64) (*Model, error) {
	var m Model
	err := s.db.WithContext(ctx).First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetModelByModelID 按调用名取模型（计费用；nil=未上架/不存在）。
// 停用的模型也返回（让调用方能区分「不存在」与「已下架」，见 Reserve 错误码）。
func (s *Store) GetModelByModelID(ctx context.Context, modelID string) (*Model, error) {
	var m Model
	err := s.db.WithContext(ctx).Where("model_id = ?", modelID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateModel 新增 SKU。
func (s *Store) CreateModel(ctx context.Context, m *Model) error {
	return s.db.WithContext(ctx).Create(m).Error
}

// UpdateModelStatus 启停（0=上架 1=下架）。
func (s *Store) UpdateModelStatus(ctx context.Context, id uint64, status int8) error {
	return s.db.WithContext(ctx).Model(&Model{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": time.Now()}).Error
}

// UpdateModelPrice 改价（分/百万 token）。
func (s *Store) UpdateModelPrice(ctx context.Context, id uint64, inCent, outCent int64) error {
	return s.db.WithContext(ctx).Model(&Model{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"input_price_cent": inCent, "output_price_cent": outCent, "updated_at": time.Now(),
		}).Error
}

// SeedModels 幂等填充商品目录：已存在的 model_id 跳过（不覆盖 admin 改价），新增的插入。
func (s *Store) SeedModels(ctx context.Context) error {
	for _, m := range DefaultModels() {
		var existing int64
		if err := s.db.WithContext(ctx).Model(&Model{}).
			Where("model_id = ?", m.ModelID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			continue
		}
		now := time.Now()
		m.CreatedAt, m.UpdatedAt = now, now
		if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
			return err
		}
	}
	return nil
}
