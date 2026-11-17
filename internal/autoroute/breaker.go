// autoroute JEV 熔断器：closed/open/half_open 三态计数熔断。closed 下连续失败达阈值
// 进入 open 并冷却；冷却期满放行一个探测（half_open），探测成功回 closed、失败重新
// open 重计冷却。全部时间由调用方以 now 参数传入，无墙钟依赖；mutex 保护内部状态。
package autoroute

import (
	"sync"
	"time"
)

// 熔断器内部相位。
type breakerPhase int

const (
	breakerClosed   breakerPhase = iota // 正常放行，统计连续失败
	breakerOpen                         // 熔断，冷却期内拒绝全部判定
	breakerHalfOpen                     // 已放行一个探测，等待其成败
)

// String 相位的对外字符串表示（State 返回值）。
func (p breakerPhase) String() string {
	switch p {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// Breaker JEV 判定熔断器。Allow 为判定入口的放行门：closed 恒 true；open 冷却未到
// false、冷却已到则放行一个探测并转 half_open（期间后续 Allow false 直到 Record 成败）。
// RecordSuccess 无条件归零计数并回 closed；RecordFailure 在 half_open 下直接重新 open
// 重计冷却，其余相位累计连续失败、达阈值 trip。零值不可用，须经 NewBreaker 构造；
// 全部方法并发安全。
type Breaker struct {
	mu        sync.Mutex
	threshold int           // 连续失败熔断阈值
	cooldown  time.Duration // 熔断冷却时长
	phase     breakerPhase
	failures  int       // closed 相位连续失败计数（trip 时清零，重新起算）
	opensAt   time.Time // open 相位冷却截止时刻
}

// NewBreaker 构造熔断器：连续 failures 次失败熔断，冷却 cooldown 后放行探测。
func NewBreaker(failures int, cooldown time.Duration) *Breaker {
	return &Breaker{threshold: failures, cooldown: cooldown, phase: breakerClosed}
}

// Allow 判定当前是否放行一次 JEV 调用。冷却已到（now >= opensAt）的首个调用
// 消耗探测名额并转 half_open，其后同相位调用一律 false，直至 RecordSuccess/RecordFailure。
func (b *Breaker) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.phase {
	case breakerClosed:
		return true
	case breakerOpen:
		if now.Before(b.opensAt) {
			return false
		}
		b.phase = breakerHalfOpen
		return true
	default:
		return false
	}
}

// RecordSuccess 记录一次判定成功：无条件回 closed 并清零连续失败计数。
func (b *Breaker) RecordSuccess(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.phase = breakerClosed
	b.failures = 0
}

// RecordFailure 记录一次判定失败：half_open 下立即重新 open 并以 now 重计冷却；
// 其余相位累计连续失败，达阈值时 open（opensAt = now + cooldown）并清零计数。
func (b *Breaker) RecordFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.phase == breakerHalfOpen {
		b.trip(now)
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.trip(now)
	}
}

// trip 进入 open：冷却截止为 now + cooldown，失败计数清零重新起算。
func (b *Breaker) trip(now time.Time) {
	b.phase = breakerOpen
	b.opensAt = now.Add(b.cooldown)
	b.failures = 0
}

// State 返回当前相位："closed" | "open" | "half_open"。
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.phase.String()
}
