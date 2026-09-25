// Package router 上游 provider 选择 + 容灾（M2 权重随机 / M5 熔断 + AutoBan）。
// Route() 只返回「健康」的 provider：DB status=active 且 熔断器允许放行。
// 熔断状态是进程内存（每 provider 一个，独立演进），AutoBan 是持久层（写回 MySQL，
// 进程重启后依然 ban）。两层的阈值不同：熔断快（5 连败 → OPEN，兜住波动），
// AutoBan 狠（20 连败 → banned，直接移除候选集）。
package router

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"gorm.io/gorm"
)

// 熔断默认阈值（生产用；可经 ROUTER_* 环境变量覆盖，场景脚本调小以加速复现）。
const (
	defaultFailThreshold = 5                // 连续失败 N 次 → OPEN
	defaultCooldown      = 10 * time.Second // OPEN 持续多久后进 HALF-OPEN
	defaultAutobanAfter  = 20               // 跨周期累计失败 N 次 → providers.status=banned
)

type Provider struct {
	ID          uint64
	Name        string
	BaseURL     string
	UpstreamKey string // 上游 API key（/route 带给网关，转发时替换租户 key 鉴权）
	Weight      int
}

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// breaker 单 provider 的熔断状态机。
type breaker struct {
	state           breakerState
	consecutiveFail int
	probeInFlight   bool      // HALF-OPEN 下真正发出、等回报的探测（0/1）
	probeSince      time.Time // 探测发出时刻（超时释放，防网关崩了卡死探测位）
	nextClosedAt    time.Time // OPEN 到期时间（之后进 HALF-OPEN）
}

// FailThreshold / Cooldown / AutobanAfter 可配（生产默认值见 default* 常量）。
type Registry struct {
	db            *gorm.DB
	log           *slog.Logger
	ttl           time.Duration
	Cooldown      time.Duration // OPEN 持续多久后进 HALF-OPEN
	FailThreshold int           // 连续失败 N 次 → OPEN
	AutobanAfter  int           // 跨周期累计失败 N 次 → banned
	probeTimeout  time.Duration // 探测发出后多久无回报即释放探测位（网关可能崩了）
	mu            sync.Mutex
	cache         map[string]*cachedRoute
	bk            map[uint64]*breaker
}

type cachedRoute struct {
	providers []Provider
	expireAt  time.Time
}

const defaultProbeTimeout = 30 * time.Second

func New(db *gorm.DB, log *slog.Logger) *Registry {
	return &Registry{
		db:            db,
		log:           log,
		ttl:           30 * time.Second,
		Cooldown:      defaultCooldown,
		FailThreshold: defaultFailThreshold,
		AutobanAfter:  defaultAutobanAfter,
		probeTimeout:  defaultProbeTimeout,
		cache:         make(map[string]*cachedRoute),
		bk:            make(map[uint64]*breaker),
	}
}

// Route 返回该模型可用且熔断允许的 provider（权重随机）。无可用返回 nil。
func (r *Registry) Route(model string) (*Provider, error) {
	return r.RouteExclude(model, nil)
}

// RouteExclude 同 Route，但排除本请求已尝试失败的 provider——failover 的核心：
// 重试必须换一个节点，否则加权随机可能再次选中同一个坏节点（request 白费）。
// exclude 为空 map/nil 时与 Route 等价。无候选返回 (nil, nil)。
func (r *Registry) RouteExclude(model string, exclude map[uint64]bool) (*Provider, error) {
	ps, err := r.snapshot(model)
	if err != nil {
		return nil, err
	}
	avail := r.available(ps)
	filtered := make([]Provider, 0, len(avail))
	for _, p := range avail {
		if !exclude[p.ID] {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	pick := pickWeighted(filtered)
	r.markProbe(pick)
	return pick, nil
}

// markProbe 选中的 provider 若是 HALF-OPEN，则这次选择就是「探测」：占住探测位，
// 等 gateway 的 Report 回报后释放。只在实际选中时占位，避免「候选里有但没被选中」
// 就把探测位永久卡死（选不中就一直排除，静默下线）。
func (r *Registry) markProbe(pick *Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bk[pick.ID]; ok && b.state == breakerHalfOpen {
		b.probeInFlight = true
		b.probeSince = time.Now()
	}
}

// Report 上报一次调用结果（gateway 在「首个字节前失败/成功」时调用）。
//   - 成功：HALF-OPEN 探测成功 → 回 CLOSED；CLOSED 清零连续失败；重置 DB 计数。
//   - 失败：CLOSED 连续失败达标 → OPEN；HALF-OPEN 探测失败 → 回 OPEN；持久化 AutoBan。
func (r *Registry) Report(providerID uint64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	b := r.breaker(providerID)
	now := time.Now()
	if ok {
		b.consecutiveFail = 0
		if b.state == breakerHalfOpen {
			b.state = breakerClosed
			b.probeInFlight = false
			r.logState(providerID, b, "recover")
		}
		r.resetDBFails(providerID)
		return
	}

	b.consecutiveFail++
	if b.state == breakerHalfOpen {
		b.probeInFlight = false
	}
	if b.consecutiveFail >= r.AutobanAfter {
		r.ban(providerID, b)
		return
	}
	if b.consecutiveFail >= r.FailThreshold && b.state != breakerOpen {
		b.state = breakerOpen
		b.nextClosedAt = now.Add(r.Cooldown)
		r.logState(providerID, b, "open")
		return
	}
	r.persistFail(providerID)
}

// available 按熔断状态过滤候选集。这里只判断「放不放行」，真正的探测占位在 Route 选中后
// 由 markProbe 完成——否则候选里有但没被选中，探测位就永远卡死。
func (r *Registry) available(ps []Provider) []Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Provider, 0, len(ps))
	now := time.Now()
	for _, p := range ps {
		b := r.breaker(p.ID)
		switch b.state {
		case breakerOpen:
			if now.After(b.nextClosedAt) {
				// OPEN 到期 → 进 HALF-OPEN，放一个探测
				b.state = breakerHalfOpen
				b.probeInFlight = false
				out = append(out, p)
			}
			// 未到期：排除
		case breakerHalfOpen:
			if b.probeInFlight {
				if now.After(b.probeSince.Add(r.probeTimeout)) {
					// 探测超时无回报（gateway 可能崩了）→ 释放探测位，让下个探测有机会
					b.probeInFlight = false
					out = append(out, p)
				}
				continue
			}
			out = append(out, p)
		default: // CLOSED
			out = append(out, p)
		}
	}
	return out
}

// breaker 取/建单 provider 状态（调用方已持锁）。
func (r *Registry) breaker(id uint64) *breaker {
	if b, ok := r.bk[id]; ok {
		return b
	}
	b := &breaker{state: breakerClosed}
	r.bk[id] = b
	return b
}

// persistFail 累计 DB 连续失败计数（AutoBan 的数据来源；写失败只记日志，不影响内存状态）。
// db 为 nil（单测）时跳过持久化，只演进内存状态机。
func (r *Registry) persistFail(providerID uint64) {
	if r.db == nil {
		return
	}
	if err := r.db.Table("providers").
		Where("id = ? AND status = ?", providerID, 0).
		UpdateColumn("consecutive_fail", gorm.Expr("consecutive_fail + 1")).Error; err != nil {
		r.log.Error("persist fail", "provider_id", providerID, "err", err)
	}
}

// resetDBFails 成功后清零 DB 连续失败计数。
func (r *Registry) resetDBFails(providerID uint64) {
	if r.db == nil {
		return
	}
	if err := r.db.Table("providers").
		Where("id = ? AND status = ?", providerID, 0).
		UpdateColumn("consecutive_fail", 0).Error; err != nil {
		r.log.Error("reset fails", "provider_id", providerID, "err", err)
	}
}

// ban AutoBan：连续失败达到阈值 → 永久(人工解除)ban，并从候选集移除。
func (r *Registry) ban(providerID uint64, b *breaker) {
	b.state = breakerOpen
	b.nextClosedAt = time.Now().Add(24 * time.Hour) // 兜底：即便人工忘了解除，也要 24h 后自动恢复探测
	if r.db != nil {
		if err := r.db.Table("providers").
			Where("id = ? AND status = ?", providerID, 0).
			Updates(map[string]any{"status": 2, "consecutive_fail": b.consecutiveFail}).Error; err != nil {
			r.log.Error("autoban persist", "provider_id", providerID, "err", err)
		}
		r.clearCache()
	}
	r.logState(providerID, b, "autoban")
}

// clearCache 让 DB 状态变化（ban）立即生效，不等 30s 缓存过期。
func (r *Registry) clearCache() {
	r.cache = make(map[string]*cachedRoute)
}

func (r *Registry) logState(id uint64, b *breaker, ev string) {
	r.log.Info("breaker", "event", ev, "provider_id", id, "state", b.state, "consecutive_fail", b.consecutiveFail)
}

// State 单个 provider 的熔断观测信息（/breakers 端点 + 压测脚本断言用）。
type State struct {
	ProviderID      uint64 `json:"provider_id"`
	State           string `json:"state"` // closed / open / half_open
	ConsecutiveFail int    `json:"consecutive_fail"`
	NextClosedAt    string `json:"next_closed_at,omitempty"`
}

// States 导出全部熔断状态（观测端点）。只读快照，不上锁长时间占用。
func (r *Registry) States() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	states := make([]State, 0, len(r.bk))
	for id, b := range r.bk {
		name := "closed"
		switch b.state {
		case breakerOpen:
			name = "open"
		case breakerHalfOpen:
			name = "half_open"
		}
		s := State{ProviderID: id, State: name, ConsecutiveFail: b.consecutiveFail}
		if b.state == breakerOpen {
			s.NextClosedAt = b.nextClosedAt.Format(time.RFC3339)
		}
		states = append(states, s)
	}
	return states
}

// ProviderState 面板观测：provider 行 + 内存熔断状态合并。
type ProviderState struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	BaseURL         string `json:"base_url"`
	Models          string `json:"models"`
	Weight          int    `json:"weight"`
	Status          int8   `json:"status"` // 0=active 1=disabled 2=banned
	ConsecutiveFail int    `json:"consecutive_fail"`
	BreakerState    string `json:"breaker_state"` // closed / open / half_open
	NextClosedAt    string `json:"next_closed_at,omitempty"`
}

// Providers 全量 provider 行 + 熔断状态（/providers 面板端点）。
// 区别于 States() 只覆盖「被路由触达过」的 provider：这里直查表，未触达的按 closed 展示。
func (r *Registry) Providers() []ProviderState {
	if r.db == nil {
		return nil
	}
	type row struct {
		ID              uint64
		Name            string
		BaseURL         string
		Models          string
		Weight          int
		Status          int8
		ConsecutiveFail int
	}
	var rows []row
	// 本地 struct 名不是表名，必须显式 Table("providers")（同 snapshot）。
	if err := r.db.Table("providers").Order("id").Find(&rows).Error; err != nil {
		r.log.Error("providers query", "err", err)
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProviderState, 0, len(rows))
	for _, rr := range rows {
		ps := ProviderState{
			ID: rr.ID, Name: rr.Name, BaseURL: rr.BaseURL, Models: rr.Models,
			Weight: rr.Weight, Status: rr.Status, ConsecutiveFail: rr.ConsecutiveFail,
			BreakerState: "closed",
		}
		if b, ok := r.bk[rr.ID]; ok {
			ps.ConsecutiveFail = b.consecutiveFail
			switch b.state {
			case breakerOpen:
				ps.BreakerState = "open"
				ps.NextClosedAt = b.nextClosedAt.Format(time.RFC3339)
			case breakerHalfOpen:
				ps.BreakerState = "half_open"
			}
		}
		out = append(out, ps)
	}
	return out
}

// ---------- snapshot（M2 原逻辑） ----------

func (r *Registry) snapshot(model string) ([]Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[model]; ok && time.Now().Before(c.expireAt) {
		return c.providers, nil
	}

	type row struct {
		ID          uint64
		Name        string
		BaseURL     string
		UpstreamKey string
		Weight      int
		Models      string // JSON 数组
	}
	var rows []row
	// 注意：本地 struct 名不是表名，必须显式 Table("providers")，否则 gorm 推断成 `rows`。
	if err := r.db.Table("providers").Where("status = ?", 0).Find(&rows).Error; err != nil {
		return nil, err
	}

	ps := make([]Provider, 0, len(rows))
	for _, rr := range rows {
		var models []string
		_ = json.Unmarshal([]byte(rr.Models), &models)
		// "*" 通配 = 服务全部模型：目录与路由解耦（seed providers 用 ["*"]）。
		if contains(models, "*") || contains(models, model) {
			ps = append(ps, Provider{ID: rr.ID, Name: rr.Name, BaseURL: rr.BaseURL, UpstreamKey: rr.UpstreamKey, Weight: rr.Weight})
		}
	}
	r.cache[model] = &cachedRoute{providers: ps, expireAt: time.Now().Add(r.ttl)}
	return ps, nil
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func pickWeighted(ps []Provider) *Provider {
	total := 0
	for _, p := range ps {
		total += p.Weight
	}
	n := rand.Intn(total)
	for i := range ps {
		n -= ps[i].Weight
		if n < 0 {
			return &ps[i]
		}
	}
	return &ps[len(ps)-1]
}
