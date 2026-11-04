package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Unit fixture for the admission stage only, never a physical predecessor proof.
// Production reaches this stage exclusively after its real104 loader returns.
func stoppedV11MetadataStageFixture(t *testing.T) (*historyReadPass, windowStoppedV10Baseline) {
	t.Helper()
	p, e := newHistoryReadPass(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Abort() })
	oldPath := windowStoppedV10Root + "/stopped-v10-original-inventory.json"
	p.paths[oldPath] = &historyReadEntry{digest: windowV11BaselineSHA}
	p.paths[windowStoppedV11InventoryPath] = &historyReadEntry{digest: windowStoppedV11InventorySHA}
	p.locks[windowStoppedV10Root+"/campaign/.lock"] = historyLockEntry{epoch: 10}
	previous := windowStoppedV10Baseline{base: expectedStoppedV10WindowBaseline(), evidencePath: oldPath, evidenceSHA256: windowV11BaselineSHA, originalRoot: windowStoppedV10Root}
	return p, previous
}
func stoppedV11MetadataCharge(t *testing.T, p *historyReadPass, path string, extra uint64) error {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chargeLocked("unit", path, "", extra)
}
func TestWindowStoppedV11MetadataDefaultOldLimit(t *testing.T) {
	p, e := newHistoryReadPass(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer p.Abort()
	p.stats.TranscriptBytes = historyPassTranscriptLimit
	if e = stoppedV11MetadataCharge(t, p, windowStoppedV11Root+"/records/new", 0); e == nil {
		t.Fatal("ordinary pass borrowed new154 allowance")
	}
	if historyPassTranscriptLimit != 32<<20 {
		t.Fatal("old global limit changed")
	}
}
func TestWindowStoppedV11MetadataFixedNewRootsAndLegacyCeiling(t *testing.T) {
	for _, path := range []string{windowStoppedV11Root + "/records/new", windowStoppedV11SourceRoot + "/source.go", windowStoppedV11InventoryPath} {
		t.Run(path, func(t *testing.T) {
			p, old := stoppedV11MetadataStageFixture(t)
			p.stats.TranscriptBytes = historyPassTranscriptLimit
			if e := p.enableStoppedV11Metadata(old); e != nil {
				t.Fatal(e)
			}
			if e := stoppedV11MetadataCharge(t, p, path, 0); e != nil {
				t.Fatal(e)
			}
			if p.stats.TranscriptBytes <= historyPassTranscriptLimit {
				t.Fatal("new metadata not admitted")
			}
		})
	}
	for _, path := range []string{windowStoppedV10Root + "/records/old", "/foreign", windowStoppedV11Root + "-alias/file", windowStoppedV11SourceRoot + "-alias/file", filepath.Dir(windowStoppedV11InventoryPath) + "/other.json", windowStoppedV11Root + "/../foreign"} {
		t.Run(path, func(t *testing.T) {
			p, old := stoppedV11MetadataStageFixture(t)
			p.stats.TranscriptBytes = historyPassTranscriptLimit
			if e := p.enableStoppedV11Metadata(old); e != nil {
				t.Fatal(e)
			}
			if e := stoppedV11MetadataCharge(t, p, path, 0); e == nil {
				t.Fatal("legacy/foreign overflow borrowed new quota")
			}
		})
	}
}
func TestWindowStoppedV11MetadataTotalAndNoReset(t *testing.T) {
	p, old := stoppedV11MetadataStageFixture(t)
	p.stats.TranscriptBytes = historyPassTranscriptLimit - 1
	if e := p.enableStoppedV11Metadata(old); e != nil {
		t.Fatal(e)
	}
	p.stats.TranscriptBytes = 64 << 20
	if e := stoppedV11MetadataCharge(t, p, windowStoppedV11Root+"/records/new", 0); e == nil {
		t.Fatal("new total64MiB overflow accepted")
	}
	p, old = stoppedV11MetadataStageFixture(t)
	p.stats.TranscriptBytes = 123
	if e := p.enableStoppedV11Metadata(old); e != nil {
		t.Fatal(e)
	}
	if e := stoppedV11MetadataCharge(t, p, "/foreign", 0); e != nil {
		t.Fatal(e)
	}
	before := p.stats
	if e := p.enableStoppedV11Metadata(old); e == nil {
		t.Fatal("repeated activation reset budget")
	}
	if p.stats != before {
		t.Fatal("repeated activation rewrote accounting")
	}
}
func TestWindowStoppedV11MetadataRejectInvalidStage(t *testing.T) {
	for _, mode := range []string{"old", "oldpin", "newpin", "lock", "canceled", "closed", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			p, old := stoppedV11MetadataStageFixture(t)
			switch mode {
			case "old":
				old.base = expectedStoppedV9WindowBaseline()
			case "oldpin":
				delete(p.paths, old.evidencePath)
			case "newpin":
				delete(p.paths, windowStoppedV11InventoryPath)
			case "lock":
				delete(p.locks, windowStoppedV10Root+"/campaign/.lock")
			case "canceled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				p.ctx = ctx
			case "closed":
				p.state = historyPassClosed
			case "overflow":
				p.stats.TranscriptBytes = historyPassTranscriptLimit + 1
			}
			if e := p.enableStoppedV11Metadata(old); e == nil {
				t.Fatal("invalid stage activated")
			}
		})
	}
}
func TestWindowStoppedV11MetadataRecordsUnchanged(t *testing.T) {
	p, old := stoppedV11MetadataStageFixture(t)
	if e := p.enableStoppedV11Metadata(old); e != nil {
		t.Fatal(e)
	}
	p.stats.Records = historyPassRecordsLimit
	if e := stoppedV11MetadataCharge(t, p, windowStoppedV11Root+"/records/new", 0); e == nil {
		t.Fatal("record limit expanded")
	}
}

func TestWindowStoppedV11MetadataModeRetainsPhysicalEnd(t *testing.T) {
	for _, mode := range []string{"normal", "mutated", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, e := newHistoryReadPass(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			// Unit-test the pass after its private stage transition; no historical token
			// or production baseline is constructed by this temporary-file fixture.
			p.stoppedV11Metadata = true
			root := stoppedEvidenceRoot(t)
			path := filepath.Join(root, "data")
			raw := []byte("data")
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = historyStoppedV7File(p, path, conversationHash(raw), 4); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "mutated":
				os.WriteFile(path, []byte("edit"), 0600)
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
func TestWindowStoppedV11MetadataLogicalCacheLimitsUnchanged(t *testing.T) {
	p, old := stoppedV11MetadataStageFixture(t)
	if e := p.enableStoppedV11Metadata(old); e != nil {
		t.Fatal(e)
	}
	p.stats.Logical = historyPassLogicalLimit
	if e := p.begin(true); e == nil {
		t.Fatal("logical limit expanded")
	}
	if historyPassCacheLimit != 128<<20 || historyPassRecordsLimit != 65536 || historyPassLogicalLimit != 1048576 {
		t.Fatal("unrelated limits changed")
	}
}

func TestWindowStoppedV11MetadataMixedChargesKeepLegacyBudget(t *testing.T) {
	p, old := stoppedV11MetadataStageFixture(t)
	p.stats.TranscriptBytes = historyPassTranscriptLimit - 1024
	if e := p.enableStoppedV11Metadata(old); e != nil {
		t.Fatal(e)
	}
	if e := stoppedV11MetadataCharge(t, p, "/foreign", 0); e != nil {
		t.Fatal(e)
	}
	legacy := p.stoppedV11LegacyTranscript
	if e := stoppedV11MetadataCharge(t, p, windowStoppedV11Root+"/records/new", 1024); e != nil {
		t.Fatal(e)
	}
	if p.stoppedV11LegacyTranscript != legacy {
		t.Fatal("new metadata altered legacy charge")
	}
	if e := stoppedV11MetadataCharge(t, p, "/foreign", 0); e == nil {
		t.Fatal("new charge replenished legacy budget")
	}
}
func TestWindowStoppedV11UncachedFixedRootPolicy(t *testing.T) {
	p, old := stoppedV11MetadataStageFixture(t)
	for _, active := range []bool{false, true} {
		if active {
			if e := p.enableStoppedV11Metadata(old); e != nil {
				t.Fatal(e)
			}
		}
		for _, path := range []string{windowStoppedV11Root + "/records/original", windowStoppedV10Root + "/records/old", windowStoppedV11SourceRoot + "/source.go", "/foreign", windowStoppedV11Root + "-alias/file", windowStoppedV11Root + "/../foreign"} {
			want := !(active && path == windowStoppedV11Root+"/records/original")
			if got := p.retainHistoryBytesLocked(historyReadSpec{kind: historyReadV7File, path: path}, 4); got != want {
				t.Fatalf("retention active=%v path=%s got=%v want=%v", active, path, got, want)
			}
		}
	}
}
func TestWindowStoppedV11UncachedPhysicalEngineAndOldCacheLimit(t *testing.T) {
	for _, mode := range []string{"retained", "uncached", "uncached-End", "old-cap", "logical"} {
		t.Run(mode, func(t *testing.T) {
			p, e := newHistoryReadPass(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			p.stoppedV11Metadata = true
			root := stoppedEvidenceRoot(t)
			path := filepath.Join(root, "data")
			raw := []byte("data")
			// The exact-root policy is tested independently above; this >entry-limit
			// fixture exercises the same uncached physical/End branch portably.
			if mode == "uncached" || mode == "uncached-End" {
				raw = make([]byte, historyPassEntryLimit+1)
			}
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			if mode == "old-cap" {
				p.stats.CacheBytes = historyPassCacheLimit
			}
			if mode == "logical" {
				p.stats.Logical = historyPassLogicalLimit
			}
			_, e = historyStoppedV7File(p, path, conversationHash(raw), int64(len(raw)))
			if mode == "old-cap" || mode == "logical" {
				if e == nil {
					t.Fatal("old limit bypassed under new metadata policy")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = historyStoppedV7File(p, path, conversationHash(raw), int64(len(raw))); e != nil {
				t.Fatal(e)
			}
			stats := p.Stats()
			if mode == "retained" {
				if stats.CacheBytes != 4 || stats.FirstPhysical != 1 || stats.CacheHits != 1 {
					t.Fatal(stats)
				}
			} else if stats.CacheBytes != 0 || stats.FirstPhysical != 2 || stats.CacheHits != 0 {
				t.Fatal(stats)
			}
			if mode == "uncached-End" {
				raw[0] = 1
				os.WriteFile(path, raw, 0600)
			}
			e = p.Finish()
			if (e == nil) != (mode != "uncached-End") {
				t.Fatalf("physical End mode=%s err=%v", mode, e)
			}
		})
	}
}
