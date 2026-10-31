package taskbench

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

type windowExecutorChangeV10 struct {
	Before string `json:"before"`
	After  string `json:"after"`
}
type windowExecutorEquivalenceV10 struct {
	Schema                string                             `json:"schema"`
	OriginalSourcesSHA256 string                             `json:"original_sources_sha256"`
	SourcesSHA256         string                             `json:"sources_sha256"`
	OriginalSources       map[string]string                  `json:"original_sources"`
	ChangedFiles          map[string]windowExecutorChangeV10 `json:"changed_files"`
}

// The independently signed manifest binds this exact diff artifact. The old
// source inventory is a hash-pinned data input, never an execution authority.
func validateWindowV10ExecutorEquivalence(m windowActualManifest) error {
	if m.Schema != "full-window-actual-ready.v10" {
		if m.ExecutorEquivalence != nil {
			return fmt.Errorf("old epoch carries v10 equivalence")
		}
		return nil
	}
	if m.ExecutorEquivalence == nil {
		return fmt.Errorf("v10 executor equivalence absent")
	}
	raw, err := readWindowStoppedV7File(m.ExecutorEquivalence.Path, m.ExecutorEquivalence.SHA256, 8<<20)
	var e windowExecutorEquivalenceV10
	if err != nil || decodeWindowActualJSON(raw, &e) != nil {
		return fmt.Errorf("v10 executor equivalence unreadable")
	}
	return validateWindowV10ExecutorEquivalenceData(m, e)
}
func validateWindowV10ExecutorEquivalenceData(m windowActualManifest, e windowExecutorEquivalenceV10) error {
	old, _ := json.Marshal(e.OriginalSources)
	current, _ := json.Marshal(m.Sources)
	if e.Schema != "full-window-executor-equivalence.v10" || e.OriginalSourcesSHA256 != expectedWindowCompletionDeclarationV10().OriginalSourcesSHA256 || conversationHash(old) != e.OriginalSourcesSHA256 || conversationHash(current) != e.SourcesSHA256 {
		return fmt.Errorf("v10 equivalence source binding")
	}
	changes := 0
	for name, before := range e.OriginalSources {
		if !comparisonDigest(before) {
			return fmt.Errorf("v10 invalid old source pin")
		}
		after := m.Sources[name]
		if before == after {
			if _, ok := e.ChangedFiles[name]; ok {
				return fmt.Errorf("v10 invented source change")
			}
			continue
		}
		if !windowV10ExecutorOnlyPath(name) || e.ChangedFiles[name] != (windowExecutorChangeV10{before, after}) {
			return fmt.Errorf("v10 customer or unreviewed change: %s", name)
		}
		changes++
	}
	for name, after := range m.Sources {
		if !comparisonDigest(after) {
			return fmt.Errorf("v10 invalid source pin")
		}
		if _, ok := e.OriginalSources[name]; ok {
			continue
		}
		if !windowV10ExecutorOnlyPath(name) || e.ChangedFiles[name] != (windowExecutorChangeV10{"", after}) {
			return fmt.Errorf("v10 unreviewed source addition: %s", name)
		}
		changes++
	}
	if len(e.ChangedFiles) != changes {
		return fmt.Errorf("v10 surplus source changes")
	}
	return nil
}

// Only named executor production files may differ. Tests and documentation have
// separate explicit paths; customer Handler, protocol, driver and fixtures stay pinned.
func windowV10ExecutorOnlyPath(name string) bool {
	if path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
		return false
	}
	if name == "AGENTS.md" || strings.HasPrefix(name, "docs/") || strings.HasPrefix(name, ".superpowers/") {
		return true
	}
	if strings.HasPrefix(name, "internal/taskbench/") && strings.HasSuffix(name, "_test.go") {
		return true
	}
	if strings.HasPrefix(name, "cmd/full-window-actual/") && strings.HasSuffix(name, "_test.go") {
		return true
	}
	if strings.HasPrefix(name, "eval/behavior-review/full1220-v10-") || strings.HasPrefix(name, "eval/behavior-review/test_full1220_v10_") || strings.HasPrefix(name, "scripts/full1220-v10-") {
		return true
	}
	if strings.HasPrefix(name, "eval/behavior-review/helpers/") && strings.HasSuffix(name, "_v10_test.go.txt") {
		return true
	}
	switch name {
	// Exact epoch-neutral capture executor and synthetic codec vectors.
	case "eval/behavior-review/fixtures/v10-contracts/go-declarations.json", "eval/behavior-review/fixtures/v10-contracts/go-delegated-authority.json", "eval/behavior-review/full1220-capture-input-operator.py", "eval/behavior-review/full1220-capture-input-v1.py", "eval/behavior-review/helpers/full1220_content_projection_input_v1_test.go.txt", "eval/behavior-review/test_full1220_capture_input_operator.py", "eval/behavior-review/test_full1220_capture_input_projection.py", "eval/behavior-review/test_full1220_capture_input_v1.py":
		return true
	case "internal/taskbench/window_actual_ready.go", "internal/taskbench/window_json_shape.go", "internal/taskbench/window_stopped_v2_bundle.go", "internal/taskbench/window_stopped_v4_evidence.go",
		"internal/taskbench/window_stopped_v9.go", "internal/taskbench/window_stopped_v9_bundle.go", "internal/taskbench/window_stopped_v9_pins.go", "internal/taskbench/window_history_read_pass.go",
		"internal/taskbench/window_completion_v10.go", "internal/taskbench/window_scope_v10.go", "internal/taskbench/window_claim_v10.go", "internal/taskbench/window_scope.go", "internal/taskbench/window_claim.go",
		"internal/taskbench/window_actual_v10.go", "internal/taskbench/window_campaign_v10.go", "internal/taskbench/window_executor_equivalence_v10.go", "internal/taskbench/window_actual_v8.go", "internal/taskbench/window_actual_v9.go", "internal/taskbench/window_actual_v2.go", "internal/taskbench/window_actual_v7.go", "internal/taskbench/window_actual_session.go", "internal/taskbench/window_collector.go", "internal/taskbench/window_campaign.go", "internal/taskbench/window_campaign_v8.go", "cmd/full-window-actual/main.go":
		return true
	}
	return false
}
