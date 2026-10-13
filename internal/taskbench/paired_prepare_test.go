package taskbench

import "testing"

func TestPrepareSmallPairPreservesFrozenRules(t *testing.T) {
	s, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := PrepareSmallPair(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Suite.Cases) != 12 || len(p.Standards) != 12 || p.ReadyForPaidRun || p.ModelCalls != 0 {
		t.Fatalf("unexpected plan: %+v", p)
	}
	for _, c := range p.Suite.Cases {
		if !c.Scenario.FullTaskMetricEligible || len(c.Assertions) == 0 {
			t.Fatal("expected answer rules lost")
		}
	}
	p.Suite.Cases[0].Scenario.Turns[0].User = "tampered"
	if err := ValidateSmallPair(s, p); err == nil {
		t.Fatal("changed frozen case accepted")
	}
}
