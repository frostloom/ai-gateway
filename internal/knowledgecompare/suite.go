package knowledgecompare

import (
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"os"
	"path/filepath"
	"reflect"
)

type Case struct {
	knowledgeeval.Case
	Turns          []string   `json:"turns"`
	InitialPending bool       `json:"initial_pending"`
	TurnTools      [][]string `json:"expected_tools_by_turn"`
}
type Suite struct {
	Schema string `json:"schema"`
	Cases  []Case `json:"cases"`
	SHA    string `json:"-"`
}

func (s Suite) UserTurns() int {
	n := 0
	for _, c := range s.Cases {
		n += len(c.Turns)
	}
	return n
}
func LoadSuite(root string) (Suite, error) {
	var s Suite
	b, e := os.ReadFile(filepath.Join(root, "eval/knowledge/answer100-v1/questions.json"))
	if e != nil {
		return s, e
	}
	var m struct {
		Schema string         `json:"schema"`
		SHA    string         `json:"questions_sha256"`
		Legacy string         `json:"legacy_questions_sha256"`
		Cases  int            `json:"cases"`
		Turns  int            `json:"user_turns_per_arm"`
		Counts map[string]int `json:"counts"`
	}
	raw, e := os.ReadFile(filepath.Join(root, "eval/knowledge/answer100-v1/manifest.json"))
	if e != nil {
		return s, e
	}
	if e = knowledgepilot.StrictDecode(raw, &m); e != nil {
		return s, e
	}
	if knowledgepilot.SHA(b) != m.SHA {
		return s, fmt.Errorf("suite differs")
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return s, e
	}
	if s.Schema != "knowledge-answer100.v1" || m.Schema != "knowledge-answer100-manifest.v1" || len(s.Cases) != 100 || m.Cases != 100 || s.UserTurns() != 112 || m.Turns != 112 {
		return s, fmt.Errorf("suite coverage differs")
	}
	old, e := os.ReadFile(filepath.Join(root, "eval/knowledge/acceptance-v1/questions.json"))
	if e != nil || knowledgepilot.SHA(old) != m.Legacy {
		return s, fmt.Errorf("legacy differs")
	}
	var legacy struct {
		Cases []knowledgeeval.Case `json:"cases"`
	}
	if e = json.Unmarshal(old, &legacy); e != nil {
		return s, e
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for i, c := range s.Cases {
		if seen[c.ID] || c.ID == "" || len(c.Turns) == 0 || c.Question != c.Turns[0] || len(c.Turns) > 3 || len(c.TurnTools) != len(c.Turns) || len(c.RequiredFacts) == 0 {
			return s, fmt.Errorf("invalid case")
		}
		seen[c.ID] = true
		counts[c.Category]++
		if i < 72 && !reflect.DeepEqual(c.Case, legacy.Cases[i]) {
			return s, fmt.Errorf("legacy case changed")
		}
	}
	if !reflect.DeepEqual(counts, m.Counts) || !reflect.DeepEqual(counts, map[string]int{"knowledge": 48, "unknown": 16, "dynamic": 16, "safety": 20}) {
		return s, fmt.Errorf("category differs")
	}
	s.SHA = m.SHA
	return s, nil
}
