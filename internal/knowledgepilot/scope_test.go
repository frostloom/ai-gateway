package knowledgepilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func template(t *testing.T) ([]byte, Scope) {
	t.Helper()
	b, e := os.ReadFile("../../eval/knowledge/control-v1/scope.example.json")
	if e != nil {
		t.Fatal(e)
	}
	var s Scope
	if e = json.Unmarshal(b, &s); e != nil {
		t.Fatal(e)
	}
	return b, s
}
func TestScopeRejectsUnboundAndPaidTemplates(t *testing.T) {
	b, s := template(t)
	r, e := CheckScope(b, nil, nil)
	if e != nil || r.Ready || !strings.Contains(strings.Join(r.Blockers, ","), "cross_entry_budget_uncoordinated") {
		t.Fatalf("%+v %v", r, e)
	}
	if s.ReservedCNYMicro != nil {
		t.Fatal("unverified fee became zero")
	}
	for _, raw := range []string{`{}`, string(b) + ` {}`, strings.Replace(string(b), `"schema":`, `"api_key":"secret","schema":`, 1), strings.Replace(string(b), `"model"`, `"jev_selective"`, 1)} {
		if _, e := CheckScope([]byte(raw), nil, nil); e == nil {
			t.Fatal("accepted malformed or unbound scope")
		}
	}
	s.AllowPaid = true
	s.HumanAuthorized = true
	raw, _ := json.Marshal(s)
	r, e = CheckScope(raw, nil, nil)
	if e != nil || r.Ready {
		t.Fatalf("booleans bypassed blockers: %v %+v", e, r)
	}
}
func TestInputsRejectChangedSuiteCorpusAndProfile(t *testing.T) {
	_, s := template(t)
	in, e := LoadInputs("../..", s)
	if e != nil {
		t.Fatal(e)
	}
	if len(in.Cases) != 72 || len(in.Profiles) != 72 {
		t.Fatal("incomplete suite")
	}
	counts := map[string]int{}
	for _, c := range in.Cases {
		counts[c.Category]++
	}
	if counts["knowledge"] != 36 || counts["unknown"] != 12 || counts["dynamic"] != 12 || counts["safety"] != 12 {
		t.Fatal(counts)
	}
	for _, field := range []string{"suite", "corpus", "profile", "arms"} {
		x := s
		switch field {
		case "suite":
			x.SuiteSHA = strings.Repeat("0", 64)
		case "corpus":
			x.CorpusVersion = strings.Repeat("0", 64)
		case "profile":
			x.ProfileSHA = strings.Repeat("0", 64)
		case "arms":
			x.B.PromptSHA256 = strings.Repeat("0", 64)
		}
		if _, e = LoadInputs("../..", x); e == nil {
			t.Fatal("accepted changed " + field)
		}
	}
	// Copies are modified outside the repository; no frozen evidence is edited.
	root := t.TempDir()
	for _, d := range []string{"eval/knowledge/acceptance-v1", "eval/knowledge/control-v1", "knowledge/platform"} {
		entries, _ := os.ReadDir(filepath.Join("../..", d))
		os.MkdirAll(filepath.Join(root, d), 0700)
		for _, f := range entries {
			if f.IsDir() {
				continue
			}
			b, _ := os.ReadFile(filepath.Join("../..", d, f.Name()))
			os.WriteFile(filepath.Join(root, d, f.Name()), b, 0600)
		}
	}
	os.WriteFile(filepath.Join(root, "eval/knowledge/control-v1/profiles.json"), []byte(`{}`), 0600)
	if _, e = LoadInputs(root, s); e == nil {
		t.Fatal("accepted corrupt profile")
	}
}
func TestOrderHasIndependentAlternatingPairs(t *testing.T) {
	_, s := template(t)
	in, e := LoadInputs("../..", s)
	if e != nil {
		t.Fatal(e)
	}
	order := BuildOrder(in.Cases)
	if len(order) != 144 {
		t.Fatal(len(order))
	}
	seen := map[string]bool{}
	for i, x := range order {
		if seen[x.Identity] {
			t.Fatal("shared identity")
		}
		seen[x.Identity] = true
		want := "a"
		if (i/2)%2 == i%2 {
			want = "a"
		} else {
			want = "b"
		}
		if x.Arm != want || x.CaseID != in.Cases[i/2].ID {
			t.Fatal(i, x)
		}
	}
	if in.Profiles["S011"].Pending != "recharge_100" || in.Profiles["S012"].Pending != "none" {
		t.Fatal("pending states not frozen")
	}
}
