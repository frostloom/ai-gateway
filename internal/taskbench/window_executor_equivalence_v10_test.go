package taskbench

import "testing"

func TestWindowV10ExecutorEquivalenceFinitePaths(t *testing.T) {
	for _, name := range []string{"internal/agent/llm.go", "internal/taskbench/window_owned_driver.go", "internal/taskbench/window_retry_v8.go", "internal/taskbench/window_provider_transport.go", "internal/taskbench/paired.go", "internal/taskbench/definitions/account.yaml", "eval/behavior-review/full1220-v9-window.py", "internal/taskbench/new_customer.go", "../docs/a", "/docs/a"} {
		if windowV10ExecutorOnlyPath(name) {
			t.Fatalf("customer/unreviewed path allowed: %s", name)
		}
	}
	for _, name := range []string{"AGENTS.md", "internal/taskbench/window_actual_v10.go", "internal/taskbench/window_stopped_v9_bundle.go", "internal/taskbench/window_actual_ready.go", "internal/taskbench/window_json_shape.go", "docs/run.md", "eval/behavior-review/full1220-v10-window.py", "eval/behavior-review/test_full1220_v10_owner.py", "eval/behavior-review/helpers/source_v10_test.go.txt"} {
		if !windowV10ExecutorOnlyPath(name) {
			t.Fatalf("reviewed executor path rejected: %s", name)
		}
	}
}
func TestWindowV10ExecutorEquivalenceCannotSelfDeclareOriginalSources(t *testing.T) {
	e := windowExecutorEquivalenceV10{Schema: "full-window-executor-equivalence.v10", OriginalSourcesSHA256: expectedWindowCompletionDeclarationV10().OriginalSourcesSHA256, OriginalSources: map[string]string{"internal/agent/llm.go": "fake"}}
	if validateWindowV10ExecutorEquivalenceData(windowActualManifest{}, e) == nil {
		t.Fatal("unbound original source map accepted")
	}
	if validateWindowV10ExecutorEquivalence(windowActualManifest{Schema: "full-window-actual-ready.v10"}) == nil {
		t.Fatal("missing equivalence accepted")
	}
	if validateWindowV10ExecutorEquivalence(windowActualManifest{Schema: "full-window-actual-ready.v9", ExecutorEquivalence: &windowActualFile{}}) == nil {
		t.Fatal("old epoch equivalence accepted")
	}
}

func TestWindowV10ExistingCaptureAndCodecArtifacts(t *testing.T) {
	for _, name := range []string{"eval/behavior-review/fixtures/v10-contracts/go-declarations.json", "eval/behavior-review/fixtures/v10-contracts/go-delegated-authority.json", "eval/behavior-review/full1220-capture-input-operator.py", "eval/behavior-review/full1220-capture-input-v1.py", "eval/behavior-review/helpers/full1220_content_projection_input_v1_test.go.txt", "eval/behavior-review/test_full1220_capture_input_operator.py", "eval/behavior-review/test_full1220_capture_input_projection.py", "eval/behavior-review/test_full1220_capture_input_v1.py"} {
		if !windowV10ExecutorOnlyPath(name) {
			t.Errorf("required executor-only artifact rejected: %s", name)
		}
	}
	for _, name := range []string{"eval/behavior-review/fixtures/v10-contracts/customer.json", "eval/behavior-review/full1220-capture-input-v2.py", "eval/behavior-review/helpers/full1220_content_projection_unknown_test.go.txt", "internal/agent/llm.go"} {
		if windowV10ExecutorOnlyPath(name) {
			t.Errorf("unreviewed neighbor allowed: %s", name)
		}
	}
}
