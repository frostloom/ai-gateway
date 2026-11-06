package taskbench

import (
	"testing"
	"time"
)

// Missing successor dispatch must not silently classify the new scope as a
// legacy non-native execution or apply the old 300-second deadline.
func TestWindowV15DispatchKeepsApprovedPolicyAndNativeAccounting(t *testing.T) {
	op := expectedWindowV14OperationPolicy()
	scope := fullWindowScope{Schema: "full-window-scope.v15", OperationPolicyV14: &op}
	if d, err := windowOperationLimitForEpoch(15); err != nil || d != 600*time.Second {
		t.Fatalf("new15 must reuse approved600 policy: %v %v", d, err)
	}
	if !windowScopeUsesNativeV8(scope.Schema) {
		t.Fatal("new15 missing native accounting dispatch")
	}
	if parent := windowNativeParent(scope); parent != (windowV8Amounts{200000000, 3000000}) {
		t.Fatalf("new15 parent changed: %+v", parent)
	}
}
