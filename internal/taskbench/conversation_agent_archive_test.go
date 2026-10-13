package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConversationAgentLocalCaptureIntegrity(t *testing.T) {
	base := "../../eval/conversation-regressions/2026-10-13-agent-local-v1"
	raw, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if conversationHash(raw) != "caf578141ce37070ba7d15eb92a23573b7b4dca9b69d599e2cc9db641409cc5e" {
		t.Fatal("local capture manifest changed")
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 {
		t.Fatal("capture file set changed")
	}
	for _, name := range []string{"events.jsonl", "summary.json"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || conversationHash(data) != manifest.Files[name] {
			t.Fatalf("capture changed: %s %v", name, err)
		}
	}
	raw, err = os.ReadFile(filepath.Join(base, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Kind     string         `json:"evidence_kind"`
		Cases    int            `json:"cases"`
		Passed   int            `json:"structural_passed"`
		Pending  int            `json:"pending_semantic_review"`
		Rate     *float64       `json:"completion_rate"`
		Requests map[string]int `json:"local_model_http_requests"`
		Total    int            `json:"local_model_http_requests_total"`
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Kind != "local_stub" || summary.Cases != 15 || summary.Passed != 15 || summary.Pending != 15 || summary.Rate != nil || summary.Total != 30 || len(summary.Requests) != 15 {
		t.Fatal("capture semantics changed")
	}
	raw, err = os.ReadFile(filepath.Join(base, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	requests := map[string]int{}
	for _, line := range strings.Split(strings.TrimPrefix(string(raw), "\ufeff"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Action string
			Test   string
			Output string
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Action == "fail" || event.Action == "skip" {
			t.Fatal("capture includes failed/skipped branch")
		}
		if event.Action == "pass" && strings.Contains(event.Test, "/") {
			name := strings.SplitN(event.Test, "/", 2)[1]
			if seen[name] {
				t.Fatal("duplicate branch")
			}
			seen[name] = true
		}
		if strings.Contains(event.Output, "local model requests=") {
			name := strings.SplitN(event.Test, "/", 2)[1]
			n := 0
			if _, err := fmt.Sscanf(strings.Split(event.Output, "local model requests=")[1], "%d;", &n); err != nil {
				t.Fatal(err)
			}
			requests[name] = n
		}
	}
	if len(seen) != 15 {
		t.Fatal("capture branch count differs")
	}
	total := 0
	for name, n := range summary.Requests {
		if !seen[name] || requests[name] != n {
			t.Fatal("summary differs from actual test events")
		}
		total += n
	}
	if total != summary.Total {
		t.Fatal("capture request total differs")
	}
}
