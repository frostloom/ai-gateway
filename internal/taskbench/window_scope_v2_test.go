package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFullWindowScopeV2KeepsAllOriginalTasksAndBindsBaseline(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{100, 1}
	}
	legacy, err := prepareFullWindowScope(s, p, 100, caps, b)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	if _, ok := fields["baseline_evidence_sha256"]; ok {
		t.Fatal("v1 JSON changed")
	}
	proof := strings.Repeat("8", 64)
	v2, err := prepareFullWindowScopeV2(s, p, 100, caps, b, proof)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Schema != "full-window-scope.v2" || v2.BaselineEvidenceSHA256 != proof || !reflect.DeepEqual(v2.Slots, legacy.Slots) || !reflect.DeepEqual(v2.Windows, legacy.Windows) || v2.AllowPaid || v2.ReadyForPaidRun {
		t.Fatal("original scope or authorization changed")
	}
	h, totals, err := validateFullWindowScope(v2, s, p, b)
	if err != nil || totals.Pairs != 3660 || totals.Arms != 7320 || totals.DefinedUserTurns != 11772 || totals.Boundaries != 1851 {
		t.Fatal(h, totals, err)
	}
	oldHash, _, err := validateFullWindowScope(legacy, s, p, b)
	if err != nil || oldHash == h {
		t.Fatal("v2 identity collided with v1", err)
	}
	for _, mutate := range []func(*fullWindowScope){func(v *fullWindowScope) { v.BaselineEvidenceSHA256 = "" }, func(v *fullWindowScope) { v.Schema = "full-window-scope.v1" }, func(v *fullWindowScope) { v.Slots = v.Slots[1:] }} {
		bad := v2
		bad.Slots = append([]fullWindowSlot(nil), v2.Slots...)
		mutate(&bad)
		if _, _, err = validateFullWindowScope(bad, s, p, b); err == nil {
			t.Fatal("cross-version or incomplete scope accepted")
		}
	}
}

func TestFullWindowScopeV2RejectsOldLargerRemainingBudget(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			caps := make([]fullWindowCaps, 37)
			for i := range caps {
				caps[i] = fullWindowCaps{1, 1}
			}
			if provider == "model" {
				caps[36].ModelCap = 70524294 - 36 + 1
			} else {
				caps[36].JEVCap = 772817 - 36 + 1
			}
			if _, err := prepareFullWindowScope(s, p, 100, caps, b); err != nil {
				t.Fatal("fixture not within old remaining budget", err)
			}
			if _, err := prepareFullWindowScopeV2(s, p, 100, caps, b, strings.Repeat("8", 64)); err == nil {
				t.Fatal("new historical reservation ignored")
			}
		})
	}
	if _, err := prepareFullWindowScopeV2(s, p, 100, nil, b, ""); err == nil {
		t.Fatal("missing stopped proof accepted")
	}
}

func TestLegacyFirstClaimRejectsV2BeforeHistoryOrRoot(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{1, 1}
	}
	v2, err := prepareFullWindowScopeV2(s, p, 100, caps, b, strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(stoppedEvidenceRoot(t), "never-created")
	_, err = claimFirstFullWindow(root, "missing-history", v2, s, p, b, strings.Repeat("9", 64))
	if err == nil || !strings.Contains(err.Error(), "scope version") {
		t.Fatal("legacy entry did not reject v2 explicitly", err)
	}
	if _, err = os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("legacy entry created v2 root")
	}
}
