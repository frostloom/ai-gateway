package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowScopeInputs(t *testing.T) (ConversationSuite, PairedProfiles, fullWindowBindings) {
	t.Helper()
	s, e := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if e != nil {
		t.Fatal(e)
	}
	p, e := LoadPairedProfiles("../../eval/runtime-profiles/2026-10-13-six-profiles-v1")
	if e != nil {
		t.Fatal(e)
	}
	b := fullWindowBindings{PolicySHA256: strings.Repeat("a", 64), ExecutorRevision: strings.Repeat("b", 40), CustomerRevision: strings.Repeat("c", 40), PriceEvidenceSHA256: strings.Repeat("d", 64), RequestEnvelopeSHA256: strings.Repeat("e", 64)}

	return s, p, b
}
func TestFullWindowScopeFixedThreePasses(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{ModelCap: 100, JEVCap: 10}
	}
	scope, e := prepareFullWindowScope(s, p, 100, caps, b)
	if e != nil {
		t.Fatal(e)
	}
	_, tot, e := validateFullWindowScope(scope, s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	if tot.Pairs != 3660 || tot.Arms != 7320 || tot.DefinedUserTurns != 11772 || tot.Boundaries != 1851 || tot.ModelCap != 3700 || tot.JEVCap != 370 {
		t.Fatalf("totals %+v", tot)
	}
	primary, _, _ := fullCustomerSelection(s)
	seen := map[string]bool{}
	for i, slot := range scope.Slots {
		pass := i / 1220
		original := (i%1220 + 406*pass) % 1220
		want := [2]string{"model", "jev_selective"}
		if (original+pass)%2 == 1 {
			want = [2]string{"jev_selective", "model"}
		}
		if slot.Ordinal != i+1 || slot.Pass != pass+1 || slot.CaseKey != primary.Cases[original].Key() || slot.ArmOrder != want {
			t.Fatalf("slot %d %+v", i, slot)
		}
		key := string(rune(slot.Pass)) + slot.CaseKey
		if seen[key] {
			t.Fatal("duplicate")
		}
		seen[key] = true
	}
	if scope.ReadyForPaidRun || scope.AllowPaid || !scope.OddPassFirstArmImbalance {
		t.Fatal("authorization/disclosure")
	}
}

func TestFullWindowScopeRejectsRewrittenScheduleAndSources(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{1, 1}
	}
	original, e := prepareFullWindowScope(s, p, 100, caps, b)
	if e != nil {
		t.Fatal(e)
	}
	mutations := map[string]func(*fullWindowScope){
		"missing":   func(v *fullWindowScope) { v.Slots = v.Slots[1:] },
		"duplicate": func(v *fullWindowScope) { v.Slots[1] = v.Slots[0] },
		"boundary": func(v *fullWindowScope) {
			plan, _ := PrepareFullCustomerPair(s)
			v.Slots[0].CaseKey = plan.BoundaryKeys[0]
		},
		"pass":  func(v *fullWindowScope) { v.Slots[0].Pass = 2 },
		"order": func(v *fullWindowScope) { v.Slots[0].ArmOrder = [2]string{"jev_selective", "model"} },
		"coherent rotation": func(v *fullWindowScope) {
			v.Slots[0].CaseKey, v.Slots[1].CaseKey = v.Slots[1].CaseKey, v.Slots[0].CaseKey
		},
		"declaration": func(v *fullWindowScope) { v.Windows[0].MaxModelCalls++ },
		"metadata":    func(v *fullWindowScope) { v.Bindings.PolicySHA256 = strings.Repeat("f", 64) },
		"paid":        func(v *fullWindowScope) { v.ReadyForPaidRun = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var v fullWindowScope
			json.Unmarshal(raw, &v)
			mutate(&v)
			raw, _ = json.Marshal(v.Slots)
			v.ScheduleSHA256 = conversationHash(raw)
			if _, _, e := validateFullWindowScope(v, s, p, b); e == nil {
				t.Fatal("accepted rewrite")
			}
		})
	}
	s.Cases[0].Scenario.Turns[0].User += " changed"
	if _, _, e := validateFullWindowScope(original, s, p, b); e == nil {
		t.Fatal("accepted source retaining hash")
	}
	s, p, b = windowScopeInputs(t)
	p.archive.Accounts["active_flagship"] = runtimeAccount{}
	if _, _, e := validateFullWindowScope(original, s, p, b); e == nil {
		t.Fatal("accepted profile retaining hash")
	}
}
func TestFullWindowScopeCapsIndependentExactFit(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{1, 1}
	}
	caps[0] = fullWindowCaps{70938880 - 36, 776896 - 36}
	if _, e := prepareFullWindowScope(s, p, 100, caps, b); e != nil {
		t.Fatal(e)
	}
	for _, provider := range []string{"model", "jev"} {
		t.Run(provider, func(t *testing.T) {
			c := append([]fullWindowCaps(nil), caps...)
			if provider == "model" {
				c[0].ModelCap++
			} else {
				c[0].JEVCap++
			}
			if _, e := prepareFullWindowScope(s, p, 100, c, b); e == nil {
				t.Fatal("one unit over accepted")
			}
		})
	}
	for _, width := range []int{0, 101} {
		if _, e := prepareFullWindowScope(s, p, width, caps, b); e == nil {
			t.Fatal("invalid width")
		}
	}
	caps[0].ModelCap = 0
	if _, e := prepareFullWindowScope(s, p, 100, caps, b); e == nil {
		t.Fatal("zero cap")
	}
}
func TestFullWindowBaselineVerifiesRawChain(t *testing.T) {
	claims := map[string][]byte{"old": []byte(`{"old":true}`), "last": []byte(`{"schema":"paired-live-receiver-repeated-claim.v1","campaign_id":"v5-repeated-receiver-20261027","status":"consumed_before_execution","cumulative_reserved_model_micro_cny":29061120,"cumulative_reserved_jev_micro_usd":223104,"reserved_model_micro_cny":8380416,"reserved_jev_micro_usd":64512,"reserved_model_calls":248,"reserved_jev_calls":24}`)}
	expected := map[string]string{}
	for k, v := range claims {
		expected[k] = conversationHash(v)
	}
	base, e := verifyFullWindowBaselineBytes(claims, expected, "last")
	if e != nil {
		t.Fatal(e)
	}
	if base.RemainingModel != 70938880 || base.RemainingJEV != 776896 || base.ActualModelCalls != 143 || base.ActualJEVCalls != 37 {
		t.Fatalf("baseline %+v", base)
	}
	for _, mutation := range []string{"missing", "changed", "wrong schema", "wrong cumulative", "unconsumed"} {
		t.Run(mutation, func(t *testing.T) {
			c := map[string][]byte{}
			x := map[string]string{}
			for k, v := range claims {
				c[k] = append([]byte(nil), v...)
				x[k] = expected[k]
			}
			switch mutation {
			case "missing":
				delete(c, "old")
			case "changed":
				c["old"] = []byte(`{}`)
			case "wrong schema":
				c["last"] = []byte(strings.Replace(string(c["last"]), "paired-live-receiver-repeated-claim.v1", "wrong", 1))
				x["last"] = conversationHash(c["last"])
			case "wrong cumulative":
				c["last"] = []byte(strings.Replace(string(c["last"]), "29061120", "29061119", 1))
				x["last"] = conversationHash(c["last"])
			case "unconsumed":
				c["last"] = []byte(strings.Replace(string(c["last"]), "consumed_before_execution", "pending", 1))
				x["last"] = conversationHash(c["last"])
			}
			if _, e := verifyFullWindowBaselineBytes(c, x, "last"); e == nil {
				t.Fatal("accepted corrupt chain")
			}
		})
	}
}

func TestFullWindowScopeInvalidFrozenMetadata(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	caps := make([]fullWindowCaps, 37)
	for i := range caps {
		caps[i] = fullWindowCaps{1, 1}
	}
	for _, field := range []string{"policy", "price", "envelope", "executor", "customer"} {
		t.Run(field, func(t *testing.T) {
			bad := b
			switch field {
			case "policy":
				bad.PolicySHA256 = ""
			case "price":
				bad.PriceEvidenceSHA256 = "bad"
			case "envelope":
				bad.RequestEnvelopeSHA256 = ""
			case "executor":
				bad.ExecutorRevision = strings.Repeat("g", 40)
			case "customer":
				bad.CustomerRevision = ""
			}
			if _, e := prepareFullWindowScope(s, p, 100, caps, bad); e == nil {
				t.Fatal("accepted incomplete binding")
			}
		})
	}
	caps[0].ModelCap = -1
	if _, e := prepareFullWindowScope(s, p, 100, caps, b); e == nil {
		t.Fatal("negative cap")
	}
	caps[0].ModelCap = 1
	caps[0].JEVCap = 1 << 62
	if _, e := prepareFullWindowScope(s, p, 100, caps, b); e == nil {
		t.Fatal("overflow cap")
	}
}
func TestFullWindowScopeWindowCountsAndDigest(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	for _, width := range []int{1, 99, 100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			caps := make([]fullWindowCaps, (3660+width-1)/width)
			for i := range caps {
				caps[i] = fullWindowCaps{1, 1}
			}
			v, e := prepareFullWindowScope(s, p, width, caps, b)
			if e != nil {
				t.Fatal(e)
			}
			for _, w := range v.Windows {
				turns := 0
				for _, slot := range v.Slots[w.FirstOrdinal-1 : w.LastOrdinal] {
					if slot.WindowID != w.ID {
						t.Fatal("window identity")
					}
					turns += slot.UserTurns
				}
				if w.MaxModelCalls != 10*turns || w.MaxJEVCalls != turns || w.LastOrdinal-w.FirstOrdinal+1 > width {
					t.Fatal("wrong attempt bounds")
				}
			}
			digest, _, e := validateFullWindowScope(v, s, p, b)
			raw, _ := json.Marshal(v)
			if e != nil || digest != conversationHash(raw) {
				t.Fatal("scope canonical identity")
			}
		})
	}
}
func TestFullWindowBaselineLoaderMissingAndChanged(t *testing.T) {
	if _, e := loadFullWindowBaseline(t.TempDir()); e == nil {
		t.Fatal("missing chain accepted")
	}
	dir := t.TempDir()
	for id := range fullWindowCanonicalClaims() {
		if e := os.WriteFile(filepath.Join(dir, id+".json"), []byte("{}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := loadFullWindowBaseline(dir); e == nil {
		t.Fatal("changed canonical raw claims accepted")
	}

}
