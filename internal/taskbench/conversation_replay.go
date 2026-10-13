package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type ConversationEpisode struct {
	Key     string                    `json:"key"`
	Fixture Fixture                   `json:"fixture"`
	Initial ConversationState         `json:"initial"`
	Steps   []ConversationObservation `json:"steps"`
	Final   ConversationState         `json:"final"`
}
type ConversationReplay struct {
	RawSHA256        string                `json:"-"`
	Schema           string                `json:"schema"`
	DefinitionSHA256 string                `json:"definition_sha256"`
	EvidenceKind     string                `json:"evidence_kind"`
	Episodes         []ConversationEpisode `json:"episodes"`
}
type ConversationReplayReport struct {
	Schema                string               `json:"schema"`
	DefinitionSHA256      string               `json:"definition_sha256"`
	TranscriptSHA256      string               `json:"transcript_sha256"`
	EvidenceKind          string               `json:"evidence_kind"`
	TimingMeaning         string               `json:"timing_meaning"`
	CostStatus            string               `json:"cost_status"`
	ObservedLLMCalls      *int64               `json:"observed_llm_calls"`
	ObservedJEVCalls      *int64               `json:"observed_jev_calls"`
	MissingTelemetryTurns int                  `json:"missing_telemetry_turns"`
	Limitations           []string             `json:"limitations"`
	Metrics               ConversationMetrics  `json:"metrics"`
	Results               []ConversationResult `json:"results"`
}

func LoadConversationReplay(path string) (ConversationReplay, error) {
	var out ConversationReplay
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&out); err != nil {
		return out, err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return out, fmt.Errorf("trailing replay JSON")
	}
	out.RawSHA256 = conversationHash(raw)
	return out, nil
}

type replayConversationDriver struct {
	episode ConversationEpisode
	next    int
}

func (d *replayConversationDriver) Setup(context.Context, ConversationCase) (Fixture, ConversationState, error) {
	return d.episode.Fixture, d.episode.Initial, nil
}
func (d *replayConversationDriver) Step(_ context.Context, _ Fixture, _, message string) (ConversationObservation, error) {
	if d.next >= len(d.episode.Steps) {
		return ConversationObservation{}, fmt.Errorf("replay turn missing")
	}
	obs := d.episode.Steps[d.next]
	d.next++
	if obs.Turn.Message != message {
		return obs, fmt.Errorf("recorded user message differs from defined continuation")
	}
	return obs, nil
}
func (d *replayConversationDriver) Final(context.Context, Fixture, string) (ConversationState, error) {
	return d.episode.Final, nil
}

// ReplayConversations is deliberately read-only. It never creates a database
// connection, loads .env, sends HTTP, or instantiates an agent/model client.
func ReplayConversations(suite ConversationSuite, input ConversationReplay) (ConversationReplayReport, error) {
	report := ConversationReplayReport{Schema: "conversation-replay-report.v1", DefinitionSHA256: suite.DefinitionSHA256, TranscriptSHA256: input.RawSHA256, EvidenceKind: input.EvidenceKind, CostStatus: "not_evaluated", TimingMeaning: "Local replay processing including oracle checks; not observed model or user task latency.", Limitations: []string{
		"Offline replay of supplied observations, not a fresh model run or recovery of missing historical state.",
		"Frozen source annotations are AI posthoc development definitions, not independent blind gold.",
		"Recorder action labels and every reply require digest-bound review. Missing review is not a passing task.",
		"local_stub evidence and reviews test the evaluator only; never publish their completion rate as V1/V2 model quality.",
		"Business snapshots and reviewer identities are trusted input provenance, not cryptographic attestations of server execution.",
		"Only supplied episodes are scored; unsubmitted definitions remain unexecuted. No population accuracy extrapolation.",
		"Usage missing is unavailable, never zero; provider prices and total currency cost are not evaluated.",
	}}
	if input.Schema != "conversation-replay.v1" || input.DefinitionSHA256 != suite.DefinitionSHA256 || suite.DefinitionSHA256 == "" {
		return report, fmt.Errorf("replay definition/schema binding mismatch")
	}
	if !hasString([]string{"local_stub", "recorded_run"}, input.EvidenceKind) {
		return report, fmt.Errorf("unsupported replay evidence kind")
	}
	if len(input.Episodes) == 0 {
		return report, fmt.Errorf("no replay episodes")
	}
	catalogue := map[string]ConversationCase{}
	for _, c := range suite.Cases {
		catalogue[c.Key()] = c
	}
	seen := map[string]bool{}
	// Validate the entire selection before consuming any episode.
	for _, episode := range input.Episodes {
		c, ok := catalogue[episode.Key]
		if !ok || seen[episode.Key] {
			return report, fmt.Errorf("unknown or duplicate replay scenario: %s", episode.Key)
		}
		seen[episode.Key] = true
		if len(episode.Steps) > len(c.Scenario.Turns) {
			return report, fmt.Errorf("replay turn count mismatch: %s", episode.Key)
		}
		if len(episode.Steps) < len(c.Scenario.Turns) {
			truncatedFailure := false
			if len(episode.Steps) > 0 {
				last := episode.Steps[len(episode.Steps)-1].Turn
				truncatedFailure = last.Status != 200 || last.Error != "" || last.Response.HTTPError != ""
			} else {
				truncatedFailure = validateConversationState(episode.Initial, episode.Fixture) != nil || validateConversationInitial(c, episode.Fixture, episode.Initial) != nil
			}
			if !truncatedFailure {
				return report, fmt.Errorf("replay turn count mismatch: %s", episode.Key)
			}
		}
		for i, obs := range episode.Steps {
			msg, err := resolveConversationMessage(c.Scenario.Turns[i].User, episode.Fixture)
			if err != nil {
				return report, err
			}
			if obs.Turn.Message != msg {
				return report, fmt.Errorf("replay user message mismatch")
			}
			for _, review := range obs.Reviews {
				if input.EvidenceKind == "recorded_run" && review.Method == "local_stub" {
					return report, fmt.Errorf("synthetic review cannot certify a recorded model run")
				}
			}
		}
	}
	var llm, jev int64
	observedTurns := 0
	for _, episode := range input.Episodes {
		r := RunConversation(context.Background(), catalogue[episode.Key], &replayConversationDriver{episode: episode})
		report.Results = append(report.Results, r)
		for _, obs := range episode.Steps {
			observedTurns++
			var t map[string]json.RawMessage
			var lc, jc *int64
			if json.Unmarshal(obs.Turn.TelemetryRaw, &t) == nil {
				_ = json.Unmarshal(t["llm_calls"], &lc)
				_ = json.Unmarshal(t["jev_calls"], &jc)
			}
			if lc == nil || jc == nil || *lc < 0 || *jc < 0 {
				report.MissingTelemetryTurns++
			} else {
				llm += *lc
				jev += *jc
			}
		}
	}
	if report.MissingTelemetryTurns == 0 && observedTurns > 0 {
		report.ObservedLLMCalls = &llm
		report.ObservedJEVCalls = &jev
	}
	report.Metrics = SummarizeConversations(report.Results)
	return report, nil
}
func WriteConversationReplayReport(path string, report ConversationReplayReport) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
