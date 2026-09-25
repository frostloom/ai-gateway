package router

import (
	"log/slog"
	"testing"
	"time"
)

// 纯逻辑测试熔断状态机（不连 DB）。用 cooldown=30ms 让 HALF-OPEN 快速出现。
func newTestRegistry() *Registry {
	r := New(nil, slog.New(slog.DiscardHandler))
	r.Cooldown = 30 * time.Millisecond
	return r
}

func oneProvider() []Provider {
	return []Provider{{ID: 1, Name: "p1", BaseURL: "http://x", Weight: 1}}
}

func TestCircuitOpensAfterThreshold(t *testing.T) {
	r := newTestRegistry()
	ps := oneProvider()

	// 前 4 次失败：仍在候选集（<5）
	for i := 0; i < 4; i++ {
		r.Report(1, false)
		if got := r.available(ps); len(got) != 1 {
			t.Fatalf("第 %d 次失败后应仍可用，got %d", i+1, len(got))
		}
	}
	// 第 5 次失败 → OPEN，立即被排除
	r.Report(1, false)
	if got := r.available(ps); len(got) != 0 {
		t.Fatalf("OPEN 后应从候选集排除，got %d", len(got))
	}
}

func TestCircuitHalfOpenRecovers(t *testing.T) {
	r := newTestRegistry()
	ps := oneProvider()

	// 打到 OPEN
	for i := 0; i < 5; i++ {
		r.Report(1, false)
	}
	if got := r.available(ps); len(got) != 0 {
		t.Fatal("应 OPEN")
	}

	// 冷却结束 → HALF-OPEN，放 1 个探测
	time.Sleep(r.Cooldown + 10*time.Millisecond)
	if got := r.available(ps); len(got) != 1 {
		t.Fatalf("HALF-OPEN 应放 1 个探测，got %d", len(got))
	}

	// 探测成功 → 回 CLOSED，恢复全量可用
	r.Report(1, true)
	for i := 0; i < 10; i++ {
		r.Report(1, true)
	}
	if got := r.available(ps); len(got) != 1 {
		t.Fatalf("恢复后应可用，got %d", len(got))
	}
}

func TestCircuitHalfOpenProbeFailReopens(t *testing.T) {
	r := newTestRegistry()
	ps := oneProvider()

	for i := 0; i < 5; i++ {
		r.Report(1, false)
	}
	time.Sleep(r.Cooldown + 10*time.Millisecond)

	// 放出的探测请求失败 → 回 OPEN
	if got := r.available(ps); len(got) != 1 {
		t.Fatalf("应放探测，got %d", len(got))
	}
	r.Report(1, false)
	if got := r.available(ps); len(got) != 0 {
		t.Fatal("探测失败应回 OPEN")
	}
}

// 回归测试：available() 只判断放行，不消耗探测位——只有 Route 实际选中（markProbe）才占位。
// 旧实现「候选里有 HALF-OPEN 就立刻占位」，但加权随机不一定选中它 → 探测位永久卡死 → 静默下线。
func TestHalfOpenProbeOnlyConsumedOnPick(t *testing.T) {
	r := newTestRegistry()
	ps := []Provider{{ID: 1, Name: "p1", BaseURL: "http://x", Weight: 1}}

	for i := 0; i < 5; i++ {
		r.Report(1, false)
	}
	time.Sleep(r.Cooldown + 10*time.Millisecond)

	// available 连调两次：都仍放行（未选中前不占探测位）
	if got := r.available(ps); len(got) != 1 {
		t.Fatalf("第一次 available 应放行 HALF-OPEN，got %d", len(got))
	}
	if got := r.available(ps); len(got) != 1 {
		t.Fatalf("第二次 available 不应消耗探测位，got %d", len(got))
	}

	// 真正选中（探测发出）→ 占位 → 后续 available 不再放行
	r.markProbe(&ps[0])
	if got := r.available(ps); len(got) != 0 {
		t.Fatalf("探测在途不应再放行，got %d", len(got))
	}

	// 探测回报失败 → 释放探测位 + 回 OPEN
	r.Report(1, false)
	if got := r.available(ps); len(got) != 0 {
		t.Fatalf("探测失败应回 OPEN 被排除，got %d", len(got))
	}
}

func TestAutobanAfter20Fails(t *testing.T) {
	r := newTestRegistry()
	ps := oneProvider()

	for i := 0; i < 20; i++ {
		r.Report(1, false)
	}
	b := r.breaker(1)
	if b.state != breakerOpen {
		t.Fatalf("AutoBan 后应 OPEN，got %v", b.state)
	}
	// 被 ban 的 provider 冷却极长（24h），短期不可能自愈
	if time.Until(b.nextClosedAt) < time.Hour {
		t.Fatalf("AutoBan 冷却应 ≥1h，got %v", time.Until(b.nextClosedAt))
	}
	if got := r.available(ps); len(got) != 0 {
		t.Fatal("被 ban 应从候选集移除")
	}
}
