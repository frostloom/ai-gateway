package taskbench

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFullWindowScopeV3ReconstructsFullScheduleAndBudgetApproval(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{100, 1}
	}
	legacy, err := prepareFullWindowScope(s, p, 100, caps, b)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(legacy)
	var fields map[string]any
	json.Unmarshal(encoded, &fields)
	fields["schema"] = "full-window-scope.v3"
	fields["baseline_evidence_sha256"] = strings.Repeat("8", 64)
	fields["budget_approval_sha256"] = "d5062e6f39f9ae7c9a8461121f9ba907c798100d7a5428af72da459595d2d554"
	raw, _ := json.Marshal(fields)
	var v3 fullWindowScope
	json.Unmarshal(raw, &v3)
	h, totals, err := validateFullWindowScope(v3, s, p, b)
	if err != nil {
		t.Fatalf("approved v3 full scope rejected: %v", err)
	}
	if !comparisonDigest(h) || totals.Pairs != 3660 || totals.Arms != 7320 || totals.DefinedUserTurns != 11772 || totals.Boundaries != 1851 || !reflect.DeepEqual(v3.Slots, legacy.Slots) || !reflect.DeepEqual(v3.Windows, legacy.Windows) || v3.AllowPaid || v3.ReadyForPaidRun {
		t.Fatal("full task, order or authorization changed")
	}
}

func TestFullWindowScopeV3SeparateBudgetAndMarginBoundaries(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	const modelLimit int64 = 69580642 // 71604066 remaining - 2023424 margin.
	const jevLimit int64 = 740424     // 740489 remaining - 65 margin.
	for _, tc := range []struct {
		name       string
		model, jev int64
		accept     bool
	}{
		{"exact", modelLimit - 36, jevLimit - 36, true},
		{"model_plus_one", modelLimit - 35, 1, false},
		{"jev_plus_one", 1, jevLimit - 35, false},
		{"model_zero", 0, 1, false},
		{"jev_zero", 1, 0, false},
		{"model_negative", -1, 1, false},
		{"jev_negative", 1, -1, false},
		{"model_overflow", math.MaxInt64, 1, false},
		{"jev_overflow", 1, math.MaxInt64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caps := make([]fullWindowCaps, 37)
			for i := range caps {
				caps[i] = fullWindowCaps{1, 1}
			}
			caps[36] = fullWindowCaps{tc.model, tc.jev}
			v, err := prepareFullWindowScopeV3(s, p, 100, caps, b, strings.Repeat("8", 64), windowBudget105ApprovalSHA)
			if (err == nil) != tc.accept {
				t.Fatal("budget admission", err)
			}
			if tc.accept {
				if _, _, err = validateFullWindowScope(v, s, p, b); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFullWindowScopeV3RejectsCrossedApprovalVersionAndLegacyClaim(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{1, 1}
	}
	proof := strings.Repeat("8", 64)
	for _, sha := range []string{"", strings.Repeat("9", 64), strings.ToUpper(windowBudget105ApprovalSHA)} {
		if _, err := prepareFullWindowScopeV3(s, p, 100, caps, b, proof, sha); err == nil {
			t.Fatal("unapproved budget accepted")
		}
	}
	if _, err := prepareFullWindowScopeV3(s, p, 100, caps, b, "", windowBudget105ApprovalSHA); err == nil {
		t.Fatal("absent stopped evidence accepted")
	}
	v3, err := prepareFullWindowScopeV3(s, p, 100, caps, b, proof, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*fullWindowScope){
		func(v *fullWindowScope) { v.Schema = "full-window-scope.v1" },
		func(v *fullWindowScope) { v.Schema = "full-window-scope.v2" },
		func(v *fullWindowScope) { v.BudgetApprovalSHA256 = "" },
		func(v *fullWindowScope) { v.BudgetApprovalSHA256 = strings.Repeat("f", 64) },
		func(v *fullWindowScope) { v.Slots = v.Slots[1:] },
		func(v *fullWindowScope) { v.Slots[0], v.Slots[1] = v.Slots[1], v.Slots[0] },
		func(v *fullWindowScope) { v.AllowPaid = true },
	} {
		raw, _ := json.Marshal(v3)
		var bad fullWindowScope
		json.Unmarshal(raw, &bad)
		change(&bad)
		if _, _, err := validateFullWindowScope(bad, s, p, b); err == nil {
			t.Fatal("crossed or changed scope accepted")
		}
	}
	for _, version := range []string{"v1", "v2"} {
		old, err := prepareFullWindowScope(s, p, 100, caps, b)
		if version == "v2" {
			old, err = prepareFullWindowScopeV2(s, p, 100, caps, b, proof)
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(old)
		if strings.Contains(string(raw), "budget_approval_sha256") {
			t.Fatal("old JSON changed")
		}
		old.BudgetApprovalSHA256 = windowBudget105ApprovalSHA
		if _, _, err := validateFullWindowScope(old, s, p, b); err == nil {
			t.Fatal("old version accepted new approval field")
		}
	}
	root := filepath.Join(stoppedEvidenceRoot(t), "never-created-v3")
	if _, err := claimFirstFullWindow(root, "missing-history", v3, s, p, b, strings.Repeat("9", 64)); err == nil || !strings.Contains(err.Error(), "scope version") {
		t.Fatal("legacy entry did not reject v3", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("offline declaration created claim root")
	}
}
