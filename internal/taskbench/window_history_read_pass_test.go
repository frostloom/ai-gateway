package taskbench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func historyTestFile(t *testing.T, raw []byte) historyReadSpec {
	t.Helper()
	path := filepath.Join(stoppedEvidenceRoot(t), "body")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return historyReadSpec{historyReadFile, path, conversationHash(raw), int64(len(raw)) + 1}
}

func historyTestPass(t *testing.T) *historyReadPass {
	t.Helper()
	p, err := newHistoryReadPass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Abort() })
	return p
}

func TestHistoryReadPassFileMutationsAndUnsafeInputs(t *testing.T) {
	for _, change := range []string{"inode", "hardlink", "mode", "body"} {
		t.Run(change, func(t *testing.T) {
			s := historyTestFile(t, []byte("original"))
			p := historyTestPass(t)
			if _, err := p.Read(s); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "inode":
				if err := os.WriteFile(s.path+".new", []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(s.path+".new", s.path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(s.path, s.path+".link"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(s.path, 0400); err != nil {
					t.Fatal(err)
				}
			case "body":
				if err := os.WriteFile(s.path, []byte("changed!"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.Finish(); err == nil {
				t.Fatal("changed dependency accepted")
			}
		})
	}
	for _, bad := range []string{"relative", "alias", "sha", "kind", "bound", "directory"} {
		t.Run(bad, func(t *testing.T) {
			s := historyTestFile(t, []byte("body"))
			p := historyTestPass(t)
			switch bad {
			case "relative":
				s.path = "body"
			case "alias":
				if err := os.Symlink(s.path, s.path+".alias"); err != nil {
					t.Fatal(err)
				}
				s.path += ".alias"
			case "sha":
				s.sha = conversationHash([]byte("wrong"))
			case "kind":
				s.kind = 255
			case "bound":
				s.max = 1
			case "directory":
				s.path = filepath.Dir(s.path)
			}
			if _, err := p.Read(s); err == nil {
				t.Fatal("unsafe leaf accepted")
			}
			if _, err := p.Read(historyTestFile(t, []byte("okay"))); err == nil {
				t.Fatal("failed pass reused")
			}
			if err := p.Finish(); err == nil {
				t.Fatal("failed pass closed")
			}
		})
	}
}

func TestHistoryReadPassKindsBoundsAndStreamsAreSeparate(t *testing.T) {
	s := historyTestFile(t, []byte("same bytes"))
	p := historyTestPass(t)
	for _, kind := range []historyReadKind{historyReadFile, historyReadBytes, historyReadV7File} {
		q := s
		q.kind = kind
		if _, err := p.Read(q); err != nil {
			t.Fatal(err)
		}
	}
	q := s
	q.max++
	if _, err := p.Read(q); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []historyReadKind{historyReadInventoryStream, historyReadExecutableStream} {
		for i := 0; i < 2; i++ {
			if digest, err := p.Stream(kind, s.path, s.sha, s.max); err != nil || digest != s.sha {
				t.Fatal(digest, err)
			}
		}
	}
	if got := p.Stats(); got.FirstPhysical != 8 || got.CacheBytes != 40 {
		t.Fatal(got)
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := p.Stats().EndPhysical; got != 6 {
		t.Fatal("missing kind-specific closure", got)
	}
}

func TestHistoryReadPassEntryThresholdAndCacheBudget(t *testing.T) {
	for _, n := range []int{int(historyPassEntryLimit), int(historyPassEntryLimit) + 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s := historyTestFile(t, bytes.Repeat([]byte("a"), n))
			p := historyTestPass(t)
			for i := 0; i < 2; i++ {
				if b, err := p.Read(s); err != nil || len(b) != n {
					t.Fatal(len(b), err)
				}
			}
			want := uint64(1)
			if n > int(historyPassEntryLimit) {
				want = 2
			}
			if p.Stats().FirstPhysical != want {
				t.Fatal(p.Stats())
			}
			if n > int(historyPassEntryLimit) && p.Stats().CacheBytes != 0 {
				t.Fatal("large file cached")
			}
			if err := p.Finish(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprint("budget", overflow), func(t *testing.T) {
			s := historyTestFile(t, []byte("four"))
			p := historyTestPass(t)
			p.stats.CacheBytes = historyPassCacheLimit - 4
			if overflow {
				p.stats.CacheBytes++
			}
			_, err := p.Read(s)
			if overflow {
				if err == nil {
					t.Fatal("over budget silently fell back")
				}
				if _, e := p.Read(s); e == nil {
					t.Fatal("overflow reused")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHistoryReadPassAdmissionAndTranscriptBudgets(t *testing.T) {
	if !historyBudgetFits(3, 2, 5) || historyBudgetFits(3, 3, 5) || historyBudgetFits(^uint64(0), 2, 5) {
		t.Fatal("overflow arithmetic")
	}
	for _, which := range []string{"logical", "active", "records", "transcript"} {
		t.Run(which, func(t *testing.T) {
			s := historyTestFile(t, []byte("a"))
			p := historyTestPass(t)
			switch which {
			case "logical":
				p.stats.Logical = historyPassLogicalLimit
			case "active":
				p.active = historyPassActiveLimit
			case "records":
				p.stats.Records = historyPassRecordsLimit
			case "transcript":
				p.stats.TranscriptBytes = historyPassTranscriptLimit
			}
			if _, err := p.Read(s); err == nil {
				t.Fatal("limit accepted")
			}
			if which == "active" {
				p.mu.Lock()
				p.active = 0
				p.cond.Broadcast()
				p.mu.Unlock()
			}
			if err := p.Finish(); err == nil {
				t.Fatal("limit did not seal failure")
			}
		})
	}
	p := historyTestPass(t)
	p.mu.Lock()
	charge := uint64(512+len("é")+len("/a")+len("sha")) + uint64(24+len("名"))
	p.stats.TranscriptBytes = historyPassTranscriptLimit - charge
	if err := p.chargeLocked("é", "/a", "sha", uint64(24+len("名"))); err != nil {
		t.Fatal(err)
	}
	if p.stats.TranscriptBytes != historyPassTranscriptLimit {
		t.Fatal("UTF8 fixed charge")
	}
	if err := p.chargeLocked("", "", "", 0); err == nil {
		t.Fatal("plus one transcript accepted")
	}
	p.mu.Unlock()
}

func TestHistoryReadPassBatchSingleflightCancellationAndJoin(t *testing.T) {
	s := historyTestFile(t, []byte("original"))
	p := historyTestPass(t)
	result, err := p.Batch([]historyReadSpec{s, s, s, s})
	if err != nil || len(result) != 4 {
		t.Fatal(err)
	}
	if p.Stats().FirstPhysical != 1 || p.Stats().CacheHits != 3 {
		t.Fatal("no singleflight", p.Stats())
	}
	result[0][0] = 'x'
	if string(result[1]) != "original" {
		t.Fatal("batch clone alias")
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q, err := newHistoryReadPass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		windowStoppedReadSlots <- struct{}{}
	}
	completed := make(chan error, 1)
	go func() { _, e := q.Batch([]historyReadSpec{s, s, s, s}); completed <- e }()
	cancel()
	select {
	case err = <-completed:
	case <-time.After(time.Second):
		t.Fatal("cancelled batch did not join")
	}
	for i := 0; i < 4; i++ {
		<-windowStoppedReadSlots
	}
	if err == nil {
		t.Fatal("cancellation accepted")
	}
	q.mu.Lock()
	active := q.active
	q.mu.Unlock()
	if active != 0 {
		t.Fatal("background worker remains", active)
	}
	if len(windowStoppedReadSlots) != 0 || len(q.leaves) != 0 {
		t.Fatal("slot leak")
	}
	_ = q.Abort()
}

func TestHistoryReadPassConcurrentClones(t *testing.T) {
	s := historyTestFile(t, []byte("original"))
	p := historyTestPass(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := p.Read(s)
			if e == nil {
				if string(b) != "original" {
					e = fmt.Errorf("polluted clone")
				}
				b[0] = 'x'
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if p.Stats().FirstPhysical != 1 {
		t.Fatal(p.Stats())
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryReadPassImmutableDirectoryAndUpgradeDoNotRebase(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			s := historyTestFile(t, []byte("body"))
			root := filepath.Dir(s.path)
			p := historyTestPass(t)
			if !upgrade {
				if err := p.MarkImmutableRoot(root); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := p.Read(s); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "temporary"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(filepath.Join(root, "temporary")); err != nil {
				t.Fatal(err)
			}
			if err = os.Chtimes(root, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if upgrade {
				if err = p.MarkImmutableRoot(root); err == nil {
					t.Fatal("root upgrade rebased changed marker")
				}
			} else if err = p.Finish(); err == nil {
				t.Fatal("immutable directory ABA accepted")
			}
		})
	}
}

func TestHistoryReadPassSharedAncestorAndClosureHorizon(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	// Above explicitly marked roots, sibling mutations are intentionally allowed.
	if _, err := p.Read(s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(s.path), "sibling"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	// Closure is a completed observation, not a lock or monitor of future writes.
	if err := os.WriteFile(s.path, []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	if p.state != historyPassClosed {
		t.Fatal("unexpected post-closure monitoring")
	}
	if err := p.Finish(); err == nil {
		t.Fatal("closure reusable")
	}
}

func TestHistoryReadPassNamespaceCallbacksAndEndFreshness(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	calls := 0
	names := map[string]bool{"body": false}
	check := func() error { calls++; _, e := p.Read(s); return e }
	for i := 0; i < 2; i++ {
		if err := p.CheckNamespace(filepath.Dir(s.path), names, check); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("namespace semantic callback cached")
	}
	names["caller-mutation"] = false
	if err := os.WriteFile(s.path+".extra", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err == nil {
		t.Fatal("End skipped exact namespace")
	}
}

func TestHistoryReadPassLocksNestedLeavesAndPartialCleanup(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	calls := 0
	locks := []string{s.path + ".lock1", s.path + ".lock2"}
	for i, path := range locks {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		check := func() error { calls++; _, e := p.Batch([]historyReadSpec{s, s, s, s}); return e }
		for n := 0; n < 2; n++ {
			if err := p.WithStoppedLock(i+1, path, check); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls != 4 {
		t.Fatal("lock callback skipped")
	}
	occupied, err := openHistoryLock(locks[1])
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Finish(); err == nil {
		t.Fatal("occupied End lock accepted")
	}
	if p.Stats().LocksClosed != 1 {
		t.Fatal("partial closure did not release earlier lock", p.Stats())
	}
	if err = occupied.close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range locks {
		l, e := openHistoryLock(path)
		if e != nil {
			t.Fatal("leaked lock", e)
		}
		if e = l.close(); e != nil {
			t.Fatal(e)
		}
	}
}

func TestHistoryReadPassLockEpochCollision(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	for i := 0; i < 2; i++ {
		path := fmt.Sprintf("%s.lock%d", s.path, i)
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		err := p.WithStoppedLock(1, path, func() error { return nil })
		if (i == 0) != (err == nil) {
			t.Fatal(i, err)
		}
	}
	if err := p.Finish(); err == nil {
		t.Fatal("collision pass closed")
	}
}

func TestHistoryReadPassRootContainment(t *testing.T) {
	if !historyInside("/epoch/input", "/") || !historyInside("/epoch/input", "/epoch") || historyInside("/epoch-sibling", "/epoch") {
		t.Fatal("immutable classification misses root or crosses component")
	}
}

type historyCleanupCancelContext struct {
	context.Context
	cancel context.CancelFunc
	pass   *historyReadPass
}

func (c *historyCleanupCancelContext) Err() error {
	if c.pass != nil && c.pass.Stats().LocksClosed > 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestHistoryReadPassChecksCancellationAfterUnlock(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &historyCleanupCancelContext{Context: base, cancel: cancel}
	p, err := newHistoryReadPass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctx.pass = p
	path := filepath.Join(stoppedEvidenceRoot(t), "lock")
	if err = os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = p.WithStoppedLock(1, path, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = p.Finish(); err == nil {
		t.Fatal("returned success after cleanup cancellation")
	}
	if p.Stats().LocksClosed != 1 {
		t.Fatal("resource not released")
	}
	l, err := openHistoryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.close(); err != nil {
		t.Fatal(err)
	}
	if p.state != historyPassFailed {
		t.Fatal("cancel did not failseal")
	}
}

func TestHistoryReadPassStreamOnlyDependencyAndEmptySHA(t *testing.T) {
	for _, kind := range []historyReadKind{historyReadBytes, historyReadInventoryStream, historyReadExecutableStream} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			s := historyTestFile(t, []byte("body"))
			p := historyTestPass(t)
			if kind == historyReadBytes {
				s.kind = kind
				s.sha = ""
				if _, err := p.Read(s); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := p.Stream(kind, s.path, s.sha, s.max); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(s.path, []byte("edit"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := p.Finish(); err == nil {
				t.Fatal("unanchored/stream input absent from End")
			}
		})
	}
}

func TestHistoryReadPassNamespaceMissingAndType(t *testing.T) {
	for _, change := range []string{"missing", "type"} {
		t.Run(change, func(t *testing.T) {
			s := historyTestFile(t, []byte("body"))
			p := historyTestPass(t)
			if err := p.CheckNamespace(filepath.Dir(s.path), map[string]bool{"body": false}, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(s.path); err != nil {
				t.Fatal(err)
			}
			if change == "type" {
				if err := os.Mkdir(s.path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.Finish(); err == nil {
				t.Fatal("namespace change accepted")
			}
		})
	}
}

func TestHistoryReadPassNamespaceExpectedNamesAreOwned(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	names := map[string]bool{"body": false}
	if err := p.CheckNamespace(filepath.Dir(s.path), names, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	delete(names, "body")
	names["other"] = true
	if err := p.Finish(); err != nil {
		t.Fatal("caller changed retained namespace", err)
	}
}

func TestHistoryReadPassFourCallbacksNeverOwnLeafSlots(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p, err := newHistoryReadPass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 4)
	for i := range paths {
		paths[i] = fmt.Sprintf("%s.lock%d", s.path, i)
		if err = os.WriteFile(paths[i], nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	ready := make(chan struct{}, 4)
	release := make(chan struct{})
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- p.WithStoppedLock(i+1, paths[i], func() error {
				ready <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				_, e := p.Batch([]historyReadSpec{s, s, s, s})
				return e
			})
		}(i)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Error("four callbacks could not enter")
		}
	}
	close(release)
	wg.Wait()
	close(errs)
	for err = range errs {
		if err != nil {
			t.Fatal("nested callbacks deadlocked or failed", err)
		}
	}
	if err = p.Finish(); err != nil {
		t.Fatal(err)
	}
	if p.Stats().LocksClosed != 4 {
		t.Fatal("End omitted locks", p.Stats())
	}
}

func TestHistoryReadPassImmutableDependencySubdirectory(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(child, "body")
	if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	p := historyTestPass(t)
	if err := p.MarkImmutableRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read(historyReadSpec{historyReadBytes, path, "", 10}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "transient"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(child, "transient")); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err == nil {
		t.Fatal("dependency subdirectory ABA accepted")
	}
}

func TestHistoryReadPassExactAdmissionBoundary(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	p.stats.Logical = historyPassLogicalLimit - 1
	if _, err := p.Read(s); err != nil {
		t.Fatal("last logical request refused", err)
	}
	if p.stats.Logical != historyPassLogicalLimit {
		t.Fatal("bad request count")
	}
	if _, err := p.Read(s); err == nil {
		t.Fatal("logical plus one accepted")
	}
	q := historyTestPass(t)
	q.stats.Records = historyPassRecordsLimit - 1
	q.mu.Lock()
	err := q.chargeLocked("key", "path", "sha", 0)
	q.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	q.mu.Lock()
	err = q.chargeLocked("next", "path", "sha", 0)
	q.mu.Unlock()
	if err == nil {
		t.Fatal("entry plus one accepted")
	}
}

func TestHistoryReadPassSharedAncestorRenameRestoreIsOutsideGuarantee(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	if _, err := p.Read(s); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(s.path)
	if err := os.Rename(root, root+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root+".moved", root); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err != nil {
		t.Fatal("shared ancestor policy unexpectedly claims ABA detection", err)
	}
}

func TestHistoryReadPassFailureUnblocksPhysicalWaiters(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	p := historyTestPass(t)
	for i := 0; i < 4; i++ {
		windowStoppedReadSlots <- struct{}{}
	}
	result := make(chan error, 1)
	go func() { _, err := p.Read(s); result <- err }()
	deadline := time.After(time.Second)
	for {
		p.mu.Lock()
		started := len(p.entries) > 0
		p.mu.Unlock()
		if started {
			break
		}
		select {
		case <-deadline:
			t.Fatal("read did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	p.fail(fmt.Errorf("another dependency failed"))
	early := false
	select {
	case <-result:
		early = true
	case <-time.After(20 * time.Millisecond):
	}
	for i := 0; i < 4; i++ {
		<-windowStoppedReadSlots
	}
	if !early {
		<-result
		t.Fatal("failed pass kept waiting for unrelated physical slot")
	}
}

func TestHistoryReadPassReusesOnlyWithinCallAndClones(t *testing.T) {
	path := filepath.Join(stoppedEvidenceRoot(t), "body")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	spec := historyReadSpec{kind: historyReadFile, path: path, sha: conversationHash([]byte("original")), max: 1024}
	p, err := newHistoryReadPass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.Read(spec)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 'X'
	second, err := p.Read(spec)
	if err != nil || string(second) != "original" {
		t.Fatal("caller mutation polluted pass", err)
	}
	if p.Stats().FirstPhysical != 1 {
		t.Fatalf("same-pass dependency was physically reread: %d", p.Stats().FirstPhysical)
	}
	if err = p.Finish(); err != nil {
		t.Fatal(err)
	}
	if p.Stats().EndPhysical != 1 {
		t.Fatal("closure skipped fresh read", p.Stats())
	}
	if _, err = p.Read(spec); err == nil {
		t.Fatal("closed pass reused")
	}
	next, err := newHistoryReadPass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = next.Read(spec); err != nil {
		t.Fatal(err)
	}
	if next.Stats().FirstPhysical != 1 {
		t.Fatal("cross-call cache")
	}
	if err = next.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryReadPassEndRejectsRestoredBytesWithChangedCtime(t *testing.T) {
	path := filepath.Join(stoppedEvidenceRoot(t), "body")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newHistoryReadPass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Read(historyReadSpec{kind: historyReadBytes, path: path, max: 1024}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = p.Finish(); err == nil {
		t.Fatal("End accepted bytes+mtime restoration")
	}
	if _, err = p.Read(historyReadSpec{kind: historyReadBytes, path: path, max: 1024}); err == nil {
		t.Fatal("failed pass reused")
	}
}

func TestHistoryReadPassAbortWaitsForEndSweep(t *testing.T) {
	path := filepath.Join(stoppedEvidenceRoot(t), "body")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := newHistoryReadPass(context.Background())
	if _, err := p.Read(historyReadSpec{historyReadBytes, path, "", 1024}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		windowStoppedReadSlots <- struct{}{}
	}
	finish := make(chan error, 1)
	go func() { finish <- p.Finish() }()
	deadline := time.After(time.Second)
	for {
		p.mu.Lock()
		sweeping := p.state == historyPassSweeping
		p.mu.Unlock()
		if sweeping {
			break
		}
		select {
		case <-deadline:
			for i := 0; i < 4; i++ {
				<-windowStoppedReadSlots
			}
			t.Fatal("Finish never started sweep")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	abort := make(chan error, 1)
	go func() { abort <- p.Abort() }()
	select {
	case <-abort:
		p.mu.Lock()
		joined := !p.finishing && p.active == 0 && p.entries == nil
		p.mu.Unlock()
		if !joined {
			t.Error("Abort returned before End cleanup joined")
		}
	case <-time.After(time.Second):
		t.Error("Abort did not interrupt End physical-slot wait")
	}
	for i := 0; i < 4; i++ {
		<-windowStoppedReadSlots
	}
	if err := <-finish; err == nil {
		t.Fatal("concurrent abort did not fail closure")
	}
}

func TestHistoryReadPassOriginalKindLimits(t *testing.T) {
	for _, kind := range []historyReadKind{historyReadFile, historyReadBytes, historyReadV7File, historyReadInventoryStream, historyReadExecutableStream} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			limit := int64(1 << 30)
			if kind == historyReadV7File || kind == historyReadInventoryStream {
				limit = 256 << 20
			}
			s := historyTestFile(t, []byte("small"))
			s.kind = kind
			s.max = limit
			p := historyTestPass(t)
			if s.isStream() {
				if _, err := p.Stream(s.kind, s.path, s.sha, s.max); err != nil {
					t.Fatal("exact bound", err)
				}
			} else if _, err := p.Read(s); err != nil {
				t.Fatal("exact bound", err)
			}
			if err := p.Finish(); err != nil {
				t.Fatal(err)
			}
			s.max++
			q := historyTestPass(t)
			var err error
			if s.isStream() {
				_, err = q.Stream(s.kind, s.path, s.sha, s.max)
			} else {
				_, err = q.Read(s)
			}
			if err == nil {
				t.Error("oversized parameter accepted for tiny file")
			}
			if q.Stats().FirstPhysical != 0 {
				t.Error("oversized parameter reached physical reader")
			}
			if _, err = observeHistoryFile(context.Background(), s, false); err == nil {
				t.Error("direct observed core accepted oversized parameter")
			}
			if kind == historyReadV7File {
				if _, err = readWindowStoppedV7File(s.path, s.sha, s.max); err == nil {
					t.Fatal("original V7 reader unexpectedly accepts oversized parameter")
				}
			}
		})
	}
}

func historyCatchPanic(fn func()) (caught any) {
	defer func() { caught = recover() }()
	fn()
	return nil
}

func TestHistoryReadPassSemanticPanicFailsClosed(t *testing.T) {
	for _, kind := range []string{"namespace", "lock"} {
		t.Run(kind, func(t *testing.T) {
			s := historyTestFile(t, []byte("body"))
			p := historyTestPass(t)
			marker := &struct{ label string }{kind}
			lock := s.path + ".lock"
			if err := os.WriteFile(lock, nil, 0600); err != nil {
				t.Fatal(err)
			}
			callback := func() error {
				if _, err := p.Read(s); err != nil {
					t.Fatal(err)
				}
				panic(marker)
			}
			caught := historyCatchPanic(func() {
				if kind == "namespace" {
					_ = p.CheckNamespace(filepath.Dir(s.path), map[string]bool{"body": false, "body.lock": false}, callback)
				} else {
					_ = p.WithStoppedLock(1, lock, callback)
				}
			})
			if caught != marker {
				t.Fatal("panic swallowed or replaced", caught)
			}
			lease, err := openHistoryLock(lock)
			if err != nil {
				t.Fatal("panic leaked lock", err)
			}
			if err = lease.close(); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			failed, active := p.state == historyPassFailed, p.active
			p.mu.Unlock()
			if !failed {
				t.Error("unfinished semantic callback left pass usable")
			}
			if active != 0 || len(p.leaves) != 0 || len(windowStoppedReadSlots) != 0 {
				t.Fatal("panic left active resources", active)
			}
			if err = p.Finish(); err == nil {
				t.Error("recovered semantic panic allowed ClosedSuccess")
			}
			if _, err = p.Read(s); err == nil {
				t.Fatal("panic pass reused")
			}
		})
	}
}

type historyFinishPanicContext struct {
	context.Context
	pass           *historyReadPass
	cleanup, armed bool
	marker         any
}

func (c *historyFinishPanicContext) Err() error {
	if c.armed && c.pass != nil {
		s := c.pass.Stats()
		if c.cleanup && s.LocksClosed > 0 || !c.cleanup && s.EndPhysical > 0 {
			c.armed = false
			panic(c.marker)
		}
	}
	return c.Context.Err()
}

func TestHistoryReadPassFinishPanicNeverClosesSuccess(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		t.Run(fmt.Sprint(cleanup), func(t *testing.T) {
			s := historyTestFile(t, []byte("body"))
			marker := &struct{ cleanup bool }{cleanup}
			ctx := &historyFinishPanicContext{Context: context.Background(), cleanup: cleanup, marker: marker}
			p, err := newHistoryReadPass(ctx)
			if err != nil {
				t.Fatal(err)
			}
			ctx.pass = p
			if _, err = p.Read(s); err != nil {
				t.Fatal(err)
			}
			lock := s.path + ".lock"
			if err = os.WriteFile(lock, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err = p.WithStoppedLock(1, lock, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			ctx.armed = true
			caught := historyCatchPanic(func() { _ = p.Finish() })
			if caught != marker {
				t.Fatal("Finish panic changed", caught)
			}
			lease, err := openHistoryLock(lock)
			if err != nil {
				t.Fatal("Finish panic leaked lock", err)
			}
			if err = lease.close(); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			failed, finishing, cleared := p.state == historyPassFailed, p.finishing, p.entries == nil
			p.mu.Unlock()
			if !failed || finishing || !cleared {
				t.Error("abnormal Finish cleanup not failsealed/joined", failed, finishing, cleared)
			}
			// A failed original implementation can leave finishing=true; avoid an
			// intentionally unbounded second Finish in that RED counterexample.
			if !finishing {
				if err = p.Finish(); err == nil {
					t.Fatal("panic Finish reused")
				}
			}
		})
	}
}

func TestHistoryReadPassTranscript32MiBExactBoundary(t *testing.T) {
	// The old16MiB prototype rejected the real46 transcript before End. Keep
	// the original conservative charge and test the new fixed capacity directly,
	// without allocating payloads or touching historical inputs.
	p := historyTestPass(t)
	key, path, sha := "kind\x00/path\x00digest\x0065536", "/path", "digest"
	charge := uint64(512 + len(key) + len(path) + len(sha))
	p.mu.Lock()
	p.stats.TranscriptBytes = (32 << 20) - charge
	err := p.chargeLocked(key, path, sha, 0)
	used := p.stats.TranscriptBytes
	p.mu.Unlock()
	if err != nil || used != 32<<20 {
		t.Fatalf("exact32MiB metadata admission rejected: used=%d err=%v", used, err)
	}
	p.mu.Lock()
	err = p.chargeLocked("next", "/next", "digest", 0)
	failed := p.state == historyPassFailed
	p.mu.Unlock()
	if err == nil || !failed {
		t.Fatal("metadata beyond32MiB did not failseal")
	}
	if err = p.Finish(); err == nil {
		t.Fatal("over-capacity transcript closed successfully")
	}
}

func TestHistoryReadPassTranscriptDeltaKeepsOtherFixedBounds(t *testing.T) {
	p := historyTestPass(t)
	if historyPassTranscriptLimit != 32<<20 || historyPassRecordsLimit != 65536 || historyPassLogicalLimit != 1048576 || historyPassCacheLimit != 128<<20 || historyPassEntryLimit != 1<<20 || cap(p.leaves) != 4 || cap(windowStoppedReadSlots) != 4 {
		t.Fatal("metadata-capacity delta changed another fixed protocol bound")
	}
}
