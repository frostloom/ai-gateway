package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/observability"
)

func fullAdapterSource(t *testing.T) (ConversationSuite, PairedProfiles) {
	t.Helper()
	source, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadPairedProfiles("../../eval/runtime-profiles/2026-10-13-six-profiles-v1")
	if err != nil {
		t.Fatal(err)
	}
	return source, profiles
}

func TestFullAdapterConfigCoversEveryPrimaryInBoundedBatches(t *testing.T) {
	source, profiles := fullAdapterSource(t)
	full, err := PrepareFullCustomerPair(source)
	if err != nil {
		t.Fatal(err)
	}
	model := PairModelSettings{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai"}
	covered := map[string]bool{}
	selectionSHA := ""
	for i := 0; i < len(full.Suite.Cases); i += 8 {
		end := i + 8
		if end > len(full.Suite.Cases) {
			end = len(full.Suite.Cases)
		}
		keys := []string{}
		for _, c := range full.Suite.Cases[i:end] {
			keys = append(keys, c.Key())
		}
		cfg, err := PrepareFullCustomerAgentPairConfig(source, keys, profiles, model, "recorded_run", "explicit-revision", "jev-1.13.0")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AllowPaid || cfg.SelectionScope != FullCustomerPrimaryScope || !comparisonDigest(cfg.SelectionSHA256) || len(cfg.Suite.Cases) != len(keys) {
			t.Fatal("scope or paid boundary lost")
		}
		if selectionSHA != "" && cfg.SelectionSHA256 != selectionSHA {
			t.Fatal("child batches do not bind same full selection")
		}
		selectionSHA = cfg.SelectionSHA256
		for j, c := range cfg.Suite.Cases {
			if c.Key() != keys[j] || covered[c.Key()] {
				t.Fatal("missing or repeated source")
			}
			covered[c.Key()] = true
		}
		legacy, err := PrepareAgentPairConfig(cfg.Suite, profiles, model, "recorded_run", "explicit-revision", "jev-1.13.0")
		if err != nil {
			t.Fatal(err)
		}
		if PairRunContract(legacy) == PairRunContract(cfg) {
			t.Fatal("full and historical contract interchangeable")
		}
		raw, _ := json.Marshal(legacy)
		if strings.Contains(string(raw), "selection_scope") || strings.Contains(string(raw), "selection_sha256") {
			t.Fatal("legacy JSON changed")
		}
	}
	if len(covered) != 1220 {
		t.Fatalf("covered=%d", len(covered))
	}
}

func TestFullAdapterRejectsNonPrimaryOrDuplicateSelection(t *testing.T) {
	source, profiles := fullAdapterSource(t)
	model := PairModelSettings{BaseURL: "http://127.0.0.1:1", Model: "local", Protocol: "anthropic"}
	for _, keys := range [][]string{nil, {"models-001/first_turn"}, {"recharge-002/cancel"}, {"models-001/answer_complete", "models-001/answer_complete"}, {"missing/answer_complete"}, make([]string, 13)} {
		if _, err := PrepareFullCustomerAgentPairConfig(source, keys, profiles, model, "local_stub", "test", ""); err == nil {
			t.Fatal("non-primary or duplicate selection accepted", keys)
		}
	}
}

func TestFullAdapterMarkersAndGuardsRejectBeforeDependencies(t *testing.T) {
	source, profiles := fullAdapterSource(t)
	cfg, err := PrepareFullCustomerAgentPairConfig(source, []string{"models-001/answer_complete"}, profiles, PairModelSettings{BaseURL: "http://127.0.0.1:1", Model: "local", Protocol: "anthropic"}, "local_stub", "test", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := RunAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, Config: cfg}); err == nil || !strings.Contains(err.Error(), "selection") {
		t.Fatalf("legacy entry did not reject full source: %v", err)
	}
	legacy := cfg
	legacy.SelectionScope = ""
	legacy.SelectionSHA256 = ""
	if err := RunFullCustomerAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, Config: legacy}); err == nil || !strings.Contains(err.Error(), "selection") {
		t.Fatalf("full entry accepted legacy source: %v", err)
	}
	if err := RunFullCustomerAgentPair(context.Background(), path, AgentPairOptions{AllowExecution: true, Config: cfg}); err == nil || !strings.Contains(err.Error(), "stop guards") {
		t.Fatalf("full entry accepted missing guards: %v", err)
	}
	for _, missing := range []string{"structural", "observer", "facts", "jev"} {
		o := AgentPairOptions{AllowExecution: true, Config: cfg, Observer: &observability.Observer{}, StopOnStructuralFailure: true, StopOnObserverFailure: true, StopOnFactFailure: true, StopOnJEVFailure: true}
		switch missing {
		case "structural":
			o.StopOnStructuralFailure = false
		case "observer":
			o.StopOnObserverFailure = false
		case "facts":
			o.StopOnFactFailure = false
		case "jev":
			o.StopOnJEVFailure = false
		}
		if err := RunFullCustomerAgentPair(context.Background(), path, o); err == nil || !strings.Contains(err.Error(), "stop guards") {
			t.Fatalf("missing %s guard reached dependencies: %v", missing, err)
		}
	}
	if err := RunFullCustomerAgentPair(context.Background(), path, AgentPairOptions{Config: cfg}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled full adapter reached dependencies: %v", err)
	}
	partial := cfg
	partial.SelectionSHA256 = ""
	if validatePairRun(partial) == nil {
		t.Fatal("partial marker accepted")
	}
	for _, suffix := range []string{"", ".lock", ".jev.jsonl", ".jev.jsonl.lock"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatal("rejected adapter created journal")
		}
	}
}
