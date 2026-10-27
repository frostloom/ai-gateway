package taskbench

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFullCustomerPrimaryPreparation(t *testing.T) {
	source, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := PrepareFullCustomerPair(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Suite.Cases) != 1220 || len(p.BoundaryKeys) != 1851 {
		t.Fatalf("primary=%d boundaries=%d", len(p.Suite.Cases), len(p.BoundaryKeys))
	}
	seen := map[string]bool{}
	turns := 0
	counts := map[string]int{}
	for _, c := range p.Suite.Cases {
		if seen[c.SourceID] || !c.Scenario.FullTaskMetricEligible {
			t.Fatal("duplicate or diagnostic primary")
		}
		seen[c.SourceID] = true
		turns += len(c.Scenario.Turns)
		counts[c.Scenario.ID]++
	}
	if turns != 1962 || !reflect.DeepEqual(counts, map[string]int{"answer_complete": 696, "explicit_scope_followup": 56, "supplement_then_confirm": 218, "confirm": 250}) {
		t.Fatalf("turns=%d scenarios=%v", turns, counts)
	}
	for _, key := range p.BoundaryKeys {
		for _, c := range p.Suite.Cases {
			if c.Key() == key {
				t.Fatal("boundary in primary denominator")
			}
		}
	}
	if p.AllowPaid || p.ReadyForPaidRun {
		t.Fatal("preparation grants paid permission")
	}
	for i := range p.Suite.Cases {
		if p.Suite.Cases[i].Key() == "recharge-002/confirm" {
			p.Suite.Cases[i].Scenario.Turns[0].User = "mutated"
		}
	}
	q, err := PrepareFullCustomerPair(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range q.Suite.Cases {
		if c.Scenario.Turns[0].User == "mutated" {
			t.Fatal("preparation aliases frozen source")
		}
	}
}

func TestFullCustomerPreparationRejectsRetainedDigestMutation(t *testing.T) {
	source, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"profile", "user", "terminal", "definition-digest"} {
		t.Run(field, func(t *testing.T) {
			var changed ConversationSuite
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "profile":
				changed.Cases[0].Scenario.Profile = "empty_balance"
			case "user":
				changed.Cases[0].Scenario.Turns[0].User += " changed"
			case "terminal":
				changed.Cases[0].Scenario.Terminal["authorized_write_count"] = 9999
			case "definition-digest":
				changed.DefinitionSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
			}
			if _, err := PrepareFullCustomerPair(changed); err == nil {
				t.Fatal("mutated source accepted with retained manifest/counts")
			}
		})
	}
}

func TestFullCustomerPreparationRejectsIncompleteSource(t *testing.T) {
	source, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"duplicate-key", "missing-source", "missing-primary", "empty-source-id"} {
		t.Run(variant, func(t *testing.T) {
			copySource, err := PrepareFullCustomerPair(source)
			if err != nil {
				t.Fatal(err)
			}
			// A primary-only source is already incomplete: boundaries are frozen inputs too.
			s := source
			s.Cases = append([]ConversationCase{}, source.Cases...)
			switch variant {
			case "duplicate-key":
				s.Cases = append(s.Cases, s.Cases[0])
			case "missing-source":
				id := copySource.Suite.Cases[0].SourceID
				out := s.Cases[:0]
				for _, c := range s.Cases {
					if c.SourceID != id {
						out = append(out, c)
					}
				}
				s.Cases = out
			case "missing-primary":
				key := copySource.Suite.Cases[0].Key()
				out := s.Cases[:0]
				for _, c := range s.Cases {
					if c.Key() != key {
						out = append(out, c)
					}
				}
				s.Cases = out
			case "empty-source-id":
				s.Cases[0].SourceID = ""
			}
			if _, err := PrepareFullCustomerPair(s); err == nil {
				t.Fatal("incomplete source accepted")
			}
		})
	}
}
