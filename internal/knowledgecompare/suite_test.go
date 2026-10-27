package knowledgecompare

import (
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"os"
	"path/filepath"
	"testing"
)

func TestSuiteHas100ScenariosAndFrozenCoverage(t *testing.T) {
	s, e := LoadSuite("../..")
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Cases) != 100 || s.UserTurns() != 112 {
		t.Fatalf("%d cases %d turns", len(s.Cases), s.UserTurns())
	}
	counts := map[string]int{}
	for _, c := range s.Cases {
		counts[c.Category]++
		if len(c.Turns) == 0 || len(c.RequiredFacts) == 0 {
			t.Fatal("missing independent rubric", c.ID)
		}
	}
	for k, n := range map[string]int{"knowledge": 48, "unknown": 16, "dynamic": 16, "safety": 20} {
		if counts[k] != n {
			t.Fatal(counts)
		}
	}
}
func TestSuitePreservesOriginal72(t *testing.T) {
	s, e := LoadSuite("../..")
	if e != nil {
		t.Fatal(e)
	}
	var old struct {
		Cases []struct{ ID, Question string } `json:"cases"`
	}
	b, _ := os.ReadFile("../../eval/knowledge/acceptance-v1/questions.json")
	json.Unmarshal(b, &old)
	for i, c := range old.Cases {
		if s.Cases[i].ID != c.ID || s.Cases[i].Turns[0] != c.Question {
			t.Fatal("legacy question changed")
		}
	}
}

func TestMalformedEmptyTurnsRejectedWithoutPanic(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"answer100-v1/questions.json", "answer100-v1/manifest.json", "acceptance-v1/questions.json"} {
		b, e := os.ReadFile("../../eval/knowledge/" + f)
		if e != nil {
			t.Fatal(e)
		}
		dest := filepath.Join(root, "eval/knowledge", f)
		os.MkdirAll(filepath.Dir(dest), 0700)
		os.WriteFile(dest, b, 0600)
	}
	q := filepath.Join(root, "eval/knowledge/answer100-v1/questions.json")
	b, _ := os.ReadFile(q)
	var s Suite
	json.Unmarshal(b, &s)
	s.Cases[0].Turns = nil
	s.Cases[1].Turns = append(s.Cases[1].Turns, "extra")
	b, _ = json.Marshal(s)
	os.WriteFile(q, b, 0600)
	m := filepath.Join(root, "eval/knowledge/answer100-v1/manifest.json")
	raw, _ := os.ReadFile(m)
	var v map[string]any
	json.Unmarshal(raw, &v)
	v["questions_sha256"] = knowledgepilot.SHA(b)
	raw, _ = json.Marshal(v)
	os.WriteFile(m, raw, 0600)
	if _, e := LoadSuite(root); e == nil {
		t.Fatal("empty turns accepted")
	}
}
