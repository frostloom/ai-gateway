package taskbench

import "testing"

// Consumed v4 scope remains readable but cannot invoke the changed v5 Handler.
func TestSupplementRechargeLocalRunner(t *testing.T) {
	testRechargeScopeLocalRunner(t, supplementRechargeInputs, "authorized-recharge-supplement.v1")
}
