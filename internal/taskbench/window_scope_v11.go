package taskbench

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

// This fixed original inventory is DATA, never an execution or source proof.
const windowV11BaselineSHA = "a50e6863d5de3a1479f41173052176e83262092e618c3297bfdc6ae59949a517"

// RunID is an independent frozen nonce. No settings file contains ScopeSHA.
type windowObservationDeclarationV11 struct {
	Schema                 string `json:"schema"`
	PrivateSettingsSHA256  string `json:"private_settings_sha256"`
	SpoolRoot              string `json:"spool_root"`
	SpoolRunID             string `json:"spool_run_id"`
	SpoolMaxBytes          int64  `json:"spool_max_bytes"`
	Boundary               string `json:"observation_receipt_boundary"`
	ObserverIdentitySHA256 string `json:"observer_identity_sha256"`
}

func (d *windowObservationDeclarationV11) validate() error {
	if d == nil || d.Schema != "full-window-observation-declaration.v11" || !comparisonDigest(d.PrivateSettingsSHA256) || !filepath.IsAbs(d.SpoolRoot) || filepath.Clean(d.SpoolRoot) != d.SpoolRoot || !comparisonDigest(d.SpoolRunID) || d.SpoolMaxBytes != 4<<30 || d.Boundary != "durable_local" || !comparisonDigest(d.ObserverIdentitySHA256) {
		return fmt.Errorf("v11 scope observation identity required")
	}
	return nil
}

func expectedWindowV11BudgetDeclaration() windowV8BudgetDeclaration {
	d := expectedWindowV9BudgetDeclaration()
	d.BaselineSHA256 = windowV11BaselineSHA
	d.Parent = windowV8Amounts{195000000, 3000000}
	d.Historical = windowV8Amounts{66825427, 638655}
	d.MaximumReserved = windowV8Amounts{194170923, 2729954}
	d.Remaining = windowV8Amounts{829077, 270046}
	return d
}
func prepareFullWindowScopeV11(s ConversationSuite, p PairedProfiles, caps []fullWindowCaps, b fullWindowBindings, baselineSHA string, observation *windowObservationDeclarationV11) (fullWindowScope, error) {
	if baselineSHA != windowV11BaselineSHA || observation.validate() != nil {
		return fullWindowScope{}, fmt.Errorf("v11 concrete104 required")
	}
	out, e := prepareFullWindowScopeV9(s, p, caps, b, windowV9BaselineSHA)
	if e != nil {
		return out, e
	}
	out.Schema = "full-window-scope.v11"
	selected := *observation
	out.Observation = &selected
	out.BaselineEvidenceSHA256 = baselineSHA
	d := expectedWindowV11BudgetDeclaration()
	out.BudgetDeclaration = &d
	out.BudgetDeclarationSHA256 = windowV8DeclarationSHA(d)
	return out, validateWindowV11ExecutionShape(out)
}
func validateWindowV11Declarations(s fullWindowScope) error {
	if e := s.Observation.validate(); e != nil {
		return e
	}
	d, r := expectedWindowV11BudgetDeclaration(), expectedWindowV8RetryDeclaration()
	if s.CompletionV13 != nil || s.CollectionPolicySHA256 != "" || s.CompletionV12 != nil || s.Schema != "full-window-scope.v11" || s.Completion != nil || s.AllowPaid || s.ReadyForPaidRun || s.BudgetApprovalSHA256 != "" || s.BaselineEvidenceSHA256 != windowV11BaselineSHA || !reflect.DeepEqual(s.BudgetDeclaration, &d) || !reflect.DeepEqual(s.RetryDeclaration, &r) || s.BudgetDeclarationSHA256 != windowV8DeclarationSHA(d) || s.RetryPolicySHA256 != windowV8DeclarationSHA(r) {
		return fmt.Errorf("v11 full primary candidate declarations required")
	}
	return nil
}
func validateWindowV11ExecutionShape(s fullWindowScope) error {
	if e := validateWindowV11Declarations(s); e != nil {
		return e
	}
	raw, e := json.Marshal(s.Slots)
	if e != nil || len(s.Slots) != 3660 || conversationHash(raw) != windowV10ScheduleSHA || s.ScheduleSHA256 != windowV10ScheduleSHA || s.Width != 12 || !s.OddPassFirstArmImbalance {
		return fmt.Errorf("v11 original full slots required")
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: windowV8DeclarationSHA(s), ScheduleSHA256: s.ScheduleSHA256, PolicySHA256: s.Bindings.PolicySHA256, Windows: s.Windows, ModelTotal: 123827032, JEVTotal: 2037474}
	if e := validateV11ClaimDeclaration(d); e != nil {
		return e
	}
	// Reconstruct each original slot/turn partition, including the24-turn bound.
	at := 0
	for _, w := range s.Windows {
		turns := 0
		first := at
		for at < len(s.Slots) && at-first < 12 && turns+2*s.Slots[at].UserTurns <= 24 {
			if s.Slots[at].WindowID != w.ID {
				return fmt.Errorf("v11 window/slot mismatch")
			}
			turns += 2 * s.Slots[at].UserTurns
			at++
		}
		if w.FirstOrdinal != first+1 || w.LastOrdinal != at || w.MaxModelCalls != turns*5 || w.MaxJEVCalls != turns/2 {
			return fmt.Errorf("v11 original partition required")
		}
	}
	if at != 3660 {
		return fmt.Errorf("v11 incomplete slots")
	}
	return nil
}
