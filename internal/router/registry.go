// Package router 上游 provider 选择 + 容灾（M2 权重随机 / M5 熔断）。
// Route() 只返回「健康」的 provider：DB status=active 且当前允许放行。
// 配了 Redis 时，只累计一分钟内的平台失败。成功和用户侧拒绝不记。
// 这一分钟满 AutobanAfter 次，就把 MySQL 里的上游标成停用，直到探测成功才打开。
// 没满 20 次时，这一分钟结束次数自己消失。没配 Redis 时，仍用本进程的连续失败熔断。
package router

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 熔断默认阈值（生产用；可经 ROUTER_* 环境变量覆盖，场景脚本调小以加速复现）。
const (
	defaultFailThreshold = 5                // 无 Redis 时，连续失败 N 次 → OPEN
	defaultCooldown      = 10 * time.Second // 无 Redis 时，OPEN 持续多久后进 HALF-OPEN
	defaultAutobanAfter  = 20               // 一分钟内平台失败 N 次 → MySQL 停用
	defaultFailWindow    = 60 * time.Second
)

type Provider struct {
	MessagesBaseURL  string
	ResponsesBaseURL string
	ID               uint64
	Name             string
	BaseURL          string
	UpstreamKey      string // 上游 API key（/route 带给网关，转发时替换租户 key 鉴权）
	Weight           int
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
	nextClosedAt    time.Time // OPEN 到期时间
	banned          bool      // 已写入停用。探测成功前不再放行。
}

// FailThreshold / Cooldown / AutobanAfter 可配（生产默认值见 default* 常量）。
type Registry struct {
	db            *gorm.DB
	failRedis     *goredis.Client // 一分钟内的失败次数。空则只用本进程内存。
	log           *slog.Logger
	ttl           time.Duration
	Cooldown      time.Duration // 无 Redis 时，OPEN 持续多久后进 HALF-OPEN
	FailWindow    time.Duration // 失败次数从第一次失败起保留这么久
	FailThreshold int           // 无 Redis 时，连续失败 N 次 → OPEN
	AutobanAfter  int           // 一分钟内平台失败 N 次 → MySQL 停用
	probeTimeout  time.Duration // 探测发出后多久无回报即释放探测位（网关可能崩了）
	ActiveRecheck time.Duration // 缓存名单隔多久再核对一次仍是启用。0 表示每次选路都查库。
	activeChecked time.Time
	mu            sync.RWMutex
	warmOnce      sync.Once
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
		FailWindow:    defaultFailWindow,
		FailThreshold: defaultFailThreshold,
		AutobanAfter:  defaultAutobanAfter,
		probeTimeout:  defaultProbeTimeout,
		ActiveRecheck: time.Second,
		cache:         make(map[string]*cachedRoute),
		bk:            make(map[uint64]*breaker),
	}
}

// EnableFailureRedis 把一分钟内的平台失败次数放到 Redis。成功不写。
func (r *Registry) EnableFailureRedis(rdb *goredis.Client) {
	r.failRedis = rdb
}

func failureKey(id uint64) string {
	return "router:fail:" + strconv.FormatUint(id, 10)
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
	ps, err = r.activeOnly(model, ps)
	if err != nil {
		return nil, err
	}
	if r.failRedis != nil {
		r.WarmFailures()
		return r.routeShared(ps, exclude), nil
	}
	// 选择和占住探测位必须在同一把锁里。否则两个并发 Route 都会看到
	// probeInFlight=false，HALF-OPEN 会放出多个探测。
	r.mu.Lock()
	defer r.mu.Unlock()
	avail := r.availableLocked(ps)
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
	r.markProbeLocked(pick)
	return pick, nil
}

// markProbe 同 markProbeLocked，自己拿锁。测试和锁外调用用这个。
func (r *Registry) markProbe(pick *Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markProbeLocked(pick)
}

// markProbeLocked 选中的 provider 若是 HALF-OPEN，则这次选择就是「探测」。
// 调用方必须已持有 r.mu。
func (r *Registry) markProbeLocked(pick *Provider) {
	if pick == nil {
		return
	}
	if b, ok := r.bk[pick.ID]; ok && b.state == breakerHalfOpen {
		b.probeInFlight = true
		b.probeSince = time.Now()
	}
}

// windowIncr 给失败次数加一。次数从第一次失败起保留 FailWindow，成功不碰这个键。
var windowIncr = goredis.NewScript(`
local n = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
`)

// Report 上报一次调用结果（gateway 在「首个字节前失败/成功」时调用）。
// 配了 Redis 时，成功直接返回。平台失败计入一分钟窗口，满 AutobanAfter 次就停用。
func (r *Registry) Report(providerID uint64, ok bool) {
	if r.failRedis != nil {
		r.reportWindow(providerID, ok)
		return
	}
	r.reportMemory(providerID, ok)
}

func (r *Registry) reportWindow(providerID uint64, ok bool) {
	if ok {
		return
	}
	count, _, counted := r.addWindowFailure(providerID)
	if !counted || count < r.AutobanAfter {
		return
	}
	if r.markBanned(providerID, count) {
		go r.persistBan(providerID, count)
	}
}

// reportMemory 没有 Redis 时的连续失败熔断。成功清零。满 FailThreshold 暂停 Cooldown。
// 满 AutobanAfter 把 MySQL 里的上游标成停用。
func (r *Registry) reportMemory(providerID uint64, ok bool) {
	r.mu.Lock()
	b := r.breaker(providerID)
	now := time.Now()
	if ok {
		b.consecutiveFail = 0
		if b.state == breakerHalfOpen {
			b.state = breakerClosed
			b.probeInFlight = false
			r.logState(providerID, b, "recover")
		}
		r.mu.Unlock()
		return
	}
	b.consecutiveFail++
	count := b.consecutiveFail
	probeFailed := b.state == breakerHalfOpen
	if probeFailed {
		b.probeInFlight = false
	}
	banNow := false
	if count >= r.AutobanAfter {
		b.state = breakerOpen
		b.nextClosedAt = now.Add(24 * time.Hour)
		r.clearCache()
		r.logState(providerID, b, "autoban")
		banNow = true
	} else if probeFailed || (count >= r.FailThreshold && b.state != breakerOpen) {
		b.state = breakerOpen
		b.nextClosedAt = now.Add(r.Cooldown)
		r.logState(providerID, b, "open")
	}
	r.mu.Unlock()
	if banNow {
		r.persistBan(providerID, count)
	}
}

// routeShared 只读内存名单。多个请求可以同时选。
func (r *Registry) routeShared(ps []Provider, exclude map[uint64]bool) *Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	filtered := make([]Provider, 0, len(ps))
	for _, p := range ps {
		if exclude[p.ID] {
			continue
		}
		if b, ok := r.bk[p.ID]; ok {
			if b.banned || (b.state == breakerOpen && !now.After(b.nextClosedAt)) {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return nil
	}
	return pickWeighted(filtered)
}

// WarmFailures 启动时看一次 Redis。已经满 20 次的上游先停用。每个请求不再为这个去抢独占锁。
func (r *Registry) WarmFailures() {
	if r.failRedis == nil {
		return
	}
	r.warmOnce.Do(r.scanBannedFailures)
}

func (r *Registry) scanBannedFailures() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var cursor uint64
	for {
		keys, next, err := r.failRedis.Scan(ctx, cursor, "router:fail:*", 200).Result()
		if err != nil {
			r.log.Error("scan provider failures", "err", err)
			return
		}
		for _, key := range keys {
			id, ok := parseFailureKey(key)
			if !ok {
				continue
			}
			n, err := r.failRedis.Get(ctx, key).Int()
			if err != nil || n < r.AutobanAfter {
				continue
			}
			if r.markBanned(id, n) {
				r.persistBan(id, n)
			}
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

func parseFailureKey(key string) (uint64, bool) {
	const prefix = "router:fail:"
	if !strings.HasPrefix(key, prefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(key[len(prefix):], 10, 64)
	return id, err == nil
}

func (r *Registry) markBanned(providerID uint64, count int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.breaker(providerID)
	b.consecutiveFail = count
	if b.banned {
		return false
	}
	b.banned = true
	b.state = breakerOpen
	r.clearCache()
	r.logState(providerID, b, "ban")
	return true
}

func (r *Registry) readFailWindow(providerID uint64) (int, time.Duration, bool) {
	if r.failRedis == nil {
		return 0, 0, false
	}
	ctx, cancel := r.failCtx()
	defer cancel()
	key := failureKey(providerID)
	n, err := r.failRedis.Get(ctx, key).Int()
	if err == goredis.Nil {
		return 0, 0, true
	}
	if err != nil {
		r.log.Error("read provider failures", "provider_id", providerID, "err", err)
		return 0, 0, false
	}
	ttl, err := r.failRedis.PTTL(ctx, key).Result()
	if err != nil {
		r.log.Error("read provider failure ttl", "provider_id", providerID, "err", err)
		return n, 0, false
	}
	if ttl < 0 {
		ttl = r.failWindow()
	}
	return n, ttl, true
}

func (r *Registry) addWindowFailure(providerID uint64) (int, time.Duration, bool) {
	if r.failRedis == nil {
		return 0, 0, false
	}
	ctx, cancel := r.failCtx()
	defer cancel()
	vals, err := windowIncr.Run(ctx, r.failRedis, []string{failureKey(providerID)}, r.failWindow().Milliseconds()).Slice()
	if err != nil {
		r.log.Error("record provider failure", "provider_id", providerID, "err", err)
		return 0, 0, false
	}
	if len(vals) != 2 {
		r.log.Error("record provider failure", "provider_id", providerID, "err", "bad script result")
		return 0, 0, false
	}
	count, okCount := scriptInt(vals[0])
	ttlMs, okTTL := scriptInt(vals[1])
	if !okCount || !okTTL {
		r.log.Error("record provider failure", "provider_id", providerID, "err", "bad script result")
		return 0, 0, false
	}
	if ttlMs < 0 {
		ttlMs = r.failWindow().Milliseconds()
	}
	return int(count), time.Duration(ttlMs) * time.Millisecond, true
}

func scriptInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (r *Registry) failWindow() time.Duration {
	if r.FailWindow <= 0 {
		return defaultFailWindow
	}
	return r.FailWindow
}

func (r *Registry) failCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Second)
}

// available 按熔断状态过滤候选集。这里只判断「放不放行」，真正的探测占位在 Route 选中后
// 由 markProbe 完成——否则候选里有但没被选中，探测位就永远卡死。
func (r *Registry) available(ps []Provider) []Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.availableLocked(ps)
}

// availableLocked 同 available，调用方必须已持有 r.mu。
func (r *Registry) availableLocked(ps []Provider) []Provider {
	out := make([]Provider, 0, len(ps))
	now := time.Now()
	for _, p := range ps {
		b := r.breaker(p.ID)
		switch b.state {
		case breakerOpen:
			if b.banned {
				continue
			}
			if now.After(b.nextClosedAt) {
				// 无 Redis：OPEN 到期 → 进 HALF-OPEN，放一个探测
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

// persistBan 连续失败达到停用阈值后，把 MySQL 里的上游标成停用。
// 调用方已经放开路由锁。写失败只记日志，内存里的暂停仍然有效。
func (r *Registry) persistBan(providerID uint64, fails int) {
	if r.db == nil {
		return
	}
	if err := r.db.Table("providers").
		Where("id = ? AND status = ?", providerID, 0).
		Updates(map[string]any{"status": 2, "consecutive_fail": fails}).Error; err != nil {
		r.log.Error("autoban persist", "provider_id", providerID, "err", err)
	}
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
	Status          int8   `json:"status"` // 0=active 1=disabled 2=banned 3=deleted
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
	if err := r.db.Table("providers").Where("status <> ?", 3).Order("id").Find(&rows).Error; err != nil {
		r.log.Error("providers query", "err", err)
		return nil
	}
	ids := make([]uint64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	redisFails := r.redisFailCounts(ids)

	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProviderState, 0, len(rows))
	for _, rr := range rows {
		ps := ProviderState{
			ID: rr.ID, Name: rr.Name, BaseURL: rr.BaseURL, Models: rr.Models,
			Weight: rr.Weight, Status: rr.Status, ConsecutiveFail: rr.ConsecutiveFail,
			BreakerState: "closed",
		}
		if r.failRedis != nil {
			ps.ConsecutiveFail = 0
			if n, ok := redisFails[rr.ID]; ok {
				ps.ConsecutiveFail = n
			}
		}
		if b, ok := r.bk[rr.ID]; ok {
			if r.failRedis == nil {
				ps.ConsecutiveFail = b.consecutiveFail
			}
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

func (r *Registry) redisFailCounts(ids []uint64) map[uint64]int {
	if r.failRedis == nil || len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = failureKey(id)
	}
	ctx, cancel := r.failCtx()
	defer cancel()
	vals, err := r.failRedis.MGet(ctx, keys...).Result()
	if err != nil {
		r.log.Error("read provider failures", "err", err)
		return nil
	}
	out := make(map[uint64]int, len(ids))
	for i, val := range vals {
		s, ok := val.(string)
		if !ok || s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		out[ids[i]] = n
	}
	return out
}

// ---------- snapshot（M2 原逻辑） ----------

// activeOnly 隔 ActiveRecheck 核对一次缓存里的渠道是否还启用。平时选路只看内存名单。
func (r *Registry) activeOnly(model string, ps []Provider) ([]Provider, error) {
	if r.db == nil || len(ps) == 0 {
		return ps, nil
	}
	r.mu.RLock()
	skip := r.ActiveRecheck > 0 && time.Since(r.activeChecked) < r.ActiveRecheck
	r.mu.RUnlock()
	if skip {
		return ps, nil
	}
	ids := make([]uint64, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	var active []uint64
	if err := r.db.Table("providers").Where("status = ? AND id IN ?", 0, ids).Pluck("id", &active).Error; err != nil {
		return nil, err
	}
	allowed := make(map[uint64]bool, len(active))
	for _, id := range active {
		allowed[id] = true
	}
	fresh := make([]Provider, 0, len(ps))
	for _, p := range ps {
		if allowed[p.ID] {
			fresh = append(fresh, p)
		}
	}
	r.mu.Lock()
	if c, ok := r.cache[model]; ok {
		c.providers = fresh
	}
	r.activeChecked = time.Now()
	r.mu.Unlock()
	return fresh, nil
}

func (r *Registry) snapshot(model string) ([]Provider, error) {
	r.mu.RLock()
	if c, ok := r.cache[model]; ok && time.Now().Before(c.expireAt) {
		ps := c.providers
		r.mu.RUnlock()
		return ps, nil
	}
	r.mu.RUnlock()

	ps, err := r.loadProviders(model)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[model] = &cachedRoute{providers: ps, expireAt: time.Now().Add(r.ttl)}
	r.mu.Unlock()
	return ps, nil
}

func (r *Registry) loadProviders(model string) ([]Provider, error) {
	if r.db == nil {
		return nil, nil
	}
	type row struct {
		MessagesBaseURL  string
		ResponsesBaseURL string
		ID               uint64
		Name             string
		BaseURL          string
		UpstreamKey      string
		Weight           int
		Models           string // JSON 数组
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
			ps = append(ps, Provider{ID: rr.ID, Name: rr.Name, BaseURL: rr.BaseURL, MessagesBaseURL: rr.MessagesBaseURL, ResponsesBaseURL: rr.ResponsesBaseURL, UpstreamKey: rr.UpstreamKey, Weight: rr.Weight})
		}
	}
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
		if p.Weight > 0 && p.Weight <= 1000000 {
			total += p.Weight
		}
	}
	if total <= 0 {
		return nil
	}
	n := rand.Intn(total)
	for i := range ps {
		if ps[i].Weight <= 0 || ps[i].Weight > 1000000 {
			continue
		}
		n -= ps[i].Weight
		if n < 0 {
			return &ps[i]
		}
	}
	return nil
}
