package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryV11StoppedEpoch10Lock(t *testing.T) {
	for _, mode := range []string{"normal", "occupied-end", "collision", "changed-epoch", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			lock := filepath.Join(root, ".lock")
			os.WriteFile(lock, nil, 0600)
			p := historyTestPass(t)
			if e := p.WithStoppedLock(10, lock, func() error { return nil }); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "normal":
				if e := p.Finish(); e != nil {
					t.Fatal(e)
				}
				if p.Stats().LocksClosed != 1 {
					t.Fatal("10 missing End check")
				}
			case "occupied-end":
				lease, e := openHistoryLock(lock)
				if e != nil {
					t.Fatal(e)
				}
				defer lease.close()
				if e = p.Finish(); e == nil {
					t.Fatal("End ignored epoch10 lock")
				}
			case "collision":
				other := filepath.Join(root, "other")
				os.WriteFile(other, nil, 0600)
				if e := p.WithStoppedLock(10, other, func() error { return nil }); e == nil {
					t.Fatal("collision accepted")
				}
			case "changed-epoch":
				if e := p.WithStoppedLock(9, lock, func() error { return nil }); e == nil {
					t.Fatal("path epoch changed")
				}
			case "canceled":
				c, cancel := context.WithCancel(context.Background())
				q, e := newHistoryReadPass(c)
				if e != nil {
					t.Fatal(e)
				}
				defer q.Abort()
				cancel()
				if e = q.WithStoppedLock(10, lock, func() error { return nil }); e == nil {
					t.Fatal("canceled10 accepted")
				}
			}
		})
	}
	p := historyTestPass(t)
	if e := p.WithStoppedLock(11, "/not-opened", func() error { return nil }); e == nil {
		t.Fatal("future11 lock accepted")
	}
}
