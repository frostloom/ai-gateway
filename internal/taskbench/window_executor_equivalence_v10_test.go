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
