package taskbench

import "testing"

func TestCancelRechargeLocalRunner(t *testing.T) {
	testRechargeScopeLocalRunner(t, cancelRechargeInputs, "authorized-recharge-cancel.v1")
}
