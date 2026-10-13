package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConversationReplayKeepsTruncatedHTTPFailureAndUsage(t *testing.T) {
	c, d := rechargeConversation(), rechargeDriver()
	suite := ConversationSuite{DefinitionSHA256: "test-sha", Cases: []ConversationCase{c}}
	d.steps[0].Turn.Message = c.Scenario.Turns[0].User
	d.steps[0].Turn.Status = 504
	d.steps[0].Turn.TelemetryRaw = json.RawMessage(`{"llm_calls":1,"jev_calls":0}`)
	input := ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: "local_stub", Episodes: []ConversationEpisode{{Key: c.Key(), Fixture: d.f, Initial: d.initial, Steps: d.steps[:1], Final: d.initial}}}
	report, err := ReplayConversations(suite, input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.ValidFullTasks != 1 || report.Metrics.Failed != 1 || report.Metrics.HTTPFailures != 1 || report.ObservedLLMCalls == nil || *report.ObservedLLMCalls != 1 {
		t.Fatalf("failure lost from denominator/usage: %+v", report)
	}
}

func TestConversationReplayUsageIndependentOfSetupScoring(t *testing.T) {
	c, d := rechargeConversation(), rechargeDriver()
	suite := ConversationSuite{DefinitionSHA256: "test-sha", Cases: []ConversationCase{c}}
	for i := range d.steps {
		d.steps[i].Turn.Message = c.Scenario.Turns[i].User
		d.steps[i].Turn.TelemetryRaw = json.RawMessage(`{"llm_calls":1,"jev_calls":0}`)
	}
	d.initial.Own.Consistent = false
	input := ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: "local_stub", Episodes: []ConversationEpisode{{Key: c.Key(), Fixture: d.f, Initial: d.initial, Steps: d.steps, Final: d.steps[2].State}}}
	report, err := ReplayConversations(suite, input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.SetupFailures != 1 || report.ObservedLLMCalls == nil || *report.ObservedLLMCalls != 3 {
		t.Fatalf("setup rejection erased supplied usage: %+v", report)
	}
}

func TestConversationReplayZeroTurnsWrongProfileIsSetupFailure(t *testing.T) {
	c, d := rechargeConversation(), rechargeDriver()
	d.initial.Own.BalanceCent = 2500
	d.initial.Own.LedgerBalance = 2500
	suite := ConversationSuite{DefinitionSHA256: "test-sha", Cases: []ConversationCase{c}}
	input := ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: "local_stub", Episodes: []ConversationEpisode{{Key: c.Key(), Fixture: d.f, Initial: d.initial, Final: d.initial}}}
	report, err := ReplayConversations(suite, input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.SetupFailures != 1 || report.Metrics.ValidFullTasks != 0 || report.ObservedLLMCalls != nil {
		t.Fatalf("setup failure misreported: %+v", report)
	}
}

func TestConversationReplayRejectsMissingDuplicatedAndUnboundEvidence(t *testing.T) {
	c, d := rechargeConversation(), rechargeDriver()
	suite := ConversationSuite{DefinitionSHA256: "frozen-test-sha", Cases: []ConversationCase{c}}
	makeReplay := func() ConversationReplay {
		steps := append([]ConversationObservation(nil), d.steps...)
		for i := range steps {
			steps[i].Turn.Message = c.Scenario.Turns[i].User
		}
		return ConversationReplay{Schema: "conversation-replay.v1", DefinitionSHA256: suite.DefinitionSHA256, EvidenceKind: "local_stub", Episodes: []ConversationEpisode{{Key: c.Key(), Fixture: d.f, Initial: d.initial, Steps: steps, Final: d.steps[2].State}}}
	}
	for _, name := range []string{"wrong_sha", "duplicate", "missing_turn", "wrong_message", "unknown_case", "real_review_stub"} {
		t.Run(name, func(t *testing.T) {
			replay := makeReplay()
			switch name {
			case "wrong_sha":
				replay.DefinitionSHA256 = "other"
			case "duplicate":
				replay.Episodes = append(replay.Episodes, replay.Episodes[0])
			case "missing_turn":
				replay.Episodes[0].Steps = replay.Episodes[0].Steps[:2]
			case "wrong_message":
				replay.Episodes[0].Steps[1].Turn.Message = "充值999元"
			case "unknown_case":
				replay.Episodes[0].Key = "unknown/confirm"
			case "real_review_stub":
				replay.EvidenceKind = "recorded_run"
				replay.Episodes[0].Steps[0].Reviews = []ConversationReview{{Method: "local_stub"}}
			}
			if _, err := ReplayConversations(suite, replay); err == nil {
				t.Fatal("invalid replay accepted")
			}
		})
	}
	report, err := ReplayConversations(suite, makeReplay())
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.PendingReview != 1 || report.Metrics.Completed != 0 || report.CostStatus != "not_evaluated" || report.MissingTelemetryTurns != 3 || report.ObservedLLMCalls != nil {
		t.Fatalf("fabricated quality/usage: %+v", report)
	}
}

func TestConversationReplayFileStrictAndExclusiveOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	report := ConversationReplayReport{Schema: "conversation-replay-report.v1"}
	if err := WriteConversationReplayReport(path, report); err != nil {
		t.Fatal(err)
	}
	if err := WriteConversationReplayReport(path, report); err == nil {
		t.Fatal("overwrote report")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got ConversationReplayReport
	if err = json.Unmarshal(raw, &got); err != nil || got.Schema != report.Schema {
		t.Fatal("invalid persisted report")
	}
	input := filepath.Join(t.TempDir(), "replay.json")
	if err = os.WriteFile(input, []byte(`{"schema":"conversation-replay.v1","url":"https://paid-model"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadConversationReplay(input); err == nil {
		t.Fatal("unknown/online field accepted")
	}
	raw = []byte(`{"schema":"conversation-replay.v1","definition_sha256":"test","evidence_kind":"local_stub","episodes":[]}`)
	if err = os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConversationReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RawSHA256 != conversationHash(raw) {
		t.Fatal("hash not bound to decoded bytes")
	}
}
