package localpaired

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareKeepsProductsInOneDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "2026-11-10-local-paired")
	campaign := Campaign{
		Passes:      1,
		Arms:        []string{"model", "jev_selective"},
		GraderModel: "gpt-6.1-sol",
		Selected:    []string{"local-read-history-001"},
		Added: []Case{{
			ID:     "local-read-history-001",
			Turns:  []Turn{{User: "查一下余额", Kind: "read_balance"}},
			Rubric: []RubricItem{{ID: "local-read-history-001:balance", Text: "回复当前余额，数值来自工具结果。"}},
		}},
		Rubrics: map[string][]RubricItem{
			"local-read-history-001": {{ID: "local-read-history-001:balance", Text: "回复当前余额，数值来自工具结果。"}},
		},
	}

	bundle, err := Prepare(root, campaign)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"manifest.json",
		"cases.json",
		"records",
		"traces",
		"packets",
		"scores",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "2026-11-10-local-paired" {
		t.Fatalf("parent entries = %v, want only the campaign directory", names(entries))
	}

	raw, err := os.ReadFile(filepath.Join(root, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "local-read-history-001") || !strings.Contains(string(raw), "查一下余额") {
		t.Fatalf("cases.json = %s", raw)
	}
	var manifest struct {
		CustomerModel string   `json:"customer_model"`
		GraderModel   string   `json:"grader_model"`
		Dirs          []string `json:"dirs"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(root, "manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.CustomerModel != CustomerModel || manifest.GraderModel != "gpt-6.1-sol" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if strings.Join(manifest.Dirs, ",") != "records,traces,packets,scores" {
		t.Fatalf("dirs = %v", manifest.Dirs)
	}

	path, err := bundle.SaveBlind(Record{
		ID:     "local-read-history-001",
		Arm:    "jev_selective",
		Rubric: campaign.Added[0].Rubric,
		Turns:  []GradedTurn{{User: "查一下余额", Reply: "余额 12 元"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(root, "packets") {
		t.Fatalf("packet path = %s", path)
	}
	packetRaw := mustRead(t, path)
	if strings.Contains(string(packetRaw), "jev_selective") {
		t.Fatalf("packet leaked the arm: %s", packetRaw)
	}

	tracePath, err := bundle.SaveTrace(context.Background(), []byte("trace-body"), func(context.Context, string, []byte) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tracePath) != filepath.Join(root, "traces") {
		t.Fatalf("trace path = %s", tracePath)
	}
}

func TestPrepareRefusesProtectedAndEscapingWrites(t *testing.T) {
	root := t.TempDir()
	if _, err := Prepare(filepath.Join(root, "deploy", "langfuse"), Campaign{}); err == nil {
		t.Fatal("expected protected path to be refused")
	}
	bundle, err := Prepare(filepath.Join(root, "campaign"), Campaign{Passes: 1, GraderModel: "gpt-6.1-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.SaveBlind(Record{ID: "../outside"}); err == nil {
		t.Fatal("expected escaping packet id to be refused")
	}
}

func TestMaterializeCampaignBundle(t *testing.T) {
	root := os.Getenv("LOCAL_PAIRED_BUNDLE")
	if root == "" {
		t.Skip("LOCAL_PAIRED_BUNDLE is empty")
	}
	campaign, err := LoadCampaign("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root, campaign); err != nil {
		t.Fatal(err)
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.Name()
	}
	return out
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
