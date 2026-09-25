// M7 自服务领域：套餐 / 订阅 / 充值 / 入账流水 / AI 会话与审计。
//
// 话费式计费模型（与用户确认的口径）：
//   - 一次性套餐（当前已订购）：买断，不可退、不可退订、不可改。
//   - 定期自动订阅：每周期扣费并授予额度；退订 = 当期不退、只取消下期续费；
//     改套餐 = 下期生效（运营商口径）。
//   - 充值（话费式）：可退，退款量 = min(订单额度, 当前余额)，绝不退成负。
//
// 入账流水 topups 是正余额的权威账本（Redis balance 仍是投影）。
// 幂等锚点：topups(ref_type, ref_id) 唯一索引 —— 重试/并发下只入账一次，
// 与 bills.uk_request_phase 同理。账本行是提交点，Redis 仅在插入成功后更新。
package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/frostloom/ai-gateway/internal/pkg/quota"
)

// ---------- 状态常量 ----------

const (
	PlanTypeOneTime   = "one_time"  // 一次性买断
	PlanTypeRecurring = "recurring" // 定期自动订阅

	SubActive    = "active"    // 生效中
	SubCancelled = "cancelled" // 已退订（本期用到 cycle_end，不续费）
	SubExpired   = "expired"   // 到期未续

	RechargePending   = "pending_payment"
	RechargePaid      = "paid"
	RechargeRefunded  = "refunded"
	RechargeCancelled = "cancelled"

	TopupRecharge     = "recharge"
	TopupSubscription = "subscription"
	TopupRefund       = "refund"
)

// ---------- 表模型（AutoMigrate 与 scripts/schema.sql 保持一致） ----------

// Plan 套餐目录（M9 起 GPT Plus 式）：月费 + 每模型档次每窗口的 token 用量额度（滚动刷新）。
// 订阅买的是「额度资格」而非钱：额度内使用不走余额；超限等待窗口刷新。one_time 不再售出（代码保留）。
type Plan struct {
	ID           uint64 `gorm:"primaryKey"`
	Name         string `gorm:"size:64;not null;uniqueIndex:uk_name"`
	PlanType     string `gorm:"size:16;not null"`   // recurring（目录只种定期订阅）
	PriceMoney   int64  `gorm:"not null"`           // 月费（元，模拟价）
	ValidityDays int    `gorm:"not null"`           // 计费周期天数（续费/改套餐周期）
	RefreshHours int    `gorm:"not null;default:5"` // 额度滚动窗口小时数（每 N 小时刷新，未用不累积）
	TierQuota    string `gorm:"type:json;not null"` // 每窗口每档次 token 额度 {"轻量":2000000,...}
	Status       int8   `gorm:"not null;default:0"` // 0=active 1=disabled
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TierQuotaMap 解析 tier_quota JSON 为 map[tier]token；坏 JSON/空返回空 map。
func (p *Plan) TierQuotaMap() map[string]int64 {
	if p == nil || p.TierQuota == "" {
		return nil
	}
	out := map[string]int64{}
	if err := json.Unmarshal([]byte(p.TierQuota), &out); err != nil {
		return nil
	}
	return out
}

// TierQuotaFor 某档次的每窗口 token 额度；未配置返回 0（该档次不可用）。
func (p *Plan) TierQuotaFor(tier string) int64 {
	return p.TierQuotaMap()[tier]
}

// Subscription 订阅记录。one_time 每笔购买一行（允许多条）；recurring 每租户至多一条 active。
type Subscription struct {
	ID            uint64     `gorm:"primaryKey"`
	TenantID      uint64     `gorm:"not null;index:idx_sub_tenant_status,priority:1"`
	PlanID        uint64     `gorm:"not null"`
	PlanType      string     `gorm:"size:16;not null"`
	Status        string     `gorm:"size:16;not null;index:idx_sub_tenant_status,priority:2"` // active/cancelled/expired
	CycleStart    *time.Time // recurring 本期起止；one_time 为 null
	CycleEnd      *time.Time
	AutoRenew     bool    `gorm:"not null;default:false"`
	CycleNum      int     `gorm:"not null;default:1"` // 已续期数（topup ref 用）
	QuotaGranted  int64   `gorm:"not null"`           // 累计授予额度
	PendingPlanID *uint64 // 改套餐：下期生效
	CancelledAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RechargeOrder 充值订单。order_no 唯一 = 幂等锚点（类比 request_id）。
type RechargeOrder struct {
	ID          uint64 `gorm:"primaryKey"`
	TenantID    uint64 `gorm:"not null;index:idx_ro_tenant_status,priority:1"`
	OrderNo     string `gorm:"size:64;not null;uniqueIndex:uk_order_no"`
	AmountMoney int64  `gorm:"not null"`                                               // wire 层金额（元，整数收）
	AmountCent  int64  `gorm:"not null"`                                               // 入账金额（分）= AmountMoney × 100
	Status      string `gorm:"size:16;not null;index:idx_ro_tenant_status,priority:2"` // pending_payment/paid/refunded/cancelled
	PaidAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Topup 入账流水（正余额权威账本）。(ref_type, ref_id) 唯一 = 只入账一次。
type Topup struct {
	ID        uint64    `gorm:"primaryKey"`
	TenantID  uint64    `gorm:"not null;index:idx_tp_tenant_created,priority:1"`
	Source    string    `gorm:"size:16;not null"` // recharge/subscription/refund
	Amount    int64     `gorm:"not null"`         // 正=入账，负=退款
	RefType   string    `gorm:"size:32;not null;uniqueIndex:uk_topup_ref,priority:1"`
	RefID     string    `gorm:"size:64;not null;uniqueIndex:uk_topup_ref,priority:2"`
	CreatedAt time.Time `gorm:"index:idx_tp_tenant_created,priority:2"`
}

// AgentSession AI 客服会话（持久记录；热态 history 在 Redis）。
type AgentSession struct {
	ID           uint64 `gorm:"primaryKey"`
	SessionID    string `gorm:"size:64;not null;uniqueIndex:uk_session"`
	TenantID     uint64 `gorm:"not null"`
	APIKeyID     uint64 `gorm:"not null"`
	CreatedAt    time.Time
	LastActiveAt time.Time
}

// AgentAuditLog AI 客服每笔工具执行的审计轨迹。
// TableName 固定为 agent_audit_log：gorm 默认会复数成 agent_audit_logs，
// 与 scripts/schema.sql 的 DDL 不一致会导致生产建表分叉。
type AgentAuditLog struct {
	ID        uint64    `gorm:"primaryKey"`
	SessionID string    `gorm:"size:64;not null;index:idx_audit_session"`
	TenantID  uint64    `gorm:"not null;index:idx_audit_tenant_created,priority:1"`
	Tool      string    `gorm:"size:32;not null"`
	Args      string    `gorm:"type:text"` // 参数 JSON
	Confirmed bool      `gorm:"not null"`  // 写操作是否经过用户确认
	Preview   string    `gorm:"size:255"`  // 确认时展示的预览文案
	Result    string    `gorm:"type:text"` // 执行结果 JSON/错误
	Guard     string    `gorm:"type:text"` // P3 Jev 安全决策 JSON（拦截原因等，可空）
	CreatedAt time.Time `gorm:"index:idx_audit_tenant_created,priority:2"`
}

func (AgentAuditLog) TableName() string { return "agent_audit_log" }

// ---------- 查询 ----------

// LedgerBalance 账本口径余额（唯一权威公式）：
//
//	balance = initial_quota + Σ(topups.amount) − Σ(pending reserve.pre) − Σ(settled settle.actual)
//
// M7 起 topups 是正余额来源；Redis 只是该值的投影。seed 与 loadtest 的断言沿用此公式。
func LedgerBalance(ctx context.Context, db *gorm.DB, tenantID uint64) (int64, error) {
	var topups, pendingPre, settledActual, initial int64
	if err := db.WithContext(ctx).Model(&Topup{}).
		Where("tenant_id = ?", tenantID).
		Select("COALESCE(SUM(amount),0)").Scan(&topups).Error; err != nil {
		return 0, err
	}
	if err := db.WithContext(ctx).Model(&Bill{}).
		Where("tenant_id = ? AND phase = ? AND status = ?", tenantID, PhaseReserve, StatusPending).
		Select("COALESCE(SUM(pre_quota),0)").Scan(&pendingPre).Error; err != nil {
		return 0, err
	}
	if err := db.WithContext(ctx).Model(&Bill{}).
		Where("tenant_id = ? AND phase = ? AND status = ?", tenantID, PhaseSettle, StatusSettled).
		Select("COALESCE(SUM(actual_quota),0)").Scan(&settledActual).Error; err != nil {
		return 0, err
	}
	if err := db.WithContext(ctx).Model(&Tenant{}).
		Where("id = ?", tenantID).
		Select("initial_quota").Scan(&initial).Error; err != nil {
		return 0, err
	}
	return initial + topups - pendingPre - settledActual, nil
}

// ListPlans 套餐目录（全部，含下架，供 admin 管理）。
func (s *Store) ListPlans(ctx context.Context) ([]Plan, error) {
	var rows []Plan
	err := s.db.WithContext(ctx).Order("id ASC").Find(&rows).Error
	return rows, err
}

// GetPlan 按 id 取套餐。
func (s *Store) GetPlan(ctx context.Context, id uint64) (*Plan, error) {
	var p Plan
	err := s.db.WithContext(ctx).First(&p, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetActiveSubscription 当前生效中的订阅（展示口径）：优先定期订阅（退订/改套餐的对象），
// 否则最近一次的一次性购买。
func (s *Store) GetActiveSubscription(ctx context.Context, tenantID uint64) (*Subscription, error) {
	sub, err := s.GetActiveRecurring(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if sub != nil {
		return sub, nil
	}
	var last Subscription
	err = s.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND plan_type = ?", tenantID, SubActive, PlanTypeOneTime).
		Order("id DESC").First(&last).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &last, nil
}

// GetActiveRecurring 生效中的定期订阅（每租户至多一条）——退订/改套餐/防重复订购的判定对象。
func (s *Store) GetActiveRecurring(ctx context.Context, tenantID uint64) (*Subscription, error) {
	var sub Subscription
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND plan_type = ?", tenantID, SubActive, PlanTypeRecurring).
		First(&sub).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ListSubscriptions 租户全部订阅记录（展示用，倒序）。
func (s *Store) ListSubscriptions(ctx context.Context, tenantID uint64, limit int) ([]Subscription, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []Subscription
	err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListTopups 租户入账流水（倒序）。
func (s *Store) ListTopups(ctx context.Context, tenantID uint64, limit int) ([]Topup, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []Topup
	err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListRechargeOrders 充值订单（倒序）。tenantID=0 返回全部租户（admin 卖方视角）。
func (s *Store) ListRechargeOrders(ctx context.Context, tenantID uint64, limit int) ([]RechargeOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := s.db.WithContext(ctx)
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var rows []RechargeOrder
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// MeView 用户自助页 / AI 客服「查一下」一次拉取的数据。金额单位分（=元/100）。
type MeView struct {
	Balance            int64         `json:"balance"`                 // 实时可用（Redis 投影，分）
	BalanceCent        int64         `json:"balance_cent"`            // 同上，显式声明单位为分（展示端 /100 显示元）
	LedgerBalance      int64         `json:"ledger_balance"`          // 账本口径（权威，分）
	Consistent         bool          `json:"consistent"`              // 两者是否一致
	Subscription       *Subscription `json:"subscription"`            // 生效中的定期订阅（可空）
	Allowance          []TierRemain  `json:"allowance,omitempty"`     // 订阅各档次每窗口剩余额度（token，活跃订阅才非空）
	RefreshHours       int           `json:"refresh_hours,omitempty"` // 额度滚动窗口小时数（活跃订阅套餐的；展示「每 N 小时刷新」）
	Plans              []Plan        `json:"plans"`                   // 套餐目录
	Consumption        []DayStat     `json:"consumption"`             // 近 7 天消耗（分/日）
	ConsumptionByModel []ModelStat   `json:"consumption_by_model"`    // 近 7 天按模型消耗
	Topups             []Topup       `json:"topups"`                  // 最近入账流水
}

// MeOverview 用户视角总览（portal /portal/me 与 agent 读工具共用）。
func (s *Store) MeOverview(ctx context.Context, tenantID uint64) (*MeView, error) {
	bal, err := s.getBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ledger, err := LedgerBalance(ctx, s.db, tenantID)
	if err != nil {
		return nil, err
	}
	sub, _ := s.GetActiveSubscription(ctx, tenantID)
	plans, err := s.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	cons, err := s.ConsumptionStats(ctx, tenantID, 7)
	if err != nil {
		return nil, err
	}
	byModel, err := s.ConsumptionByModel(ctx, tenantID, 7)
	if err != nil {
		return nil, err
	}
	topups, err := s.ListTopups(ctx, tenantID, 20)
	if err != nil {
		return nil, err
	}
	allowance, err := s.SubscriptionAllowance(ctx, sub)
	if err != nil {
		return nil, err
	}
	refreshHours := 0
	if sub != nil {
		if p, _ := s.GetPlan(ctx, sub.PlanID); p != nil {
			refreshHours = p.RefreshHours
		}
	}
	return &MeView{
		Balance: bal, BalanceCent: bal, LedgerBalance: ledger, Consistent: bal == ledger,
		Subscription: sub, Allowance: allowance, RefreshHours: refreshHours, Plans: plans,
		Consumption: cons, ConsumptionByModel: byModel, Topups: topups,
	}, nil
}

// TierRemain 某档次的每窗口额度余量（token）。quota=窗口额度，consumed=窗口已用，remaining=剩余。
type TierRemain struct {
	Tier      string `json:"tier"`
	Quota     int64  `json:"quota"`
	Consumed  int64  `json:"consumed"`
	Remaining int64  `json:"remaining"`
}

// SubscriptionAllowance 活跃订阅的各档次剩余额度（token）。滚动窗口：消费从账单实时推导，
// 未用不累积；窗口滑过即恢复（GPT Plus 式）。非定期订阅/无订阅返回 nil。
func (s *Store) SubscriptionAllowance(ctx context.Context, sub *Subscription) ([]TierRemain, error) {
	if sub == nil || sub.PlanType != PlanTypeRecurring {
		return nil, nil
	}
	p, err := s.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.Status != 0 {
		return nil, nil
	}
	qm := p.TierQuotaMap()
	if len(qm) == 0 {
		return nil, nil
	}
	consumed, err := s.subscriptionConsumed(ctx, sub.TenantID, p.RefreshHours)
	if err != nil {
		return nil, err
	}
	out := make([]TierRemain, 0, len(qm))
	for tier, q := range qm {
		c := consumed[tier]
		remain := q - c
		if remain < 0 {
			remain = 0
		}
		out = append(out, TierRemain{Tier: tier, Quota: q, Consumed: c, Remaining: remain})
	}
	sort.Slice(out, func(i, j int) bool { return tierRank(out[i].Tier) < tierRank(out[j].Tier) })
	return out, nil
}

// subscriptionConsumed 滚动窗口内（最近 refreshHours 小时）按档次的订阅额度消费（token）。
// 只统计 funding=subscription 的 reserve 行（status pending/settled；reversed 不占额度）。
// reserve 行 token 由 ReportUsage 写入（幂等），settle 不再产生第二份消费。
func (s *Store) subscriptionConsumed(ctx context.Context, tenantID uint64, refreshHours int) (map[string]int64, error) {
	if refreshHours <= 0 {
		refreshHours = 5
	}
	since := time.Now().Add(-time.Duration(refreshHours) * time.Hour)
	type row struct {
		Model    string
		Prompt   int64
		Complete int64
	}
	// 只统计 reserve 行（token 由 ReportUsage 写入）：settle 镜像行也带 token，
	// 若不加 phase 过滤会把同一次用量算两遍。
	rows, err := s.db.WithContext(ctx).Model(&Bill{}).
		Where("tenant_id = ? AND phase = ? AND funding = ? AND status IN (?, ?) AND created_at >= ?",
			tenantID, PhaseReserve, FundingSubscription, StatusPending, StatusSettled, since).
		Select("model, COALESCE(SUM(prompt_tokens),0) AS prompt, COALESCE(SUM(completion_tokens),0) AS complete").
		Group("model").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tierOf := map[string]string{}
	consumed := map[string]int64{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Model, &r.Prompt, &r.Complete); err != nil {
			return nil, err
		}
		tier, ok := tierOf[r.Model]
		if !ok {
			if m, e := s.GetModelByModelID(ctx, r.Model); e == nil && m != nil {
				tier = TierFromTags(m.TagsList())
			}
			tierOf[r.Model] = tier
		}
		consumed[tier] += r.Prompt + r.Complete
	}
	return consumed, rows.Err()
}

// tierRank 档次在目录中的排序权重（旗舰=0 … 免费=6），用于稳定展示。
func tierRank(tier string) int {
	for i, t := range tierOrder {
		if t == tier {
			return i
		}
	}
	return len(tierOrder)
}

// DayStat 单日消耗（面板柱状）。Tokens 单位为分。
type DayStat struct {
	Day    string `json:"day"`
	Tokens int64  `json:"tokens"`
}

// ModelStat 买家按模型消耗视图：近 N 天某模型调用几次、用了多少 token、花了多少分。
type ModelStat struct {
	Model        string `json:"model"`            // 目录 model_id
	Vendor       string `json:"vendor,omitempty"` // 厂商（JOIN models）
	InputTokens  int64  `json:"input_tokens"`     // prompt token
	OutputTokens int64  `json:"output_tokens"`    // completion token
	CostCent     int64  `json:"cost_cent"`        // 实际消耗金额（分）
	Calls        int64  `json:"calls"`            // 调用次数
}

// ConsumptionByModel 近 days 天按模型分组消耗（settle.actual 求和，JOIN 目录取厂商）。
func (s *Store) ConsumptionByModel(ctx context.Context, tenantID uint64, days int) ([]ModelStat, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := s.db.WithContext(ctx).Model(&Bill{}).
		Select("bills.model, models.vendor, COALESCE(SUM(bills.actual_quota),0) AS cost_cent, "+
			"COALESCE(SUM(bills.prompt_tokens),0) AS input_tokens, COALESCE(SUM(bills.completion_tokens),0) AS output_tokens, COUNT(*) AS calls").
		Joins("LEFT JOIN models ON models.model_id = bills.model").
		Where("bills.tenant_id = ? AND bills.phase = ? AND bills.status = ? AND bills.created_at >= ?",
			tenantID, PhaseSettle, StatusSettled, since).
		Group("bills.model, models.vendor").
		Order("cost_cent DESC").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelStat
	for rows.Next() {
		var r ModelStat
		if err := rows.Scan(&r.Model, &r.Vendor, &r.CostCent, &r.InputTokens, &r.OutputTokens, &r.Calls); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ConsumptionStats 近 days 天每日结算消耗（settle.actual 求和，缺失日补 0）。
func (s *Store) ConsumptionStats(ctx context.Context, tenantID uint64, days int) ([]DayStat, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	type row struct {
		Day    string
		Tokens int64
	}
	rows, err := s.db.WithContext(ctx).Model(&Bill{}).
		Where("tenant_id = ? AND phase = ? AND status = ? AND created_at >= ?",
			tenantID, PhaseSettle, StatusSettled, since).
		Select("DATE_FORMAT(created_at, '%Y-%m-%d') AS day, COALESCE(SUM(actual_quota),0) AS tokens").
		Group("day").Order("day ASC").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]int64{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Day, &r.Tokens); err != nil {
			return nil, err
		}
		byDay[r.Day] = r.Tokens
	}
	// 补齐缺失天
	out := make([]DayStat, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := time.Now().Add(-time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
		out = append(out, DayStat{Day: d, Tokens: byDay[d]})
	}
	return out, nil
}

// ---------- 充值 ----------

// CreateRechargeOrder 建充值订单（pending_payment）。idemKey 非空时作 order_no（幂等锚点），
// 重复 idemKey 返回已有订单。
func (s *Store) CreateRechargeOrder(ctx context.Context, tenantID uint64, amountMoney int64, idemKey string) (*RechargeOrder, error) {
	if amountMoney <= 0 {
		return nil, fmt.Errorf("充值金额必须 > 0")
	}
	orderNo := idemKey
	if orderNo == "" {
		orderNo = newOrderNo()
	}
	o := &RechargeOrder{
		TenantID: tenantID, OrderNo: orderNo,
		AmountMoney: amountMoney, AmountCent: amountMoney * quota.CentPerYuan,
		Status: RechargePending, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(o).Error; err != nil {
		if isDupKey(err) {
			return s.rechargeByOrderNo(ctx, orderNo) // 幂等重放
		}
		return nil, err
	}
	return o, nil
}

// PayRecharge 模拟支付：pending_payment → paid，并把额度入账（topup 行幂等）。
func (s *Store) PayRecharge(ctx context.Context, orderNo string) (*RechargeOrder, error) {
	o, err := s.rechargeByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("充值订单不存在: %s", orderNo)
	}
	if o.Status == RechargePaid {
		return o, nil // 幂等
	}
	if o.Status != RechargePending {
		return nil, fmt.Errorf("订单 %s 状态 %s，不可支付", orderNo, o.Status)
	}
	// 1) topup 行是提交点（ref 唯一 → 重试/并发只入账一次）
	if err := s.grantTokens(ctx, o.TenantID, TopupRecharge, TopupRecharge, o.OrderNo, o.AmountCent); err != nil {
		return nil, err
	}
	// 2) 状态推进（守卫：只有 pending_payment 允许）
	paidAt := time.Now()
	if err := s.db.WithContext(ctx).Model(&RechargeOrder{}).
		Where("id = ? AND status = ?", o.ID, RechargePending).
		Updates(map[string]any{"status": RechargePaid, "paid_at": paidAt}).Error; err != nil {
		return nil, err
	}
	// 3) 返回权威行：o 是更新前读的，status 仍是 pending_payment——原地刷新，
	//    否则响应里带旧状态会误导调用方（admin/agent/脚本都靠它判断）。
	o.Status = RechargePaid
	o.PaidAt = &paidAt
	return o, nil
}

// RefundResult 退款结果（展示退款量与余额，单位分）。
// OrderNo 按订单退时是订单号，按金额退时为空；AmountCent 相应为订单原金额 / 要退的目标金额（分）。
type RefundResult struct {
	OrderNo      string `json:"order_no"`
	AmountCent   int64  `json:"amount_cent"`   // 订单原金额 / 按金额退的目标金额（分）
	RefundCent   int64  `json:"refund_cent"`   // 实际退回 = min(目标, 余额)（分）
	BalanceAfter int64  `json:"balance_after"` // 退款后余额（分）
}

// RefundRecharge 充值退款（话费式）：只退已支付订单，退款量 = min(订单额度, 当前余额)，绝不退负。
func (s *Store) RefundRecharge(ctx context.Context, tenantID uint64, orderNo string) (*RefundResult, error) {
	o, err := s.rechargeByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("充值订单不存在: %s", orderNo)
	}
	if o.TenantID != tenantID {
		return nil, fmt.Errorf("无权操作他人订单: %s", orderNo)
	}
	if o.Status == RechargeRefunded {
		return &RefundResult{OrderNo: o.OrderNo, AmountCent: o.AmountCent, RefundCent: 0, BalanceAfter: mustBalance(s, ctx, tenantID)}, nil
	}
	if o.Status != RechargePaid {
		return nil, fmt.Errorf("订单 %s 状态 %s，不可退款（仅已支付可退）", orderNo, o.Status)
	}
	bal, err := s.getBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	refund := min64(o.AmountCent, bal)
	if refund <= 0 {
		return nil, fmt.Errorf("当前余额为 0，订单 %s 无可退额度", orderNo)
	}
	// 1) 负 topup 行是提交点（幂等：同 order_no 只退一次）
	if err := s.grantTokens(ctx, tenantID, TopupRefund, TopupRefund, o.OrderNo, -refund); err != nil {
		return nil, err
	}
	// 2) 状态推进（守卫：只有 paid 允许）
	if err := s.db.WithContext(ctx).Model(&RechargeOrder{}).
		Where("id = ? AND status = ?", o.ID, RechargePaid).
		Updates(map[string]any{"status": RechargeRefunded}).Error; err != nil {
		return nil, err
	}
	return &RefundResult{OrderNo: o.OrderNo, AmountCent: o.AmountCent, RefundCent: refund, BalanceAfter: bal - refund}, nil
}

// RefundAmount 按金额退款（不绑定订单，话费式）：退款量 = min(要退的金额, 当前余额)，绝不退负。
// idemKey 是幂等锚点（topup ref = refund:idemKey）；同 key 重试返回已退结果，不再动余额。
func (s *Store) RefundAmount(ctx context.Context, tenantID uint64, amountMoney int64, idemKey string) (*RefundResult, error) {
	if amountMoney <= 0 {
		return nil, fmt.Errorf("退款金额必须 > 0")
	}
	if idemKey == "" {
		idemKey = newOrderNo() // 直接打 admin 未带锚点时自生成；agent/portal 会显式传
	}
	// 幂等：同锚点已退过 → 直接返回已退结果
	var existed int64
	if err := s.db.WithContext(ctx).Model(&Topup{}).
		Where("tenant_id = ? AND ref_type = ? AND ref_id = ?", tenantID, TopupRefund, idemKey).
		Count(&existed).Error; err != nil {
		return nil, err
	}
	target := amountMoney * quota.CentPerYuan
	if existed > 0 {
		return &RefundResult{OrderNo: "", AmountCent: target, RefundCent: 0, BalanceAfter: mustBalance(s, ctx, tenantID)}, nil
	}
	bal, err := s.getBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	refund := min64(target, bal)
	if refund <= 0 {
		return nil, fmt.Errorf("当前余额为 0，无可退额度")
	}
	// 负 topup 行是提交点（幂等：同 idemKey 只退一次）
	if err := s.grantTokens(ctx, tenantID, TopupRefund, TopupRefund, idemKey, -refund); err != nil {
		return nil, err
	}
	return &RefundResult{OrderNo: "", AmountCent: target, RefundCent: refund, BalanceAfter: bal - refund}, nil
}

// ---------- 订阅 ----------

// SubscribePlan 订阅套餐（M9 起 GPT Plus 式）：只开通「每窗口 token 额度资格」，不再入账钱。
// 月费是模拟价不接真钱；额度内使用免费（Reserve 走 funding=subscription），余额不动。
func (s *Store) SubscribePlan(ctx context.Context, tenantID uint64, planID uint64) (*Subscription, error) {
	p, err := s.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("套餐不存在: %d", planID)
	}
	if p.Status != 0 {
		return nil, fmt.Errorf("套餐已下架: %s", p.Name)
	}
	if p.PlanType != PlanTypeRecurring {
		return nil, fmt.Errorf("该套餐已不支持订购（仅定期订阅在售）: %s", p.Name)
	}
	if sub, _ := s.GetActiveRecurring(ctx, tenantID); sub != nil {
		return nil, fmt.Errorf("已有生效中的定期订阅（套餐#%d），请先退订或改套餐", sub.PlanID)
	}
	now := time.Now()
	end := now.Add(time.Duration(p.ValidityDays) * 24 * time.Hour)
	sub := &Subscription{
		TenantID: tenantID, PlanID: p.ID, PlanType: p.PlanType,
		Status: SubActive, AutoRenew: true,
		CycleStart: &now, CycleEnd: &end,
		CycleNum: 1, QuotaGranted: 0, // 额度资格，无钱可授
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

// CancelResult 退订结果（当期不退，退款金额恒 0）。
type CancelResult struct {
	PlanName        string     `json:"plan_name"`
	Refund          int64      `json:"refund"` // 恒 0（当期不退，只停续费）
	CurrentCycleEnd *time.Time `json:"current_cycle_end"`
}

// CancelSubscription 退订。只针对定期订阅：停续费、本期用到期，当期不退（话费口径）。
// 一次性套餐无续费可退 → 明确拒绝（用户口径：买断不可退订）。
func (s *Store) CancelSubscription(ctx context.Context, tenantID uint64) (*CancelResult, error) {
	sub, err := s.GetActiveRecurring(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		// 区分「只有一次性套餐」与「完全没有订阅」，给用户准确拒绝文案
		var oneTime int64
		s.db.WithContext(ctx).Model(&Subscription{}).
			Where("tenant_id = ? AND status = ? AND plan_type = ?", tenantID, SubActive, PlanTypeOneTime).
			Count(&oneTime)
		if oneTime > 0 {
			return nil, fmt.Errorf("当前只有一次性套餐（买断不可退订），无定期订阅可退")
		}
		return nil, fmt.Errorf("无生效中的订阅")
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Subscription{}).
		Where("id = ? AND status = ?", sub.ID, SubActive).
		Updates(map[string]any{"auto_renew": false, "status": SubCancelled, "cancelled_at": now, "updated_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	p, _ := s.GetPlan(ctx, sub.PlanID)
	name := ""
	if p != nil {
		name = p.Name
	}
	return &CancelResult{PlanName: name, Refund: 0, CurrentCycleEnd: sub.CycleEnd}, nil
}

// ChangePlan 改套餐：仅定期订阅之间，记录 pending_plan_id 下期生效（运营商口径）。
func (s *Store) ChangePlan(ctx context.Context, tenantID uint64, newPlanID uint64) error {
	sub, err := s.GetActiveRecurring(ctx, tenantID)
	if err != nil {
		return err
	}
	if sub == nil {
		var oneTime int64
		s.db.WithContext(ctx).Model(&Subscription{}).
			Where("tenant_id = ? AND status = ? AND plan_type = ?", tenantID, SubActive, PlanTypeOneTime).
			Count(&oneTime)
		if oneTime > 0 {
			return fmt.Errorf("一次性套餐不支持改套餐（买断不变更）")
		}
		return fmt.Errorf("无生效中的订阅")
	}
	p, err := s.GetPlan(ctx, newPlanID)
	if err != nil {
		return err
	}
	if p == nil || p.Status != 0 || p.PlanType != PlanTypeRecurring {
		return fmt.Errorf("目标套餐无效或非定期订阅: %d", newPlanID)
	}
	res := s.db.WithContext(ctx).Model(&Subscription{}).
		Where("id = ? AND status = ?", sub.ID, SubActive).
		Updates(map[string]any{"pending_plan_id": newPlanID, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RenewDueSubscriptions 续费器：扫到期且 auto_renew 的订阅，推进下一计费周期并应用 pending 改套餐。
// M9 起额度窗口是滚动实时推导的（消费随 created_at 滑出窗口自动恢复），续费器不再动钱。
func (s *Store) RenewDueSubscriptions(ctx context.Context) (int, error) {
	now := time.Now()
	var subs []Subscription
	if err := s.db.WithContext(ctx).
		Where("status = ? AND auto_renew = ? AND cycle_end IS NOT NULL AND cycle_end <= ?",
			SubActive, true, now).Find(&subs).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, sub := range subs {
		planID := sub.PlanID
		if sub.PendingPlanID != nil {
			planID = *sub.PendingPlanID
		}
		p, err := s.GetPlan(ctx, planID)
		if err != nil {
			continue
		}
		if p == nil || p.Status != 0 || p.PlanType != PlanTypeRecurring {
			// 目标套餐不可用：停止续费，本期用到自然结束
			_ = s.db.WithContext(ctx).Model(&Subscription{}).
				Where("id = ?", sub.ID).Update("auto_renew", false).Error
			continue
		}
		newStart := *sub.CycleEnd
		newEnd := newStart.Add(time.Duration(p.ValidityDays) * 24 * time.Hour)
		_ = s.db.WithContext(ctx).Model(&Subscription{}).
			Where("id = ? AND status = ?", sub.ID, SubActive).
			Updates(map[string]any{
				"plan_id": planID, "cycle_start": newStart, "cycle_end": newEnd,
				"cycle_num":       sub.CycleNum + 1,
				"pending_plan_id": nil, "updated_at": now,
			}).Error
		count++
	}
	return count, nil
}

// ---------- AI 会话 / 审计 ----------

// RecordSession 记录/刷新客服会话。
//
// 实现说明：不要先 Create 再靠 duplicate-key 兜回 Update —— 那样每轮对话都会让
// GORM 打一条 Error 1062 日志。同一 session 连续多轮是常态，这些噪音会淹没真错误。
// 这里改用 UPDATE 优先，未命中再 INSERT（并发下 INSERT 仍可能撞键，保留兜底）。
func (s *Store) RecordSession(ctx context.Context, sessionID string, tenantID, apiKeyID uint64) error {
	now := time.Now()
	db := s.db.WithContext(ctx)

	res := db.Model(&AgentSession{}).
		Where("session_id = ?", sessionID).
		Update("last_active_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}

	ses := &AgentSession{SessionID: sessionID, TenantID: tenantID, APIKeyID: apiKeyID, CreatedAt: now, LastActiveAt: now}
	if err := db.Create(ses).Error; err != nil {
		if isDupKey(err) { // 并发下被别的请求抢先插入 → 视为已存在
			return db.Model(&AgentSession{}).
				Where("session_id = ?", sessionID).
				Update("last_active_at", now).Error
		}
		return err
	}
	return nil
}

// RecordAudit 记录一次 AI 工具执行（读/写都记，写操作含确认标记与预览文案）。
// guard 为 P3 Jev 安全决策 JSON（可空）。
func (s *Store) RecordAudit(ctx context.Context, sessionID string, tenantID uint64, tool, args string, confirmed bool, preview, result, guard string) error {
	return s.db.WithContext(ctx).Create(&AgentAuditLog{
		SessionID: sessionID, TenantID: tenantID, Tool: tool,
		Args: args, Confirmed: confirmed, Preview: preview, Result: result, Guard: guard, CreatedAt: time.Now(),
	}).Error
}

// ListAuditLogs 审计列表（倒序）。tenantID=0 返回全部租户（admin 卖方视角）。
func (s *Store) ListAuditLogs(ctx context.Context, tenantID uint64, limit int) ([]AgentAuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := s.db.WithContext(ctx)
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var rows []AgentAuditLog
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ---------- 内部 helpers ----------

func (s *Store) rechargeByOrderNo(ctx context.Context, orderNo string) (*RechargeOrder, error) {
	var o RechargeOrder
	err := s.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&o).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// grantTokens 入账：topup 行是提交点（ref 唯一索引），插入成功才更新 Redis 投影。
// 重复调用（重试/并发）由唯一索引拦下，不再动 Redis —— 与 bills.uk_request_phase 同理。
// 投影更新失败不阻断：Redis 只是投影，M4 rebuild 以账本收敛。
func (s *Store) grantTokens(ctx context.Context, tenantID uint64, source, refType, refID string, amount int64) error {
	if amount == 0 {
		return nil
	}
	row := &Topup{TenantID: tenantID, Source: source, Amount: amount, RefType: refType, RefID: refID, CreatedAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		if isDupKey(err) {
			return nil // 已入账过（幂等）
		}
		return err
	}
	_ = s.rdb.IncrBy(ctx, BalanceKey(tenantID), amount).Err()
	return nil
}

func newOrderNo() string {
	// 与 xid 同理：crypto/rand 16 字节 hex，128bit 全局唯一。
	return "ord_" + randHex(16)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func mustBalance(s *Store, ctx context.Context, tenantID uint64) int64 {
	b, _ := s.getBalance(ctx, tenantID)
	return b
}

// randHex crypto/rand 生成 n 字节 hex（订单号用，不强依赖时间）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	const hexc = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hexc[v>>4]
		out[i*2+1] = hexc[v&0xf]
	}
	return string(out)
}
