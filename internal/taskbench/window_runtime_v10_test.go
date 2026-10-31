package taskbench

import (
	"context"
	"testing"
)

func TestWindowV10NativeDispatchAndDeadline(t *testing.T) {
	scope, _, _, _ := windowV10FixtureScope(t)
	if !windowScopeUsesNativeV8(scope.Schema) {
		t.Fatal("V10 missing bounded native dispatch")
	}
	if err := validateWindowNativeDeclarations(scope); err != nil {
		t.Fatal(err)
	}
	if got := windowNativeParent(scope); got != (windowV8Amounts{185000000, 3000000}) {
		t.Fatalf("wrong parent: %+v", got)
	}
	if got := windowObserverTimeoutForScope(scope.Schema); got != 10000 {
		t.Fatalf("wrong observer deadline: %d", got)
	}
	for _, w := range scope.Windows {
		c := contractWindowCampaign(scope, "scope", w, windowV4ExpectedLimits())
		if c.Schema != windowAccountingV3 || c.Model.MaxAttempts != w.MaxModelCalls+20 || c.JEV.MaxAttempts != w.MaxJEVCalls+20 {
			t.Fatal("bounded native accounting lost")
		}
	}
}
func TestWindowV10PreparationUnfrozenAndOldOwnerDenied(t *testing.T) {
	if _, p, err := checkFullWindowActualReadyEpoch(FullWindowActualReadyOptions{"/synthetic/not-read"}, 10); err == nil || p != nil {
		t.Fatal("unfrozen V10 accepted")
	}
	// This rejects before opening settings or making any directory.
	r := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v9", Scope: fullWindowScope{Schema: "full-window-scope.v9"}}}
	if s, err := OpenFullWindowActual(context.Background(), r, "/synthetic/do-not-read"); err == nil || s != nil {
		t.Fatal("consumed V9 opened")
	}
}

// Exercise the unchanged live cursor guard with full Slots and suffix Windows.
// No provider, fixture database, credential, or real historical loader is used.
func TestWindowV10OwnedCursorCannotExecutePrefix(t *testing.T) {
	prepared, base := fixtureWindowCampaign(t)
	c, err := startWindowCampaign(prepared, base)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	ledger, err := c.openWindowCampaignLedger()
	if err != nil {
		t.Fatal(err)
	}
	scope, _, _, _ := windowV10FixtureScope(t)
	c.prepared.scope = scope
	env := &PairedFixtureEnvironment{}
	permit := &windowAcquisitionPermit{campaign: c, ledger: ledger, executionSHA: c.prepared.executionSHA, windowIndex: 1, environment: env}
	d := &windowOwnedDriver{permit: permit, ledger: ledger, environment: env, slot: scope.Slots[0], arm: scope.Slots[0].ArmOrder[0]}
	permit.active = d
	if err := permit.guardWindowOwnedLocked(d); err == nil {
		t.Fatal("prefix original ordinal1 got suffix permit")
	}
	d.slot = scope.Slots[385]
	d.arm = d.slot.ArmOrder[0]
	if err := permit.guardWindowOwnedLocked(d); err != nil {
		t.Fatalf("local1 declaration56 ordinal386 rejected: %v", err)
	}
	c.index = 456
	permit.windowIndex = 456
	c.prepared.contracts = make([]windowAccountingContract, 456)
	c.prepared.contracts[455] = ledger.contract
	w := scope.Windows[455]
	d.slot = scope.Slots[w.FirstOrdinal-1]
	d.arm = d.slot.ArmOrder[0]
	if err := permit.guardWindowOwnedLocked(d); err != nil {
		t.Fatalf("final local456 declaration511 rejected: %v", err)
	}
	beforeModel, beforeJEV := c.model, c.jev
	if err := c.persistClaimWindowCampaignContext(context.Background(), 456, "", ""); err == nil {
		t.Fatal("claim457 created")
	}
	if c.model != beforeModel || c.jev != beforeJEV {
		t.Fatal("out of range claim charged cap")
	}
}

func TestWindowV10StatusCompletionIsDetached(t *testing.T) {
	completion := expectedWindowCompletionDeclarationV10()
	s := &FullWindowActualSession{status: FullWindowActualStatus{Completion: &completion}}
	out := s.Status()
	out.Completion.Primary.Pairs = 1
	out.Completion.PrefixWindowIDs[0] = "foreign"
	if s.status.Completion.Primary.Pairs != 3660 || s.status.Completion.PrefixWindowIDs[0] != "window-001" {
		t.Fatal("public status changed private completion")
	}
}
