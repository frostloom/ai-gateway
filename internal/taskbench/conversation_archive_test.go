package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConversationArchivedStoreEvidenceReplay(t *testing.T) {
	const base = "../../eval/conversation-regressions/2026-10-13-local-v1"
	raw, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if conversationHash(raw) != "143c3bfe8323bce7288c6e6933874d01fb2520aed3d9cc8e85b3ca5a05133325" {
		t.Fatal("frozen local manifest changed")
	}
	var manifest struct {
		Schema        string            `json:"schema"`
		Files         map[string]string `json:"files"`
		DefinitionSHA string            `json:"definition_sha256"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "conversation-local-regression.v1" || len(manifest.Files) != 2 {
		t.Fatal("invalid local evidence manifest")
	}
	for name, digest := range manifest.Files {
		if filepath.Base(name) != name {
			t.Fatal("invalid archive name")
		}
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if conversationHash(data) != digest {
			t.Fatalf("local evidence changed: %s", name)
		}
	}
	suite, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	if suite.DefinitionSHA256 != manifest.DefinitionSHA {
		t.Fatal("source definition binding changed")
	}
	input, err := LoadConversationReplay(filepath.Join(base, "transcript.json"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ReplayConversations(suite, input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(base, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved ConversationReplayReport
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual.Metrics, saved.Metrics) || actual.TranscriptSHA256 != saved.TranscriptSHA256 || actual.EvidenceKind != "local_stub" || actual.Metrics.StructuralPassed != 30 || actual.Metrics.PendingReview != 30 || actual.Metrics.Completed != 0 || actual.Metrics.CompletionRate != nil || actual.ObservedLLMCalls == nil || *actual.ObservedLLMCalls != 0 || actual.ObservedJEVCalls == nil || *actual.ObservedJEVCalls != 0 {
		t.Fatal("archived local evidence no longer reproduces its explicit boundaries")
	}
}
