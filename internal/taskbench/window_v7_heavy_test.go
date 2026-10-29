package taskbench

import (
	"sync"
	"testing"
)

// Only the four new v7 integration tests use these gates. Historical tests keep
// their original scheduling, assertions and deadlines. There is one new DB
// matrix; the other three tests read history or mutate their own temporary copy.
var windowV7HeavyTestSlots = make(chan struct{}, 2)
var windowV7HistoryTestMu sync.Mutex

func parallelWindowV7HeavyTest(t *testing.T, history bool) {
	t.Helper()
	t.Parallel()
	// The original history reader deliberately takes a nonblocking exclusive
	// flock. Serialize these readers before taking a slot so waiting readers
	// cannot consume both slots and prevent the independent DB test from running.
	if history {
		windowV7HistoryTestMu.Lock()
		t.Cleanup(windowV7HistoryTestMu.Unlock)
	}
	windowV7HeavyTestSlots <- struct{}{}
	t.Cleanup(func() { <-windowV7HeavyTestSlots })
}
