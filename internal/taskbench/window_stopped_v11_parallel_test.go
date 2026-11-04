package taskbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowStoppedV11FourReadsJoinAndInputError(t *testing.T) {
	starts := make(chan int, 4)
	release := make(chan struct{})
	done := make(chan error, 1)
	first := errors.New("input0")
	var active, peak, finished atomic.Int32
	go func() {
		done <- runWindowStoppedV11Four(4, func(i int) error {
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			starts <- i
			<-release
			defer active.Add(-1)
			defer finished.Add(1)
			if i == 0 {
				return first
			}
			if i == 2 {
				return errors.New("input2")
			}
			return nil
		})
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-starts:
		case <-time.After(time.Second):
			close(release)
			<-done
			t.Fatal("fixed four independent readers did not start")
		}
	}
	select {
	case <-done:
		close(release)
		t.Fatal("returned before joined readers")
	default:
	}
	close(release)
	if e := <-done; e != first || finished.Load() != 4 || active.Load() != 0 || peak.Load() != 4 {
		t.Fatalf("join/order e=%v finished=%d peak=%d", e, finished.Load(), peak.Load())
	}
}
func TestWindowStoppedV11FourReadsRejectBounds(t *testing.T) {
	for _, n := range []int{0, 5} {
		called := false
		if e := runWindowStoppedV11Four(n, func(int) error { called = true; return nil }); e == nil || called {
			t.Fatal("unbounded read dispatch")
		}
	}
	if e := runWindowStoppedV11Four(1, nil); e == nil {
		t.Fatal("nil callback")
	}
}
func TestWindowStoppedV11FourSourceStreamsRetainEnd(t *testing.T) {
	for _, mode := range []string{"normal", "changed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, e := newHistoryReadPass(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			names := []string{"a", "b", "c", "d"}
			raw := []byte("source")
			for _, name := range names {
				if e = os.WriteFile(filepath.Join(root, name), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e = runWindowStoppedV11Four(4, func(i int) error {
				_, err := p.Stream(historyReadInventoryStream, filepath.Join(root, names[i]), conversationHash(raw), windowActualSourceMaxBytes)
				return err
			}); e != nil {
				t.Fatal(e)
			}
			if s := p.Stats(); s.FirstPhysical != 4 || s.CacheBytes != 0 {
				t.Fatal(s)
			}
			switch mode {
			case "changed":
				os.WriteFile(filepath.Join(root, "b"), []byte("change"), 0600)
			case "canceled":
				cancel()
			}
			e = p.Finish()
			if (e == nil) != (mode == "normal") {
				t.Fatalf("End mode=%s error=%v", mode, e)
			}
		})
	}
}

func TestWindowStoppedV11InventoryBatchTailAndDuplicate(t *testing.T) {
	for _, mode := range []string{"normal", "duplicate", "End"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			os.Mkdir(filepath.Join(root, "campaign"), 0700)
			os.Mkdir(filepath.Join(root, "records"), 0700)
			var inv windowStoppedV11Inventory
			names := []string{"campaign/.lock", "records/a", "records/b", "records/c", "records/d", "records/e"}
			for _, name := range names {
				path := filepath.Join(root, name)
				raw := []byte(name)
				if e := os.WriteFile(path, raw, 0600); e != nil {
					t.Fatal(e)
				}
				id, e := historyPathIdentity(path, false)
				if e != nil {
					t.Fatal(e)
				}
				inv.Files = append(inv.Files, windowStoppedV11InventoryEntry{Path: name, SHA256: conversationHash(raw), Bytes: int64(len(raw)), Identity: [7]int64{int64(id.Dev), int64(id.Ino), int64(id.Mode.Perm()) | 0100000, int64(id.Links), id.Size, id.Mtime, id.Ctime}})
				inv.Bytes += int64(len(raw))
			}
			inv.Count = len(inv.Files)
			if mode == "duplicate" {
				inv.Files = append(inv.Files[:1], inv.Files...)
				inv.Count++
			}
			p, e := newHistoryReadPass(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			files, e := readWindowStoppedV11Inventory(p, root, inv)
			if mode == "duplicate" {
				if e == nil {
					t.Fatal("duplicate across batch entries accepted")
				}
				return
			}
			if e != nil || len(files) != 6 {
				t.Fatalf("inventory len=%d err=%v", len(files), e)
			}
			if mode == "End" {
				os.WriteFile(filepath.Join(root, "records/e"), []byte("changed"), 0600)
			}
			e = p.Finish()
			if (e == nil) != (mode == "normal") {
				t.Fatalf("batch End mode=%s error=%v", mode, e)
			}
		})
	}
}
func TestWindowStoppedV11FourReadTailAndGoexit(t *testing.T) {
	for n := 1; n <= 4; n++ {
		var count atomic.Int32
		if e := runWindowStoppedV11Four(n, func(int) error { count.Add(1); return nil }); e != nil || count.Load() != int32(n) {
			t.Fatalf("tail %d count=%d err=%v", n, count.Load(), e)
		}
	}
	var done atomic.Int32
	if e := runWindowStoppedV11Four(2, func(i int) error {
		defer done.Add(1)
		if i == 0 {
			runtime.Goexit()
		}
		return nil
	}); e == nil || done.Load() != 2 {
		t.Fatal("Goexit escaped fulljoin failure")
	}
}

func TestWindowStoppedV11PanicJoinsBlockedSiblingAndCallerCleanup(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	lock := filepath.Join(root, "lock")
	if e := os.WriteFile(lock, nil, 0600); e != nil {
		t.Fatal(e)
	}
	p, e := newHistoryReadPass(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer p.Abort()
	started := make(chan struct{})
	panicked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	cleaned := make(chan struct{})
	ordinary := errors.New("lower input ordinary")
	var finished atomic.Int32
	go func() {
		defer close(cleaned)
		defer p.Abort()
		done <- historyStoppedLock(p, 11, lock, func() error {
			return runWindowStoppedV11Four(3, func(i int) error {
				defer finished.Add(1)
				switch i {
				case 0:
					return ordinary
				case 1:
					<-started
					defer close(panicked)
					panic("private worker panic must not escape before join")
				default:
					close(started)
					<-release
					return nil
				}
			})
		})
	}()
	select {
	case <-panicked:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("panic worker did not start")
	}
	select {
	case <-done:
		close(release)
		t.Fatal("panic returned before blocked sibling joined")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	e = <-done
	if e == nil || errors.Is(e, ordinary) || finished.Load() != 3 {
		t.Fatalf("abnormal completion hidden by ordinary error: %v finished=%d", e, finished.Load())
	}
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("caller cleanup did not finish")
	}
	lease, e := openHistoryLock(lock)
	if e != nil {
		t.Fatal("caller historical lock leaked", e)
	}
	if e = lease.close(); e != nil {
		t.Fatal(e)
	}
	p.mu.Lock()
	active := p.active
	state := p.state
	p.mu.Unlock()
	if active != 0 || len(p.leaves) != 0 || len(windowStoppedReadSlots) != 0 || state != historyPassFailed {
		t.Fatalf("abnormal worker leaked resources active=%d state=%d", active, state)
	}
}
func TestWindowStoppedV11ByteBatchNilVersusLegitimateZero(t *testing.T) {
	for _, mode := range []string{"nil", "zero", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			raws, e := readWindowStoppedV11Four(1, func(int) ([]byte, error) {
				switch mode {
				case "nil":
					return nil, nil
				case "goexit":
					runtime.Goexit()
				}
				return []byte{}, nil
			})
			if mode == "zero" {
				if e != nil || len(raws) != 1 || raws[0] == nil || len(raws[0]) != 0 {
					t.Fatalf("legitimate zero rejected %v", e)
				}
			} else if e == nil || raws != nil {
				t.Fatal("incomplete nil/Goexit bytes accepted")
			}
		})
	}
}
