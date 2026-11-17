// autoroute JEV 熔断测试：closed 恒放行、连续失败达阈值进 open、冷却期满半开只放行一个
// 探测、半开成败分别回 closed / 重新 open 重计冷却、成功清零连续失败计数，
// 以及 -race 下 50 goroutine 混合调用三方法无数据竞态。全部用例传入递增 now，无真实 sleep。
package autoroute

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// breakerNow 以 Unix 纪元为基线的确定性时钟：now = step 秒，递增传参，无墙钟依赖。
func breakerNow(step int) time.Time {
	return time.Unix(int64(step), 0).UTC()
}

func TestBreakerClosedAllowsAlways(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	for step := 0; step < 5; step++ {
		if !b.Allow(breakerNow(step)) {
			t.Fatalf("closed 态第 %d 次 Allow 应放行", step+1)
		}
		if got := b.State(); got != "closed" {
			t.Fatalf("初始态应 closed，得到 %q", got)
		}
	}
}

func TestBreakerTripsAfterConsecutiveFailures(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	// 前两次失败未达阈值：仍 closed，Allow 恒放行。
	for step := 0; step < 2; step++ {
		b.RecordFailure(breakerNow(step))
		if got := b.State(); got != "closed" {
			t.Fatalf("%d 次失败后应仍 closed，得到 %q", step+1, got)
		}
		if !b.Allow(breakerNow(step)) {
			t.Fatalf("%d 次失败后 Allow 应仍放行", step+1)
		}
	}
	// 第 3 次连续失败达阈值：open，冷却期内拒绝（opensAt=2s+30s=32s）。
	b.RecordFailure(breakerNow(2))
	if got := b.State(); got != "open" {
		t.Fatalf("3 连败后应 open，得到 %q", got)
	}
	if b.Allow(breakerNow(2)) {
		t.Fatal("open 态冷却期内 Allow 应拒绝")
	}
	if b.Allow(breakerNow(31)) {
		t.Fatal("冷却未满（31s<32s）时 Allow 应拒绝")
	}
}

func TestBreakerHalfOpenAdmitsSingleProbe(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	for step := 0; step < 3; step++ {
		b.RecordFailure(breakerNow(step))
	}
	// 冷却期满（now>=opensAt=32s）：首个 Allow 进半开并放行一个探测。
	if !b.Allow(breakerNow(32)) {
		t.Fatal("冷却期满后首个 Allow 应放行探测")
	}
	if got := b.State(); got != "half_open" {
		t.Fatalf("探测放行后应 half_open，得到 %q", got)
	}
	// 半开期间后续 Allow 拒绝，直到 Record 成败。
	if b.Allow(breakerNow(33)) {
		t.Fatal("半开期间第二次 Allow 应拒绝")
	}
	if b.Allow(breakerNow(34)) {
		t.Fatal("半开期间第三次 Allow 应拒绝")
	}
}

func TestBreakerHalfOpenSuccessClosesAndResets(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	for step := 0; step < 3; step++ {
		b.RecordFailure(breakerNow(step))
	}
	if !b.Allow(breakerNow(32)) {
		t.Fatal("冷却期满后应放行探测")
	}
	b.RecordSuccess(breakerNow(33))
	if got := b.State(); got != "closed" {
		t.Fatalf("半开成功后应 closed，得到 %q", got)
	}
	// 计数已归零：再一次失败不触发熔断。
	b.RecordFailure(breakerNow(34))
	if got := b.State(); got != "closed" {
		t.Fatalf("成功后单次失败不应熔断，得到 %q", got)
	}
	if !b.Allow(breakerNow(35)) {
		t.Fatal("closed 态 Allow 应放行")
	}
}

func TestBreakerHalfOpenFailureReopensWithFreshCooldown(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	for step := 0; step < 3; step++ {
		b.RecordFailure(breakerNow(step))
	}
	if !b.Allow(breakerNow(32)) {
		t.Fatal("冷却期满后应放行探测")
	}
	// 半开失败：重新 open，冷却从失败时刻重计（opensAt=34s+30s=64s）。
	b.RecordFailure(breakerNow(34))
	if got := b.State(); got != "open" {
		t.Fatalf("半开失败后应重新 open，得到 %q", got)
	}
	if b.Allow(breakerNow(63)) {
		t.Fatal("新冷却未满（63s<64s）时 Allow 应拒绝")
	}
	if got := b.State(); got != "open" {
		t.Fatalf("重计冷却期内应保持 open，得到 %q", got)
	}
	if !b.Allow(breakerNow(64)) {
		t.Fatal("新冷却期满后应再次放行探测")
	}
	if got := b.State(); got != "half_open" {
		t.Fatalf("再次放行后应 half_open，得到 %q", got)
	}
}

func TestBreakerSuccessResetsConsecutiveFailures(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	b.RecordFailure(breakerNow(0))
	b.RecordFailure(breakerNow(1))
	b.RecordSuccess(breakerNow(2))
	// 计数清零后再两次失败仍不到阈值。
	b.RecordFailure(breakerNow(3))
	b.RecordFailure(breakerNow(4))
	if got := b.State(); got != "closed" {
		t.Fatalf("成功清零后 2 次失败不应熔断，得到 %q", got)
	}
	if !b.Allow(breakerNow(5)) {
		t.Fatal("closed 态 Allow 应放行")
	}
	// 自上次成功起第 3 次连续失败才达阈值熔断。
	b.RecordFailure(breakerNow(5))
	if got := b.State(); got != "open" {
		t.Fatalf("成功后再 3 连败应熔断，得到 %q", got)
	}
}

func TestBreakerConcurrentMixedCalls(t *testing.T) {
	b := NewBreaker(3, 30*time.Second)
	var wg sync.WaitGroup
	var calls atomic.Int64
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				n := int(calls.Add(1))
				now := breakerNow(n % 1000)
				switch n % 3 {
				case 0:
					b.Allow(now)
				case 1:
					b.RecordSuccess(now)
				default:
					b.RecordFailure(now)
				}
				_ = b.State()
			}
		}()
	}
	wg.Wait()
	switch got := b.State(); got {
	case "closed", "open", "half_open":
	default:
		t.Fatalf("并发结束后状态非法: %q", got)
	}
}
