package taskbench

import (
	"errors"
	"testing"
)

func TestHistoryEndAlreadyObservedPanicSurvivesEarlierFileError(t *testing.T) {
	marker := new(int)
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("already observed worker panic lost: %v", got)
		}
	}()
	_ = checkHistoryEndResults([]historyEndResult{{err: errors.New("earlier file changed")}, {panicked: true, panicValue: marker}})
}

func TestHistoryEndAlreadyObservedGoexitSurvivesEarlierFileError(t *testing.T) {
	done := make(chan struct{})
	returned := make(chan bool, 1)
	go func() {
		defer close(done)
		_ = checkHistoryEndResults([]historyEndResult{{err: errors.New("earlier file changed")}, {panicked: true, goexit: true}})
		returned <- true
	}()
	<-done
	select {
	case <-returned:
		t.Fatal("already observed worker Goexit became an ordinary error")
	default:
	}
}
