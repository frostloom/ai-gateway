package taskbench

import (
	"reflect"
	"testing"
)

func TestWindowActualGoCommandEpochAndIsolation(t *testing.T) {
	for _, schema := range []string{"full-window-actual-ready.v1", "full-window-actual-ready.v2", "full-window-actual-ready.v3", "full-window-actual-ready.v4", "full-window-actual-ready.v5", "full-window-actual-ready.v6", "full-window-actual-ready.v7", "full-window-actual-ready.v70", "full-window-actual-ready.v7 ", "", "full-window-scope.v7"} {
		timeout := "-timeout=20m"
		if schema == "full-window-actual-ready.v7" {
			timeout = "-timeout=30m"
		}
		want := []string{"go", "test", "./...", "-count=1", timeout, "-json"}
		got := windowActualGoCommand(schema)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q command: got %v, want %v", schema, got, want)
		}
		got[0] = "changed"
		if !reflect.DeepEqual(windowActualGoCommand(schema), want) {
			t.Errorf("%q returned aliased command", schema)
		}
	}
}
