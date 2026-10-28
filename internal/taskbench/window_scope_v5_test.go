package taskbench

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func windowV5TestCaps(t *testing.T) []fullWindowCaps {
	t.Helper()
	base := windowV4TestCaps(t)
	raw, _ := json.Marshal(base)
	if conversationHash(raw) != windowV4CapsSHA {
		t.Fatal("originalP4capschanged")
	}
	allocate := func(values []int64, total int64) []int64 {
		var sum, assigned int64
		for _, v := range values {
			sum += v
		}
		out := make([]int64, len(values))
		type fraction struct {
			i int
			r int64
		}
		fractions := make([]fraction, len(values))
		for i, v := range values {
			out[i] = v * total / sum
			assigned += out[i]
			fractions[i] = fraction{i, v * total % sum}
		}
		sort.Slice(fractions, func(i, j int) bool {
			if fractions[i].r == fractions[j].r {
				return fractions[i].i < fractions[j].i
			}
			return fractions[i].r > fractions[j].r
		})
		for i := int64(0); i < total-assigned; i++ {
			out[fractions[i].i]++
		}
		return out
	}
	var model, jev []int64
	for _, c := range base {
		model = append(model, c.ModelCap)
		jev = append(jev, c.JEVCap)
	}
	model = allocate(model, 62000000)
	jev = allocate(jev, 676000)
	result := make([]fullWindowCaps, len(base))
	for i := range result {
		result[i] = fullWindowCaps{model[i], jev[i]}
	}
	raw, _ = json.Marshal(result)
	if conversationHash(raw) != windowV5CapsSHA {
		t.Fatal("v5largest-remainder-vector")
	}
	return result
}
func TestFullWindowScopeV5PreservesAllScheduleAndExactBudget(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := windowV5TestCaps(t)
	old, err := prepareFullWindowScope(s, p, 28, caps, b)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := prepareFullWindowScopeV5(s, p, 28, caps, b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal("v5fixeddeclaration", err)
	}
	sha, total, err := validateFullWindowScope(scope, s, p, b)
	if err != nil {
		t.Fatal(err)
	}
	if !comparisonDigest(sha) || scope.Schema != "full-window-scope.v5" || total.Pairs != 3660 || total.Arms != 7320 || total.DefinedUserTurns != 11772 || total.Boundaries != 1851 || total.ModelCap != 62000000 || total.JEVCap != 676000 || !reflect.DeepEqual(scope.Slots, old.Slots) || !reflect.DeepEqual(scope.Windows, old.Windows) || scope.AllowPaid || scope.ReadyForPaidRun {
		t.Fatal("originalscopechanged", total)
	}
}

func TestFullWindowScopeV5CapsRiskApprovalAndOldEpochReject(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	for _, mode := range []string{"width", "missing-window", "model-plus", "jev-plus", "same-total-redistribution", "model-overflow", "jev-overflow", "model-hold", "jev-hold", "old25-proof", "approval"} {
		t.Run(mode, func(t *testing.T) {
			caps := windowV5TestCaps(t)
			width := 28
			baseline, approval := windowV5BaselineSHA, windowBudget105ApprovalSHA
			switch mode {
			case "width":
				width = 27
			case "missing-window":
				caps = caps[:130]
			case "model-plus":
				caps[0].ModelCap++
			case "jev-plus":
				caps[0].JEVCap++
			case "same-total-redistribution":
				caps[0].ModelCap++
				caps[1].ModelCap--
			case "model-overflow":
				caps[0].ModelCap = 1<<63 - 1
			case "jev-overflow":
				caps[0].JEVCap = 1<<63 - 1
			case "model-hold":
				caps[0].ModelCap = 74751
			case "jev-hold":
				caps[0].JEVCap = 2687
			case "old25-proof":
				baseline = windowV4BaselineSHA
			case "approval":
				approval = ""
			}
			if _, err := prepareFullWindowScopeV5(s, p, width, caps, b, baseline, approval); err == nil {
				t.Fatal("baddeclarationaccepted")
			}
		})
	}
	base := expectedStoppedV4WindowBaseline()
	if base.PermanentModel+62000000+windowV3RiskModel != 104649315 || base.PermanentJEV+676000+windowV3RiskJEV != 999890 || base.TotalModel-104649315 != 350685 || base.TotalJEV-999890 != 110 {
		t.Fatal("parent/riskchanged")
	}
	scope, err := prepareFullWindowScopeV5(s, p, 28, windowV5TestCaps(t), b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"scope4", "paid", "ready", "swap", "maxcalls", "approval"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(scope)
			var bad fullWindowScope
			json.Unmarshal(raw, &bad)
			switch mode {
			case "scope4":
				bad.Schema = "full-window-scope.v4"
			case "paid":
				bad.AllowPaid = true
			case "ready":
				bad.ReadyForPaidRun = true
			case "swap":
				bad.Slots[0], bad.Slots[1] = bad.Slots[1], bad.Slots[0]
				raw, _ = json.Marshal(bad.Slots)
				bad.ScheduleSHA256 = conversationHash(raw)
			case "maxcalls":
				bad.Windows[0].MaxModelCalls++
			case "approval":
				bad.BudgetApprovalSHA256 = ""
			}
			if _, _, err := validateFullWindowScope(bad, s, p, b); err == nil {
				t.Fatal("coherentmixedscopeaccepted")
			}
		})
	}
}
