package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowV10ClaimDeclarationExactSuffixOnly(t *testing.T) {
	v, s, p, b := windowV10FixtureScope(t)
	sha, _, e := validateFullWindowScope(v, s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: sha, ScheduleSHA256: v.ScheduleSHA256, PolicySHA256: b.PolicySHA256, Windows: v.Windows, ModelTotal: 110191257, JEVTotal: 1818872}
	if e = validateV10ClaimDeclaration(d, v.Completion); e != nil {
		t.Fatal(e)
	}
	mutations := map[string]func(*fullWindowClaimDeclaration){
		"empty scope":          func(d *fullWindowClaimDeclaration) { d.ScopeSHA256 = "" },
		"empty policy":         func(d *fullWindowClaimDeclaration) { d.PolicySHA256 = "" },
		"foreign schedule":     func(d *fullWindowClaimDeclaration) { d.ScheduleSHA256 = strings.Repeat("f", 64) },
		"old full declaration": func(d *fullWindowClaimDeclaration) { d.Windows = windowV9FixtureWindows(t, s, p, b) },
		"prefix window":        func(d *fullWindowClaimDeclaration) { d.Windows[0].ID = "window-001"; d.Windows[0].FirstOrdinal = 1 },
		"foreign order":        func(d *fullWindowClaimDeclaration) { d.Windows[0], d.Windows[1] = d.Windows[1], d.Windows[0] },
		"missing":              func(d *fullWindowClaimDeclaration) { d.Windows = d.Windows[:455] },
		"extra":                func(d *fullWindowClaimDeclaration) { d.Windows = append(d.Windows, d.Windows[455]) },
		"rewritten last":       func(d *fullWindowClaimDeclaration) { d.Windows[455].LastOrdinal-- },
		"sum preserving caps":  func(d *fullWindowClaimDeclaration) { d.Windows[0].ModelCap++; d.Windows[1].ModelCap-- },
		"sum preserving calls": func(d *fullWindowClaimDeclaration) {
			d.Windows[0].MaxModelCalls += 10
			d.Windows[0].MaxJEVCalls++
			d.Windows[1].MaxModelCalls -= 10
			d.Windows[1].MaxJEVCalls--
		},
		"parent instead of base": func(d *fullWindowClaimDeclaration) { d.ModelTotal = 185000000; d.JEVTotal = 3000000 },
		"negative cap":           func(d *fullWindowClaimDeclaration) { d.Windows[0].ModelCap = -1 },
		"overflow counts":        func(d *fullWindowClaimDeclaration) { d.Windows[0].MaxJEVCalls = int(^uint(0) >> 1) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(d)
			var changed fullWindowClaimDeclaration
			if e := json.Unmarshal(raw, &changed); e != nil {
				t.Fatal(e)
			}
			mutate(&changed)
			if e := validateV10ClaimDeclaration(changed, v.Completion); e == nil {
				t.Fatal("foreign or rewritten claim declaration accepted")
			}
		})
	}
	badCompletion := *v.Completion
	badCompletion.SuffixWindowsSHA256 = windowV8DeclarationSHA(d.Windows)
	badCompletion.Execution.FirstOrdinal = 1
	if e := validateV10ClaimDeclaration(d, &badCompletion); e == nil {
		t.Fatal("caller-defined composition accepted")
	}
	if e := validateV10ClaimDeclaration(d, nil); e == nil {
		t.Fatal("missing completion accepted")
	}
}

func TestWindowV10PureDataCannotClaimLegacyRegistry(t *testing.T) {
	v, s, p, b := windowV10FixtureScope(t)
	sha, _, e := validateFullWindowScope(v, s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: sha, ScheduleSHA256: v.ScheduleSHA256, PolicySHA256: b.PolicySHA256, Windows: v.Windows, ModelTotal: 110191257, JEVTotal: 1818872}
	if e := validateV10ClaimDeclaration(d, v.Completion); e != nil {
		t.Fatal(e)
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "new-registry")
	if _, e := createFirstFullWindowClaim(root, d); e == nil {
		t.Fatal("pure V10 data entered legacy financial claim writer")
	}
	if _, e := claimFirstFullWindow(root, filepath.Join(parent, "missing-history"), v, s, p, b, sha); e == nil {
		t.Fatal("pure V10 data entered legacy claim pipeline")
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		t.Fatalf("rejection changed registry: %v", e)
	}
	// An arbitrary well-formed scope hash remains data. This validator cannot
	// authenticate dynamic new source/binding hashes and must never issue a token.
	d.ScopeSHA256 = strings.Repeat("f", 64)
	if e := validateV10ClaimDeclaration(d, v.Completion); e != nil {
		t.Fatal("pure syntax/declaration validation unexpectedly authenticates dynamic scope")
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		t.Fatal("pure validation changed registry")
	}
}
