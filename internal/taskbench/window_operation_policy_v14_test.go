package taskbench

import (
	"testing"
	"time"
)

func TestWindowOperationPolicyV14KeepsLegacyDeadlines(t *testing.T) {
	for epoch := 1; epoch <= 13; epoch++ {
		got, err := windowOperationLimitForEpoch(epoch)
		if err != nil || got != 300*time.Second {
			t.Fatalf("legacy epoch %d: limit=%s error=%v", epoch, got, err)
		}
	}
	got, err := windowOperationLimitForEpoch(14)
	if err != nil || got != 600*time.Second {
		t.Fatalf("candidate epoch14: limit=%s error=%v", got, err)
	}
	for _, epoch := range []int{-1, 0, 15, 100} {
		if got, err := windowOperationLimitForEpoch(epoch); err == nil || got != 0 {
			t.Fatalf("unknown epoch %d accepted: %s %v", epoch, got, err)
		}
	}
}

func TestWindowOperationPolicyV14RejectsOtherLimits(t *testing.T) {
	policy := expectedWindowV14OperationPolicy()
	if err := policy.validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*windowV14OperationPolicy){
		"old-schema":       func(p *windowV14OperationPolicy) { p.Schema = "full-window-operation-policy.v13" },
		"legacy-window":    func(p *windowV14OperationPolicy) { p.WindowSeconds = 300 },
		"unbounded-window": func(p *windowV14OperationPolicy) { p.WindowSeconds = 0 },
		"larger-model":     func(p *windowV14OperationPolicy) { p.ModelSeconds = 91 },
		"larger-jev":       func(p *windowV14OperationPolicy) { p.JEVSeconds = 11 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := policy
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("unfrozen timing policy accepted")
			}
		})
	}
}
