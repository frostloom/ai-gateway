package taskbench

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestWindowStoppedV15Exact249DataOnly(t *testing.T) {
	old := expectedStoppedV14WindowBaseline()
	b := expectedStoppedV15WindowBaseline()
	if len(b.ClaimSHA256) != 249 || b.PermanentModel != 103307765 || b.PermanentJEV != 1224400 || b.ActualModelCalls != 9184 || b.ActualJEVCalls != 2437 {
		t.Fatalf("wrong249: %+v", b)
	}
	for k, v := range old.ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("old178 changed")
		}
	}
	for i, h := range windowStoppedV15ClaimHashes {
		if b.ClaimSHA256[fmt.Sprintf("%s/window-%06d", windowStoppedV15ClaimPrefix, i+1)] != h {
			t.Fatal("new claim missing", i+1)
		}
	}
	b.ClaimSHA256["mutated"] = "bad"
	if len(expectedStoppedV15WindowBaseline().ClaimSHA256) != 249 || len(old.ClaimSHA256) != 178 {
		t.Fatal("aliased claim map")
	}
	if _, ok := any(windowStoppedV15Baseline{}).(windowStoppedHistory); ok {
		t.Fatal("DATA implements authority")
	}
	s := windowStoppedV14Baseline{base: expectedStoppedV14WindowBaseline()}
	s.previous.base = expectedStoppedV13WindowBaseline()
	f := freezeWindowStoppedV15Predecessor(s)
	s.base.ClaimSHA256["mutated"] = "bad"
	s.previous.base.ClaimSHA256["mutated"] = "bad"
	if reflect.DeepEqual(s, f) || f.base.ClaimSHA256["mutated"] != "" || f.previous.base.ClaimSHA256["mutated"] != "" {
		t.Fatal("predecessor alias")
	}
}

func TestWindowStoppedV15ContextBeforeIO(t *testing.T) {
	if _, err := loadWindowStoppedV15BaselineContext(nil, "/must-not-read", "bad"); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadWindowStoppedV15BaselineContext(ctx, "/must-not-read", "bad"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel identity lost", err)
	}
	if _, err := loadWindowStoppedV15BaselineContext(context.Background(), "/must-not-read", "bad"); err == nil {
		t.Fatal("unanchored descriptor accepted")
	}
}
