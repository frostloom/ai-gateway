package localpaired

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteCaseWritesBothArmsInsideTheBundle(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "2026-11-10-local-paired")
	bundle, err := Prepare(root, Campaign{Passes: 1, GraderModel: "gpt-6.1-sol"})
	if err != nil {
		t.Fatal(err)
	}
	item := storedCase{
		ID:     "local-read-history-001",
		Turns:  []storedTurn{{User: "查一下余额", Kind: "read_balance"}},
		Rubric: []storedRubric{{ID: "local-read-history-001:balance", Text: "回复当前余额，数值来自工具结果。"}},
	}
	var posts []string
	err = bundle.ExecuteCase(context.Background(), item, nil, func(_ context.Context, arm string, _ Turn) (Outcome, error) {
		if arm == "jev_selective" {
			return Outcome{
				Reply:           "余额 12 元",
				Tools:           []ToolRecord{{Name: "get_balance", Result: "12"}},
				HighConfidence:  true,
				PendingBefore:   true,
				PendingAfter:    true,
				DatabaseChanged: false,
			}, nil
		}
		return Outcome{Reply: "我来查", ModelCalls: 1, PendingBefore: true, PendingAfter: true}, nil
	}, func(_ context.Context, endpoint string, body []byte) error {
		posts = append(posts, endpoint)
		matches, err := filepath.Glob(filepath.Join(root, "traces", "trace-*.bin"))
		if err != nil {
			return err
		}
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if string(raw) == string(body) {
				return nil
			}
		}
		return fmt.Errorf("trace was posted before the local file existed")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0] != localLangfuseTraceEndpoint || posts[1] != localLangfuseTraceEndpoint {
		t.Fatalf("posts = %v", posts)
	}

	raw := mustRead(t, filepath.Join(root, "records", "local-read-history-001.json"))
	var record struct {
		ID   string `json:"id"`
		Arms []struct {
			Arm     string   `json:"arm"`
			Pass    bool     `json:"pass"`
			Reasons []string `json:"reasons"`
			Packet  string   `json:"packet"`
			Traces  []string `json:"traces"`
		} `json:"arms"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record.ID != item.ID || len(record.Arms) != 2 {
		t.Fatalf("record = %+v", record)
	}
	if record.Arms[0].Arm != "model" || record.Arms[0].Pass || !containsReason(record.Arms[0].Reasons, "tool_missing") {
		t.Fatalf("model arm = %+v", record.Arms[0])
	}
	if record.Arms[1].Arm != "jev_selective" || !record.Arms[1].Pass || len(record.Arms[1].Reasons) != 0 {
		t.Fatalf("jev arm = %+v", record.Arms[1])
	}
	for _, arm := range record.Arms {
		packet := mustRead(t, filepath.Join(root, filepath.FromSlash(arm.Packet)))
		if strings.Contains(string(packet), "jev_selective") || strings.Contains(string(packet), "model_calls") {
			t.Fatalf("packet leaked arm data: %s", packet)
		}
		if len(arm.Traces) != 1 {
			t.Fatalf("traces = %v", arm.Traces)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(arm.Traces[0]))); err != nil {
			t.Fatal(err)
		}
	}
	scoreEntries, err := os.ReadDir(filepath.Join(root, "scores"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scoreEntries) != 0 {
		t.Fatalf("scores = %v", names(scoreEntries))
	}
	parentEntries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(parentEntries) != 1 {
		t.Fatalf("parent entries = %v", names(parentEntries))
	}
}

func TestExecuteCaseWritesNothingWhenDialogFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "campaign")
	bundle, err := Prepare(root, Campaign{Passes: 1})
	if err != nil {
		t.Fatal(err)
	}
	item := storedCase{ID: "local-read-history-001", Turns: []storedTurn{{User: "查一下余额", Kind: "read_balance"}}}
	err = bundle.ExecuteCase(context.Background(), item, nil, func(context.Context, string, Turn) (Outcome, error) {
		return Outcome{}, fmt.Errorf("stopped")
	}, func(context.Context, string, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("err = %v", err)
	}
	for _, dir := range []string{"records", "traces", "packets"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s = %v", dir, names(entries))
		}
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
