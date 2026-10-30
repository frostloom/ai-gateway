package taskbench

import (
	"testing"
)

func TestWindowV9ReusesBoundedNativeAccounting(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range scope.Windows {
		c := contractWindowCampaign(scope, "scope", w, windowV4ExpectedLimits())
		if c.Schema != windowAccountingV3 || c.Model.MaxAttempts != w.MaxModelCalls+20 || c.JEV.MaxAttempts != w.MaxJEVCalls+20 {
			t.Fatalf("new scope lost original bounded native accounting: %s %d %d", c.Schema, c.Model.MaxAttempts, c.JEV.MaxAttempts)
		}
	}
}

func TestWindowV9KeepsNativeRuntimeEvidence(t *testing.T) {
	c := &windowCampaign{prepared: windowCampaignPrepared{scope: fullWindowScope{Schema: "full-window-scope.v9"}}, baseline: expectedStoppedV8WindowBaseline(), model: 52654504, jev: 408701}
	r := c.snapshotWindowV8Runtime(windowAccountingState{}, "test")
	if r == nil || r.Schema != "full-window-runtime-evidence.v8" || r.HistoricalPermanent != (windowV8Amounts{52654504, 408701}) {
		t.Fatalf("native evidence missing or historical amounts changed: %+v", r)
	}
}

func TestWindowV9KeepsObserverDeadline(t *testing.T) {
	if got := windowObserverTimeoutForScope("full-window-scope.v9"); got != 10000 {
		t.Fatalf("observer deadline changed: %d", got)
	}
}
