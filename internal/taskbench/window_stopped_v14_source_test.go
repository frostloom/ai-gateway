package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStoppedV14NewSourcesFreshAndClosed(t *testing.T) {
	for _, mode := range []string{"normal", "changed", "extra", "missing", "hardlink", "end-bytes", "end-extra", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			os.Mkdir(filepath.Join(root, "sub"), 0700)
			a := filepath.Join(root, "a.go")
			b := filepath.Join(root, "sub/b.go")
			os.WriteFile(a, []byte("package one\n"), 0600)
			os.WriteFile(b, []byte("package two\n"), 0600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, _ := newHistoryReadPass(ctx)
			defer p.Abort()
			sources := map[string]string{"a.go": conversationHash([]byte("package one\n")), "sub/b.go": conversationHash([]byte("package two\n"))}
			switch mode {
			case "changed":
				os.WriteFile(a, []byte("package bad\n"), 0600)
			case "extra":
				os.WriteFile(filepath.Join(root, "sub/extra"), nil, 0600)
			case "missing":
				os.Remove(b)
			case "hardlink":
				os.Link(a, filepath.Join(root, "hardlink"))
			case "cancel":
				cancel()
			}
			e := readWindowStoppedV14SourcesWithPass(p, root, sources, false)
			if mode != "normal" && mode != "end-bytes" && mode != "end-extra" {
				if e == nil {
					t.Fatal("changed source admitted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if p.Stats().CacheBytes != 0 {
				t.Fatal("new source streams cached")
			}
			if mode == "end-bytes" {
				os.WriteFile(a, []byte("package bad\n"), 0600)
			}
			if mode == "end-extra" {
				os.WriteFile(filepath.Join(root, "sub/extra"), nil, 0600)
			}
			e = p.Finish()
			if (e == nil) != (mode == "normal") {
				t.Fatal("fresh End boundary", e)
			}
		})
	}
}
func TestWindowStoppedV14LockAdmissionFixedRootAndPredecessor(t *testing.T) {
	for _, mode := range []string{"normal", "legacy", "unmarked", "missing-lock", "wrong-epoch", "alias", "other-path", "other-epoch"} {
		t.Run(mode, func(t *testing.T) {
			p, _ := newHistoryReadPass(context.Background())
			defer p.Abort()
			p.stoppedV13Metadata = true
			p.roots[windowStoppedV14Root] = true
			p.locks[windowStoppedV13Root+"/campaign/.lock"] = historyLockEntry{epoch: 13}
			path := windowStoppedV14Root + "/campaign/.lock"
			epoch := 14
			switch mode {
			case "legacy":
				p.stoppedV13Metadata = false
			case "unmarked":
				delete(p.roots, windowStoppedV14Root)
			case "missing-lock":
				delete(p.locks, windowStoppedV13Root+"/campaign/.lock")
			case "wrong-epoch":
				p.locks[windowStoppedV13Root+"/campaign/.lock"] = historyLockEntry{epoch: 12}
			case "alias":
				path = windowStoppedV14Root + "/campaign/../campaign/.lock"
			case "other-path":
				path = "/foreign/.lock"
			case "other-epoch":
				epoch = 15
			}
			if p.stoppedV14LockAllowed(epoch, path) != (mode == "normal") {
				t.Fatal("fixed lock admission differs")
			}
		})
	}
}
