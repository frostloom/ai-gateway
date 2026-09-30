// Package store billing 数据层：MySQL bills 账本（权威） + Redis balance（热投影）。
//
// 权威口径：MySQL bills 是账本，Redis balance 只是缓存投影。
// 崩溃恢复以账本为准（对账/重建 job，M4）。
package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/frostloom/ai-gateway/internal/pkg/quota"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"

	_ "embed"
)

//go:embed lua/reserve.lua
var reserveLua string

//go:embed lua/settle.lua
var settleLua string

// 账单状态机。
const (
	PhaseReserve = "reserve"
	PhaseSettle  = "settle"

	StatusPending  = "pending"
	StatusSettled  = "settled"
	StatusReversed = "reversed"
)

// 计费事件阶段（M10 outbox/audit 的 phase 枚举；usage 是网关流终态上报的持久 marker 阶段）。
const (
	PhaseUsage   = "usage"
	PhaseReverse = "reverse"
)

// 扣费来源（M9）：balance 余额按量扣费 | subscription 订阅额度（token 计额度、不碰钱）。
const (
	FundingBalance      = "balance"
	FundingSubscription = "subscription"
)

// ---------- 表模型（gorm AutoMigrate 与 scripts/schema.sql 保持一致） ----------

type Tenant struct {
	ID           uint64 `gorm:"primaryKey"`
	Name         string `gorm:"size:64;uniqueIndex:uk_name;not null"`
	InitialQuota int64  `gorm:"not null;default:0"` // 初始额度，对账重建 balance 的依据
	Status       int8   `gorm:"not null;default:0"` // 0=active 1=disabled
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APIKey struct {
	ID        uint64 `gorm:"primaryKey"`
	TenantID  uint64 `gorm:"not null;index:idx_tenant_status"`
	KeyHash   string `gorm:"size:64;uniqueIndex:uk_key_hash;not null"`
	KeyPrefix string `gorm:"size:16;not null"`
	Name      string `gorm:"size:64"`
	Status    int8   `gorm:"not null;default:0"`
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Provider struct {
	ID              uint64     `gorm:"primaryKey" json:"id"`
	Name            string     `gorm:"size:64;uniqueIndex:uk_name;not null" json:"name"`
	BaseURL         string     `gorm:"size:255;not null" json:"base_url"`
	UpstreamKey     string     `gorm:"size:255" json:"upstream_key"`            // 上游 API key（demo 明文存；生产应加密/KMS 引用，转发时才解密）
	CostInCent      int64      `gorm:"not null;default:0" json:"cost_in_cent"`  // 上游成本：输入 分/百万 token（毛利 = 售价 − 成本）
	CostOutCent     int64      `gorm:"not null;default:0" json:"cost_out_cent"` // 上游成本：输出 分/百万 token
	Models          string     `gorm:"type:json" json:"models"`                 // JSON 数组
	Weight          int        `gorm:"not null;default:1" json:"weight"`
	Status          int8       `gorm:"not null;default:0" json:"status"` // 0=active 1=disabled 2=banned
	FailCount       int        `gorm:"not null;default:0" json:"fail_count"`
	ConsecutiveFail int        `gorm:"not null;default:0" json:"consecutive_fail"`
	CooldownUntil   *time.Time `json:"cooldown_until,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Bill struct {
	ID               uint64  `gorm:"primaryKey"`
	RequestID        string  `gorm:"size:64;not null;uniqueIndex:uk_request_phase,priority:1"`
	Phase            string  `gorm:"size:16;not null;uniqueIndex:uk_request_phase,priority:2"`
	Status           string  `gorm:"size:16;not null;index:idx_recon,priority:1"`
	TenantID         uint64  `gorm:"not null;index:idx_tenant_status_created,priority:1"`
	APIKeyID         uint64  `gorm:"not null"`
	ProviderID       *uint64 `gorm:"index:idx_provider_created,priority:1"`
	Model            string  `gorm:"size:64"`                          // 调用名 == 目录 model_id
	Funding          string  `gorm:"size:16;not null;default:balance"` // 扣费来源：balance|subscription（M9）
	PreQuota         int64   `gorm:"not null"`                         // 预扣金额（分）
	ActualQuota      *int64  // settle 实际金额（分）
	DeltaQuota       *int64  // actual - pre（settle 行）
	PromptTokens     *int64
	CompletionTokens *int64
	// 价格快照（分/百万 token）：reserve 时写入当时目录价，settle 用快照算实际，
	// 使 delta 自洽、历史账单不受 admin 改价影响。
	InPriceCent     *int64
	OutPriceCent    *int64
	UsageReportedAt *time.Time `gorm:"index:idx_recon,priority:2"` // 网关流终态上报的持久 marker
	ErrorCode       string     `gorm:"size:64"`
	CreatedAt       time.Time  `gorm:"index:idx_recon,priority:3;index:idx_tenant_status_created,priority:2;index:idx_provider_created,priority:2"`
}

// ---------- Redis key 前缀规范 ----------

func BalanceKey(tenantID uint64) string     { return fmt.Sprintf("billing:balance:%d", tenantID) }
func ReqGuardKey(requestID string) string   { return fmt.Sprintf("billing:req:%s", requestID) }
func DeltaGuardKey(requestID string) string { return fmt.Sprintf("billing:delta:%s", requestID) }
func UsageKey(requestID string) string      { return fmt.Sprintf("usage:%s", requestID) }

// ---------- Store ----------

type Store struct {
	db  *gorm.DB
	rdb *goredis.Client
}

func NewStore(db *gorm.DB, rdb *goredis.Client) *Store {
	return &Store{db: db, rdb: rdb}
}

// AutoMigrate 建表/补列（开发便利；生产以 scripts/schema.sql 为准）。
func (s *Store) AutoMigrate() error {
	return s.db.AutoMigrate(&Tenant{}, &APIKey{}, &Provider{}, &Model{}, &Bill{},
		&Plan{}, &Subscription{}, &RechargeOrder{}, &Topup{}, &AgentSession{}, &AgentAuditLog{},
		&OutboxEvent{}, &AuditEvent{}, &AdminUser{}, &AdminSession{}, &PortalUser{}, &PortalSession{})
}

// ---------- 预占（核心） ----------

// Reserve 预占额度。幂等：同一 request_id 只扣一次、只落一行。
//
// 顺序设计（为什么这样安全）：
//  1. DB 快速查重：已存在该 request_id 的 reserve 行 → 幂等重放，直接返回；
//  2. Redis Lua 原子扣减：guard 幂等——若本 request 已扣过则返回 ALREADY 不重复扣；
//  3. 落 reserve 行：唯一索引 uk_request_phase 兜底并发；插入失败且非重复 → 返回错误，
//     调用方重试时 guard 让 Lua 变 no-op，insert 会成功 → 自愈。
//
// 残留风险：第 2 步成功但第 3 步永久失败且调用方永不重试 → Redis 余额少记无行。
// 该孤儿由 M4 的「从账本重建 balance」job 收敛（以 DB 为权威，重算投影）。
func (s *Store) Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	// 目录是计费权威：未上架/不存在的模型直接拒绝（gateway 映射为 400「模型未上架/已下架」）。
	model, err := s.GetModelByModelID(ctx, req.Model)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "model lookup: %v", err)
	}
	if model == nil || model.Status != 0 {
		return nil, status.Errorf(codes.InvalidArgument, "model unavailable or not listed: %s", req.Model)
	}
	// 预扣 = 估算用量按目录价 ×1.2 安全系数；价格快照落 bill，settle 用快照多退少补。
	estCompletion := req.MaxCompletionTokens
	if estCompletion < 0 {
		estCompletion = 0
	}
	pre := quota.ReserveCostCent(req.EstPromptTokens, estCompletion, model.InputPriceCent, model.OutputPriceCent)
	if pre < 0 {
		pre = 0
	}
	inC, outC := model.InputPriceCent, model.OutputPriceCent

	tenantID := uint64(req.TenantId)

	// M9 订阅额度路径判定：有活跃订阅 + 该模型档次在套餐额度内 → 走订阅额度（token 计额度、不碰钱）。
	// 免费档/无标签模型、无订阅 → 余额路径（原逻辑不变）。
	isSub, refreshHours, tier, quota, err := s.subscriptionAllowancePath(ctx, model, tenantID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "subscription path: %v", err)
	}

	// 1) DB 快速查重（幂等重放，两种路径共用）
	if row, err := s.getReserve(ctx, req.RequestId); err == nil && row != nil {
		bal, _ := s.getBalance(ctx, tenantID)
		return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(row.ID), BalanceAfter: bal}, nil
	}

	if isSub {
		// 窗口额度检查：consumed[本档]（滚动窗口内已用 token）+ 本次估算 ≤ 窗口额度，否则超限拒绝。
		consumed, err := s.subscriptionConsumed(ctx, tenantID, refreshHours)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "subscription consumed: %v", err)
		}
		est := req.EstPromptTokens + estCompletion
		if est < 0 {
			est = 0
		}
		if consumed[tier]+est > quota {
			return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_SUB_QUOTA, BalanceAfter: 0}, nil
		}
		// 落订阅额度 reserve 行：pre_quota=0、funding=subscription，不走 Lua/余额（额度按窗口涌现，非预扣）。
		row := &Bill{
			RequestID:    req.RequestId,
			Phase:        PhaseReserve,
			Status:       StatusPending,
			TenantID:     tenantID,
			APIKeyID:     uint64(req.ApiKeyId),
			Model:        req.Model,
			Funding:      FundingSubscription,
			PreQuota:     0,
			InPriceCent:  &inC,
			OutPriceCent: &outC,
			CreatedAt:    time.Now(),
		}
		if req.ProviderId > 0 {
			pid := uint64(req.ProviderId)
			row.ProviderID = &pid
		}
		// 落订阅额度 reserve 行 + 同事务 outbox 事件（M10：金额 0、funding=subscription）。
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
			return insertOutbox(tx, newBillingEvent(row, 0, PhaseReserve))
		}); err != nil {
			if isDupKey(err) {
				if r2, e2 := s.getReserve(ctx, req.RequestId); e2 == nil && r2 != nil {
					bal, _ := s.getBalance(ctx, tenantID)
					return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(r2.ID), BalanceAfter: bal}, nil
				}
			}
			return nil, status.Errorf(codes.Unavailable, "reserve insert: %v", err)
		}
		bal, _ := s.getBalance(ctx, tenantID)
		return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(row.ID), BalanceAfter: bal}, nil
	}

	// 2) Redis Lua 原子扣减（余额路径）
	script := goredis.NewScript(reserveLua)
	res, err := script.Run(ctx, s.rdb, []string{BalanceKey(tenantID), ReqGuardKey(req.RequestId)}, pre, req.RequestId).Result()
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "reserve lua: %v", err)
	}
	code, bal := parseReserveResult(res)
	if code == -1 {
		return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_INSUFFICIENT, BalanceAfter: bal}, nil
	}

	// 3) 落 reserve 行
	row := &Bill{
		RequestID:    req.RequestId,
		Phase:        PhaseReserve,
		Status:       StatusPending,
		TenantID:     tenantID,
		APIKeyID:     uint64(req.ApiKeyId),
		Model:        req.Model,
		Funding:      FundingBalance,
		PreQuota:     pre,
		InPriceCent:  &inC,
		OutPriceCent: &outC,
		CreatedAt:    time.Now(),
	}
	if req.ProviderId > 0 {
		pid := uint64(req.ProviderId)
		row.ProviderID = &pid
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		return insertOutbox(tx, newBillingEvent(row, pre, PhaseReserve))
	}); err != nil {
		if isDupKey(err) {
			if r2, e2 := s.getReserve(ctx, req.RequestId); e2 == nil && r2 != nil {
				return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(r2.ID), BalanceAfter: bal}, nil
			}
		}
		return nil, status.Errorf(codes.Unavailable, "reserve insert: %v", err)
	}
	return &billingv1.ReserveResponse{Code: billingv1.ReserveCode_RESERVE_OK, BillId: int64(row.ID), BalanceAfter: bal}, nil
}

// subscriptionAllowancePath 判定请求是否走订阅额度（M9）。返回 isSub=true 时 refreshHours/tier/quota
// 为当前套餐的窗口参数。额度判定：有 active 且 recurring 订阅、套餐有效、该档在套餐额度内。
// 免费档/无标签模型（不限量）、无订阅、订阅停用 → isSub=false（余额路径）。
func (s *Store) subscriptionAllowancePath(ctx context.Context, model *Model, tenantID uint64) (isSub bool, refreshHours int, tier string, quota int64, err error) {
	tier = TierFromTags(model.TagsList())
	if tier == "" || tier == "免费" {
		return false, 0, "", 0, nil // 免费/未知档不限量
	}
	sub, err := s.GetActiveSubscription(ctx, tenantID)
	if err != nil || sub == nil {
		return false, 0, "", 0, err
	}
	if sub.PlanType != PlanTypeRecurring {
		return false, 0, tier, 0, nil
	}
	p, err := s.GetPlan(ctx, sub.PlanID)
	if err != nil || p == nil || p.Status != 0 {
		return false, 0, tier, 0, err
	}
	return true, p.RefreshHours, tier, p.TierQuotaFor(tier), nil
}

// GetBalance 查租户当前余额（Redis 投影；权威口径是 MySQL 账本）。
func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {
	return s.getBalance(ctx, tenantID)
}

// EnsureBalanceKeys 启动时兜底：为每个活跃租户补建 balance key（缺失时用初始额度初始化）。
// 后续 M4 的 rebuild job 才是「从账本重算投影」的完整版。
func (s *Store) EnsureBalanceKeys(ctx context.Context) error {
	var tenants []Tenant
	if err := s.db.WithContext(ctx).Find(&tenants).Error; err != nil {
		return err
	}
	for _, t := range tenants {
		if t.Status != 0 {
			continue
		}
		n, err := s.rdb.Exists(ctx, BalanceKey(t.ID)).Result()
		if err != nil {
			return err
		}
		if n == 0 {
			if err := s.rdb.Set(ctx, BalanceKey(t.ID), t.InitialQuota, 0).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------- 用量上报 / 结算 / 退款 / 密钥校验 ----------

// ReportUsage 上报实际用量：把 usage_reported_at（持久 marker）+ token 数写进 reserve 行。
// 幂等：marker 用 COALESCE 只写一次；token 数重复写同值无副作用。
// 这个 marker 是对账的「分水岭」：无 marker = 流没跑完（应全退）；有 marker = 跑完了（只差 settle）。
// M10：UPDATE 与 outbox usage 事件同一事务提交。
func (s *Store) ReportUsage(ctx context.Context, req *billingv1.ReportUsageRequest) error {
	row, err := s.getReserve(ctx, req.RequestId)
	if err != nil {
		return status.Errorf(codes.Unavailable, "report usage lookup: %v", err)
	}
	if row == nil {
		return status.Errorf(codes.NotFound, "reserve row not found: %s", req.RequestId)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Bill{}).
			Where("request_id = ? AND phase = ?", req.RequestId, PhaseReserve).
			Updates(map[string]any{
				"prompt_tokens":     req.PromptTokens,
				"completion_tokens": req.CompletionTokens,
				"usage_reported_at": gorm.Expr("COALESCE(usage_reported_at, ?)", time.Now()),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errReportUsageNotFound
		}
		// 同事务 outbox usage 事件（M10）：金额 0，token 用上报值。
		ev := newBillingEvent(row, 0, PhaseUsage)
		ev.PromptTokens = &req.PromptTokens
		ev.CompletionTokens = &req.CompletionTokens
		return insertOutbox(tx, ev)
	})
	if err != nil {
		if errors.Is(err, errReportUsageNotFound) {
			return status.Errorf(codes.NotFound, "reserve row not found: %s", req.RequestId)
		}
		return status.Errorf(codes.Unavailable, "report usage: %v", err)
	}
	return nil
}

// errReportUsageNotFound 区分「reserve 行不存在」与「落库失败」的内部哨兵。
var errReportUsageNotFound = errors.New("reserve row not found")

// Settle 结算：pending → settled。按实际用量与预扣算 delta，Redis 恰好冲正一次，落 settle 行。
// 幂等：settle 行撞唯一索引 no-op；delta guard 保证 Redis 只应用一次。
func (s *Store) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	row, err := s.getReserve(ctx, req.RequestId)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, status.Errorf(codes.NotFound, "reserve row not found: %s", req.RequestId)
	}
	if row.Status == StatusReversed {
		// 已冲正：结算 no-op（账已退，不能再结）
		return &billingv1.SettleResponse{Settled: false}, nil
	}
	if row.UsageReportedAt == nil || row.PromptTokens == nil || row.CompletionTokens == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "usage not reported: %s", req.RequestId)
	}

	// M9 订阅额度：token 已在 reserve 时消耗（额度按窗口涌现），settle 不产生任何钱。
	// actual=0/delta=0 → applyDelta(0) 是 no-op（settle.lua 既不改余额也不退回）。
	if row.Funding == FundingSubscription {
		actual, delta := int64(0), int64(0)
		bal, err := s.applyDelta(ctx, row.TenantID, req.RequestId, 0)
		if err != nil {
			return nil, err
		}
		settleRow := &Bill{
			RequestID:        req.RequestId,
			Phase:            PhaseSettle,
			Status:           StatusSettled,
			TenantID:         row.TenantID,
			APIKeyID:         row.APIKeyID,
			ProviderID:       row.ProviderID,
			Model:            row.Model,
			Funding:          FundingSubscription,
			PreQuota:         0,
			ActualQuota:      &actual,
			DeltaQuota:       &delta,
			PromptTokens:     row.PromptTokens,
			CompletionTokens: row.CompletionTokens,
			InPriceCent:      row.InPriceCent,
			OutPriceCent:     row.OutPriceCent,
			CreatedAt:        time.Now(),
		}
		// 落 settle 行 + 推进 reserve 状态 + 同事务 outbox 事件（M10：金额 0、funding=subscription）。
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(settleRow).Error; err != nil && !isDupKey(err) {
				return err
			}
			if err := tx.Model(&Bill{}).
				Where("request_id = ? AND phase = ? AND status = ?", req.RequestId, PhaseReserve, StatusPending).
				Update("status", StatusSettled).Error; err != nil {
				return err
			}
			return insertOutbox(tx, newBillingEvent(settleRow, 0, PhaseSettle))
		}); err != nil {
			return nil, status.Errorf(codes.Unavailable, "settle insert: %v", err)
		}
		return &billingv1.SettleResponse{Settled: true, DeltaQuota: 0, BalanceAfter: bal}, nil
	}

	// 用 reserve 时快照的目录价算实际（改价不影响历史账单）；快照缺失（legacy 行）回查目录。
	inC, outC, err := s.priceSnapshot(ctx, row)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "settle price: %v", err)
	}
	actual := quota.CostCent(*row.PromptTokens, *row.CompletionTokens, inC, outC)
	delta := actual - row.PreQuota

	bal, err := s.applyDelta(ctx, row.TenantID, req.RequestId, delta)
	if err != nil {
		return nil, err
	}

	settleRow := &Bill{
		RequestID:        req.RequestId,
		Phase:            PhaseSettle,
		Status:           StatusSettled,
		TenantID:         row.TenantID,
		APIKeyID:         row.APIKeyID,
		ProviderID:       row.ProviderID,
		Model:            row.Model,
		Funding:          row.Funding,
		PreQuota:         row.PreQuota,
		ActualQuota:      &actual,
		DeltaQuota:       &delta,
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		InPriceCent:      &inC,
		OutPriceCent:     &outC,
		CreatedAt:        time.Now(),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(settleRow).Error; err != nil && !isDupKey(err) {
			return err
		}
		// 状态推进：只有 pending 才允许 → settled（并发/重试时 second 次 RowsAffected=0 也无妨）
		if err := tx.Model(&Bill{}).
			Where("request_id = ? AND phase = ? AND status = ?", req.RequestId, PhaseReserve, StatusPending).
			Update("status", StatusSettled).Error; err != nil {
			return err
		}
		return insertOutbox(tx, newBillingEvent(settleRow, delta, PhaseSettle))
	}); err != nil {
		return nil, status.Errorf(codes.Unavailable, "settle insert: %v", err)
	}

	return &billingv1.SettleResponse{Settled: true, DeltaQuota: delta, BalanceAfter: bal}, nil
}

// Reverse 冲正/退款：pending → reversed，退回全部预扣。
// 幂等：delta guard 保证只退一次；状态守卫保证不重复推进。
func (s *Store) Reverse(ctx context.Context, req *billingv1.ReverseRequest) (*billingv1.ReverseResponse, error) {
	row, err := s.getReserve(ctx, req.RequestId)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, status.Errorf(codes.NotFound, "reserve row not found: %s", req.RequestId)
	}
	if row.Status == StatusSettled {
		// 已结算，钱按实际结清了，不再退
		return &billingv1.ReverseResponse{Reversed: false, RefundQuota: 0}, nil
	}
	if row.Status == StatusReversed {
		// 幂等重放
		return &billingv1.ReverseResponse{Reversed: true, RefundQuota: row.PreQuota}, nil
	}

	// 退回全部预扣：delta 语义「新余额 = 旧余额 - delta」，退款用负 delta（-pre）。
	if _, err := s.applyDelta(ctx, row.TenantID, req.RequestId, -row.PreQuota); err != nil {
		return nil, err
	}
	// 状态推进 + 同事务 outbox reverse 事件（M10：amount = -pre 退款）。
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Bill{}).
			Where("request_id = ? AND phase = ? AND status = ?", req.RequestId, PhaseReserve, StatusPending).
			Update("status", StatusReversed).Error; err != nil {
			return err
		}
		return insertOutbox(tx, newBillingEvent(row, -row.PreQuota, PhaseReverse))
	}); err != nil {
		return nil, status.Errorf(codes.Unavailable, "reverse update: %v", err)
	}
	return &billingv1.ReverseResponse{Reversed: true, RefundQuota: row.PreQuota}, nil
}

// RebuildBalance 从账本重建 Redis 余额投影（M4 对账兜底）。
//
// 权威公式：balance = initial_quota
//   - Σ(pending reserve 行的 pre_quota)   // 在途预扣（还没 settle/reverse）
//   - Σ(settled settle 行的 actual_quota)  // 已结算的净消耗
//
// 为什么这样算：
//   - 已结算请求：reserve 预扣了 pre，settle 又冲正了 delta(=actual-pre)，净消耗 = actual；
//   - 已冲正请求：预扣退回，净消耗 0，不进入任何求和；
//   - 在途请求（预扣未结算）：钱已从 Redis 扣走，投影必须也减掉，否则补结算时会多退。
//
// 用途：收敛「Lua 已扣但落库失败」这类 Redis 与账本不一致的脏投影。
func (s *Store) RebuildBalance(ctx context.Context, tenantID uint64) (int64, error) {
	var tenant Tenant
	if err := s.db.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return 0, status.Errorf(codes.Unavailable, "tenant not found: %v", err)
	}
	if tenant.Status != 0 {
		return 0, status.Errorf(codes.FailedPrecondition, "tenant disabled: %d", tenantID)
	}

	// M7 起口径：initial + Σtopups − Σpending pre − Σsettled actual（单一权威公式，见 LedgerBalance）。
	bal, err := LedgerBalance(ctx, s.db, tenantID)
	if err != nil {
		return 0, status.Errorf(codes.Unavailable, "ledger balance: %v", err)
	}
	if err := s.rdb.Set(ctx, BalanceKey(tenantID), bal, 0).Err(); err != nil {
		return 0, status.Errorf(codes.Unavailable, "rebuild set balance: %v", err)
	}
	return bal, nil
}

// ValidateAPIKey 按 SHA-256 哈希查 key，校验状态与过期时间。
func (s *Store) ValidateAPIKey(ctx context.Context, keyHash string) (*APIKey, error) {
	var k APIKey
	err := s.db.WithContext(ctx).Where("key_hash = ?", keyHash).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if k.Status != 0 || (k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)) {
		return nil, nil
	}
	if k.Name == browserKeyName {
		var n int64
		err := s.db.WithContext(ctx).Model(&PortalSession{}).
			Joins("JOIN portal_users ON portal_users.id = portal_sessions.user_id").
			Joins("JOIN tenants ON tenants.id = portal_users.tenant_id").
			Where("portal_sessions.api_key_id = ? AND portal_users.status = 0 AND tenants.status = 0 AND tenants.id = ?", k.ID, k.TenantID).Count(&n).Error
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, nil
		}
	}
	return &k, nil
}

// ---------- 观测（Web 面板数据源，只读） ----------

// ListBills 分页查账单（默认 phase='reserve'，一请求一行）。面板用。
// phase="settle" 时查结算行（agent 展示精确扣费 ActualQuota 用）。
func (s *Store) ListBills(ctx context.Context, tenantID uint64, phase, status string, limit, offset int) ([]Bill, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if phase == "" {
		phase = PhaseReserve
	}
	q := s.db.WithContext(ctx).Model(&Bill{}).Where("phase = ?", phase)
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Bill
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// TenantStat 面板租户余额/消耗口径（账本权威 vs Redis 投影）。
type TenantStat struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	InitialQuota int64  `json:"initial_quota"`
	Balance      int64  `json:"balance"`       // 账本口径：initial + Σtopups - Σpending - Σsettled
	RedisBalance int64  `json:"redis_balance"` // Redis 投影（live）
	Consistent   bool   `json:"consistent"`    // 两者是否一致（对账收敛观测点）
	Topups       int64  `json:"topups"`        // Σ入账流水（充值/订阅/退款）
	Settled      int64  `json:"settled"`
	Pending      int64  `json:"pending"`
	BillCount    int64  `json:"bill_count"`
	TodayBills   int64  `json:"today_bills"`
	TodaySettled int64  `json:"today_settled"`
}

// Overview 面板总览：逐租户账本/投影 + 全局汇总。
type Overview struct {
	Tenants []TenantStat `json:"tenants"`
	Totals  struct {
		Settled      int64 `json:"settled"`
		Pending      int64 `json:"pending"`
		Bills        int64 `json:"bills"`
		TodayBills   int64 `json:"today_bills"`
		TodaySettled int64 `json:"today_settled"`
	} `json:"totals"`
}

// OverviewStats 总览统计。余额口径与 RebuildBalance 一致（含 M7 的 Σtopups 正项）。
func (s *Store) OverviewStats(ctx context.Context) (*Overview, error) {
	var tenants []Tenant
	if err := s.db.WithContext(ctx).Find(&tenants).Error; err != nil {
		return nil, err
	}
	o := &Overview{Tenants: make([]TenantStat, 0, len(tenants))}
	dayAgo := time.Now().Add(-24 * time.Hour)
	for _, t := range tenants {
		var topups, settled, pending, billCount, todayBills, todaySettled int64
		if err := s.db.WithContext(ctx).Model(&Topup{}).
			Where("tenant_id = ?", t.ID).
			Select("COALESCE(SUM(amount),0)").Scan(&topups).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&Bill{}).
			Where("tenant_id = ? AND phase = ? AND status = ?", t.ID, PhaseSettle, StatusSettled).
			Select("COALESCE(SUM(actual_quota),0)").Scan(&settled).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&Bill{}).
			Where("tenant_id = ? AND phase = ? AND status = ?", t.ID, PhaseReserve, StatusPending).
			Select("COALESCE(SUM(pre_quota),0)").Scan(&pending).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&Bill{}).
			Where("tenant_id = ? AND phase = ?", t.ID, PhaseReserve).
			Select("COUNT(*)").Scan(&billCount).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&Bill{}).
			Where("tenant_id = ? AND phase = ? AND created_at >= ?", t.ID, PhaseReserve, dayAgo).
			Select("COUNT(*)").Scan(&todayBills).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&Bill{}).
			Where("tenant_id = ? AND phase = ? AND status = ? AND created_at >= ?", t.ID, PhaseSettle, StatusSettled, dayAgo).
			Select("COALESCE(SUM(actual_quota),0)").Scan(&todaySettled).Error; err != nil {
			return nil, err
		}

		balance := t.InitialQuota + topups - settled - pending
		redisBal, _ := s.getBalance(ctx, t.ID)
		o.Tenants = append(o.Tenants, TenantStat{
			ID: t.ID, Name: t.Name, InitialQuota: t.InitialQuota,
			Balance: balance, RedisBalance: redisBal, Consistent: balance == redisBal,
			Topups: topups, Settled: settled, Pending: pending, BillCount: billCount,
			TodayBills: todayBills, TodaySettled: todaySettled,
		})
		o.Totals.Settled += settled
		o.Totals.Pending += pending
		o.Totals.Bills += billCount
		o.Totals.TodayBills += todayBills
		o.Totals.TodaySettled += todaySettled
	}
	return o, nil
}

// applyDelta 在 Redis 上恰好应用一次 delta（Settle 与 Reverse 共用，互斥）。
func (s *Store) applyDelta(ctx context.Context, tenantID uint64, requestID string, delta int64) (int64, error) {
	script := goredis.NewScript(settleLua)
	res, err := script.Run(ctx, s.rdb, []string{BalanceKey(tenantID), DeltaGuardKey(requestID)}, delta).Result()
	if err != nil {
		return 0, status.Errorf(codes.Unavailable, "delta lua: %v", err)
	}
	code, bal := parseReserveResult(res)
	if code == -1 {
		return 0, status.Errorf(codes.FailedPrecondition, "balance would go negative: tenant=%d delta=%d", tenantID, delta)
	}
	return bal, nil
}

// ---------- 内部 helpers ----------

// priceSnapshot 返回结算用的目录价：优先 reserve 时快照；缺失（旧数据）回查目录。
func (s *Store) priceSnapshot(ctx context.Context, row *Bill) (int64, int64, error) {
	if row.InPriceCent != nil && row.OutPriceCent != nil {
		return *row.InPriceCent, *row.OutPriceCent, nil
	}
	m, err := s.GetModelByModelID(ctx, row.Model)
	if err != nil {
		return 0, 0, err
	}
	if m == nil {
		return 0, 0, fmt.Errorf("model %q no longer in catalog", row.Model)
	}
	return m.InputPriceCent, m.OutputPriceCent, nil
}

func (s *Store) getReserve(ctx context.Context, requestID string) (*Bill, error) {
	var row Bill
	err := s.db.WithContext(ctx).
		Where("request_id = ? AND phase = ?", requestID, PhaseReserve).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) getBalance(ctx context.Context, tenantID uint64) (int64, error) {
	v, err := s.rdb.Get(ctx, BalanceKey(tenantID)).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	return v, err
}

// parseReserveResult 解析 Lua 返回 {code, balance}。
func parseReserveResult(res any) (int64, int64) {
	arr, ok := res.([]any)
	if !ok || len(arr) < 2 {
		return 0, 0
	}
	return toInt64(arr[0]), toInt64(arr[1])
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

// isDupKey 判断是否 MySQL 唯一键冲突（1062）。
func isDupKey(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
