package taskbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A serial implementation cannot reach four reads while each is waiting for
// release. Always release and join before failing, including the RED run.
func TestStoppedReadBatchStartsFourAndJoins(t *testing.T) {
	entered := make(chan int, 4)
	release := make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		got, err := runWindowStoppedReadBatch4(4, func(i int) ([]byte, error) {
			calls.Add(1)
			entered <- i
			<-release
			return []byte{byte(i)}, nil
		})
		if err == nil {
			for i := range got {
				if len(got[i]) != 1 || got[i][0] != byte(i) {
					err = fmt.Errorf("result order changed at %d", i)
				}
			}
		}
		done <- err
	}()
	started := 0
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
wait:
	for started < 4 {
		select {
		case <-entered:
			started++
		case <-timer.C:
			break wait
		}
	}
	close(release)
	err := <-done
	if started != 4 {
		t.Fatalf("batch did not start four independent reads before release: %d", started)
	}
	if err != nil || calls.Load() != 4 {
		t.Fatal("read lost, repeated or reordered", calls.Load(), err)
	}
}

func TestStoppedReadBatchErrorsWaitForEveryReader(t *testing.T) {
	entered := make(chan int, 4)
	release := [4]chan struct{}{}
	finished := [4]chan struct{}{}
	for i := range release {
		release[i], finished[i] = make(chan struct{}), make(chan struct{})
	}
	first, last := errors.New("first read"), errors.New("last read")
	done := make(chan error, 1)
	go func() {
		_, err := runWindowStoppedReadBatch4(4, func(i int) ([]byte, error) {
			entered <- i
			<-release[i]
			defer close(finished[i])
			if i == 0 {
				return nil, first
			}
			if i == 3 {
				return nil, last
			}
			return []byte{byte(i)}, nil
		})
		done <- err
	}()
	for i := 0; i < 4; i++ {
		<-entered
	}
	close(release[3])
	<-finished[3]
	select {
	case err := <-done:
		for i := 0; i < 3; i++ {
			close(release[i])
		}
		t.Fatal("returned with readers still active", err)
	default:
	}
	for i := 0; i < 3; i++ {
		close(release[i])
	}
	if err := <-done; !errors.Is(err, first) {
		t.Fatal("completion order selected error instead of input order", err)
	}
	for i := range finished {
		select {
		case <-finished[i]:
		default:
			t.Fatal("reader not joined", i)
		}
	}
}

func TestStoppedReadBatchSharedLimitAcrossCalls(t *testing.T) {
	var active, peak, calls atomic.Int32
	done := make(chan error, 2)
	for batch := 0; batch < 2; batch++ {
		go func() {
			_, err := runWindowStoppedReadBatch4(4, func(i int) ([]byte, error) {
				n := active.Add(1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				defer active.Add(-1)
				calls.Add(1)
				time.Sleep(10 * time.Millisecond)
				return []byte{byte(i)}, nil
			})
			done <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if active.Load() != 0 || peak.Load() > 4 || calls.Load() != 8 {
		t.Fatal("global read bound, joining or exactly-once changed", active.Load(), peak.Load(), calls.Load())
	}
}

func TestStoppedReadBatchRejectsInvalidSizeWithoutRead(t *testing.T) {
	for _, n := range []int{0, -1, 5} {
		called := false
		if _, err := runWindowStoppedReadBatch4(n, func(int) ([]byte, error) { called = true; return nil, nil }); err == nil || called {
			t.Fatal("invalid batch invoked reader", n, err)
		}
	}
}

func TestStoppedReadBatchRetainsSafeReadersAndFreshBytes(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	path := filepath.Join(root, "body.json")
	before := []byte(`{"original":true}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	spec := windowStoppedReadSpec{path: path, sha256: conversationHash(before), maxBytes: 65536, kind: windowStoppedReadFile}
	got, err := readWindowStoppedBatch4([]windowStoppedReadSpec{spec})
	if err != nil || len(got) != 1 || string(got[0]) != string(before) {
		t.Fatal("original safe read rejected", err)
	}
	for _, mode := range []string{"changed", "symlink", "missing", "bound", "kind"} {
		t.Run(mode, func(t *testing.T) {
			p := filepath.Join(stoppedEvidenceRoot(t), "body.json")
			if err := os.WriteFile(p, before, 0600); err != nil {
				t.Fatal(err)
			}
			s := spec
			s.path = p
			switch mode {
			case "changed":
				os.WriteFile(p, []byte(`{"original":false}`), 0600)
			case "symlink":
				os.Remove(p)
				os.Symlink(path, p)
			case "missing":
				os.Remove(p)
			case "bound":
				s.maxBytes = 1
			case "kind":
				s.kind = 99
			}
			if _, err := readWindowStoppedBatch4([]windowStoppedReadSpec{s}); err == nil {
				t.Fatal("reader safety weakened", mode)
			}
		})
	}
	if err := os.WriteFile(path, []byte(`{"original":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowStoppedBatch4([]windowStoppedReadSpec{spec}); err == nil {
		t.Fatal("previous call bytes were reused")
	}
}

func TestStoppedReadBatchMixedReadersKeepInputBindings(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	specs := make([]windowStoppedReadSpec, 4)
	for i := range specs {
		raw := []byte(fmt.Sprintf("original-%d", i))
		path := filepath.Join(root, fmt.Sprintf("file-%d", i))
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		specs[i] = windowStoppedReadSpec{path: path, sha256: conversationHash(raw), maxBytes: 65536, kind: windowStoppedReadFile}
		if i%2 == 1 {
			specs[i].kind, specs[i].sha256 = windowStoppedReadBytes, ""
		}
	}
	got, err := readWindowStoppedBatch4(specs)
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range got {
		if string(raw) != fmt.Sprintf("original-%d", i) {
			t.Fatal("reader/path result binding changed", i, string(raw))
		}
	}
}

func TestStoppedReadBatchPinnedFilesKeepTailAndNames(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	pins := map[string]string{}
	for _, name := range []string{"z.json", "b.json", "tail.json", "a.json", "c.json"} {
		raw := []byte("original-" + name)
		if err := os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
		pins[name] = conversationHash(raw)
	}
	got, err := readWindowStoppedFiles(root, pins, 65536)
	if err != nil || len(got) != 5 {
		t.Fatal("pinned file or tail batch lost", len(got), err)
	}
	for name, raw := range got {
		if string(raw) != "original-"+name {
			t.Fatal("sorted batch changed file binding", name, string(raw))
		}
	}
}
