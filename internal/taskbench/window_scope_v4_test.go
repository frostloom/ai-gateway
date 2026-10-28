package taskbench

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

func windowV4TestCaps(t *testing.T) []fullWindowCaps {
	t.Helper()
	raw, e := os.ReadFile("../../eval/paired-pilots/2026-10-29-cache-price/declaration.json")
	if e != nil {
		t.Fatal(e)
	}
	var d struct {
		Caps []fullWindowCaps `json:"caps"`
	}
	if e = json.Unmarshal(raw, &d); e != nil {
		t.Fatal(e)
	}
	return d.Caps
}
func TestFullWindowScopeV4KeepsOriginalFullSchedule(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := windowV4TestCaps(t)
	legacy, e := prepareFullWindowScope(s, p, 28, caps, b)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := prepareFullWindowScopeV4(s, p, 28, caps, b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
	if e != nil {
		t.Fatal("scope4 rejected", e)
	}
	sha, total, e := validateFullWindowScope(scope, s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	if !comparisonDigest(sha) || scope.Schema != "full-window-scope.v4" || total.Pairs != 3660 || total.Arms != 7320 || total.DefinedUserTurns != 11772 || total.Boundaries != 1851 || total.ModelCap != 64000000 || total.JEVCap != 690000 || !reflect.DeepEqual(scope.Slots, legacy.Slots) || !reflect.DeepEqual(scope.Windows, legacy.Windows) || scope.AllowPaid || scope.ReadyForPaidRun {
		t.Fatal("scope/order/budget changed", total)
	}
}

func TestFullWindowScopeV4FixedCapsAndRisk(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	for _, mode := range []string{"width", "missing", "model-plus", "jev-plus", "redistribution", "overflow", "hold", "baseline", "approval"} {
		t.Run(mode, func(t *testing.T) {
			caps := windowV4TestCaps(t)
			width := 28
			baseline, approval := windowV4BaselineSHA, windowBudget105ApprovalSHA
			switch mode {
			case "width":
				width = 27
			case "missing":
				caps = caps[:130]
			case "model-plus":
				caps[0].ModelCap++
			case "jev-plus":
				caps[0].JEVCap++
			case "redistribution":
				caps[0].ModelCap++
				caps[1].ModelCap--
			case "overflow":
				caps[0].ModelCap = 1<<63 - 1
			case "hold":
				caps[0].ModelCap = 74751
			case "baseline":
				baseline = ""
			case "approval":
				approval = ""
			}
			if _, err := prepareFullWindowScopeV4(s, p, width, caps, b, baseline, approval); err == nil {
				t.Fatal("bad fixed scope accepted")
			}
		})
	}
	base := expectedStoppedV3WindowBaseline()
	if base.PermanentModel+64000000+windowV3RiskModel != 104911594 || base.PermanentJEV+690000+windowV3RiskJEV != 998068 {
		t.Fatal("risk or parent changed")
	}
}
func TestFullWindowScopeV4RejectsCoherentMutation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	scope, e := prepareFullWindowScopeV4(s, p, 28, windowV4TestCaps(t), b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"v2", "approval", "baseline", "schedule", "maxcalls", "paid", "ready"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(scope)
			var bad fullWindowScope
			json.Unmarshal(raw, &bad)
			switch mode {
			case "v3":
				bad.Schema = "full-window-scope.v3"
			case "v2":
				bad.Schema = "full-window-scope.v2"
			case "approval":
				bad.BudgetApprovalSHA256 = ""
			case "baseline":
				bad.BaselineEvidenceSHA256 = ""
			case "schedule":
				bad.Slots[0], bad.Slots[1] = bad.Slots[1], bad.Slots[0]
				raw, _ := json.Marshal(bad.Slots)
				bad.ScheduleSHA256 = conversationHash(raw)
			case "maxcalls":
				bad.Windows[0].MaxModelCalls++
			case "paid":
				bad.AllowPaid = true
			case "ready":
				bad.ReadyForPaidRun = true
			}
			if _, _, e := validateFullWindowScope(bad, s, p, b); e == nil {
				t.Fatal("changed scope accepted")
			}
		})
	}
}

func TestFullWindowScopeV4CapsAreOriginalLargestRemainder(t *testing.T) {
	raw, e := os.ReadFile("../../eval/paired-pilots/2026-10-29-cache-price/declaration.json")
	if e != nil {
		t.Fatal(e)
	}
	var d struct {
		Kind     string           `json:"kind"`
		Paid     bool             `json:"allow_paid"`
		Ready    bool             `json:"ready_for_paid_run"`
		Human    bool             `json:"human_execution_approved"`
		Original []fullWindowCaps `json:"original_caps"`
		Caps     []fullWindowCaps `json:"caps"`
	}
	if json.Unmarshal(raw, &d) != nil || d.Kind != "non_executable_offline_declaration" || d.Paid || d.Ready || d.Human || len(d.Original) != 131 {
		t.Fatal("candidate authority changed")
	}
	distribute := func(values []int64, total int64) []int64 {
		var sum, allocated int64
		for _, v := range values {
			sum += v
		}
		out := make([]int64, len(values))
		type fraction struct {
			index     int
			remainder int64
		}
		fractions := make([]fraction, len(values))
		for i, v := range values {
			out[i] = total * v / sum
			allocated += out[i]
			fractions[i] = fraction{i, total * v % sum}
		}
		sort.Slice(fractions, func(i, j int) bool {
			if fractions[i].remainder == fractions[j].remainder {
				return fractions[i].index < fractions[j].index
			}
			return fractions[i].remainder > fractions[j].remainder
		})
		for i := int64(0); i < total-allocated; i++ {
			out[fractions[i].index]++
		}
		return out
	}
	var model, jev []int64
	var oldModel, oldJEV int64
	for _, c := range d.Original {
		model = append(model, c.ModelCap)
		jev = append(jev, c.JEVCap)
		oldModel += c.ModelCap
		oldJEV += c.JEVCap
	}
	if oldModel != 68421662 || oldJEV != 705028 {
		t.Fatal("original caps changed", oldModel, oldJEV)
	}
	model = distribute(model, 64000000)
	jev = distribute(jev, 690000)
	for i, c := range d.Caps {
		if c.ModelCap != model[i] || c.JEVCap != jev[i] {
			t.Fatal("allocation/tie order", i)
		}
	}
	encoded, _ := json.Marshal(d.Caps)
	if conversationHash(encoded) != windowV4CapsSHA {
		t.Fatal("caps canonicalSHA")
	}
}
