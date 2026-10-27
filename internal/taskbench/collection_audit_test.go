package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func collectionAuditFixture(t *testing.T) CollectionJournalReference {
	t.Helper()
	source, profiles := fullAdapterSource(t)
	full, err := PrepareFullCustomerPair(source)
	if err != nil {
		t.Fatal(err)
	}
	key := ""
	for _, c := range full.Suite.Cases {
		if c.Scenario.Profile == "no_subscription" {
			key = c.Key()
			break
		}
	}
	cfg, err := PrepareFullCustomerAgentPairConfig(source, []string{key}, profiles, PairModelSettings{BaseURL: "http://127.0.0.1", Model: "local-full-customer", Protocol: "anthropic"}, "local_stub", strings.Repeat("b", 40), "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ArmOrder = []string{"model", "jev_selective"}
	path := filepath.Join(t.TempDir(), "run.jsonl")
	err = RunPreparedPair(context.Background(), path, cfg, nil, func(_ context.Context, _ string, c ConversationCase) (ConversationDriver, error) {
		f, state := conversationFixture()
		d := &scriptedConversation{f: f, initial: state}
		for range c.Scenario.Turns {
			d.steps = append(d.steps, ConversationObservation{Action: "unreviewed", State: state, Turn: Turn{Status: 200, Response: Response{SessionID: "session-19", Reply: "未执行任何操作。"}}})
		}
		return d, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return CollectionJournalReference{Path: path, SHA256: conversationHash(raw)}
}
func TestCollectionAuditFrozenSourceAndSafeNegative(t *testing.T) {
	ref := collectionAuditFixture(t)
	report, err := AuditFullCollectionPolicy("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", []CollectionJournalReference{ref})
	if err != nil {
		t.Fatal(err)
	}
	if report.SuppliedPairs != 1 || report.RecordedArms != 2 || report.UnrunSourceCases != 1219 || report.ContinueArms != 2 || report.HaltArms != 0 || report.AllowPaid || report.ActualFull1220Complete {
		t.Fatalf("audit scope wrong: %+v", report)
	}
	if report.RuntimeComplete || report.MissingRuntimeTurns != report.ObservedTurns {
		t.Fatal("absent synthetic telemetry was certified complete")
	}
	for _, r := range report.Rows {
		if !r.LegacyAssessmentUnchanged {
			t.Fatal("old strict assessment changed")
		}
		if r.TaskStatus != "failed" || r.TaskSuccess || r.Disposition != "continue_recorded" {
			t.Fatal("quality negative was upgraded", r)
		}
	}
}
func TestCollectionAuditRejectsMissingChangedOrDuplicateJournal(t *testing.T) {
	ref := collectionAuditFixture(t)
	for _, refs := range [][]CollectionJournalReference{nil, {ref, ref}, {{Path: ref.Path, SHA256: strings.Repeat("f", 64)}}, {{Path: ref.Path + "-missing", SHA256: ref.SHA256}}} {
		if _, err := AuditFullCollectionPolicy("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", refs); err == nil {
			t.Fatal("invalid journal accepted")
		}
	}
}

func TestCollectionAuditRejectsContradictoryRuntime(t *testing.T) {
	ref := collectionAuditFixture(t)
	raw, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"model", "protocol", "routing_mode", "jev_calls", "shared"} {
		t.Run(fault, func(t *testing.T) {
			var cfg PairRunConfig
			events := []pairRunEvent{}
			for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
				var e pairRunEvent
				if json.Unmarshal(line, &e) != nil {
					t.Fatal("bad fixture journal")
				}
				if e.Config != nil {
					cfg = *e.Config
				}
				events = append(events, e)
			}
			changed := false
			for i := range events {
				e := &events[i]
				if e.Kind != "finish" {
					continue
				}
				armcfg := cfg.Model
				if e.Arm == "jev_selective" {
					armcfg = cfg.Selective
				}
				for j := range e.Result.Observations {
					telemetry := comparisonRuntimeTelemetry(armcfg, json.RawMessage(`{}`))
					var fields map[string]any
					json.Unmarshal(telemetry, &fields)
					runtime := fields["runtime"].(map[string]any)
					runtime["thinking_mode"] = "disabled"
					if e.Arm == "model" && !changed {
						if fault == "jev_calls" {
							fields["jev_calls"] = 1
						} else if fault == "shared" {
							runtime["reply_repair_limit"] = 2
						} else {
							runtime[fault] = "wrong"
						}
						changed = true
					}
					e.Result.Observations[j].Turn.TelemetryRaw, _ = json.Marshal(fields)
				}
			}
			var output bytes.Buffer
			for _, e := range events {
				data, _ := json.Marshal(e)
				output.Write(data)
				output.WriteByte('\n')
			}
			path := filepath.Join(t.TempDir(), "changed.jsonl")
			os.WriteFile(path, output.Bytes(), 0600)
			_, err := AuditFullCollectionPolicy("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", []CollectionJournalReference{{Path: path, SHA256: conversationHash(output.Bytes())}})
			if err == nil {
				t.Fatal("contradictory runtime accepted", fault)
			}
		})
	}
}
