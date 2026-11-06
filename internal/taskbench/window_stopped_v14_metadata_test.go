package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Admission fixtures below are synthetic private pass states, never historical
// producer proofs. They must not read or create anything in an original root.
func TestWindowStoppedV14MetadataMeasuredLegacyBoundary(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, path := range []string{windowStoppedV14Root + "/records/new", windowStoppedV14SourceRoot + "/source.go", windowStoppedV14InventoryPath} {
			t.Run(path+map[bool]string{false: "/before", true: "/after"}[active], func(t *testing.T) {
				p, e := newHistoryReadPass(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				defer p.Abort()
				p.stoppedV11Metadata = true
				p.stoppedV13Metadata = true
				p.stoppedV14Metadata = active
				p.stats.TranscriptBytes = 49174400
				p.stoppedV11LegacyTranscript = 33554319
				before := p.stoppedV11LegacyTranscript
				e = stoppedV11MetadataCharge(t, p, path, 0)
				if (e == nil) != active {
					t.Fatalf("new156 metadata classification active=%v: %v", active, e)
				}
				if active && (p.stoppedV11LegacyTranscript != before || p.stats.TranscriptBytes <= 49174400) {
					t.Fatal("new156 metadata reset or charged legacy")
				}
			})
		}
	}
}

func stoppedV14MetadataStageFixture(t *testing.T) (*historyReadPass, windowStoppedV13Baseline) {
	t.Helper()
	p, e := newHistoryReadPass(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Abort() })
	p.stoppedV11Metadata = true
	p.stoppedV13Metadata = true
	p.stats.TranscriptBytes = 49174400
	p.stoppedV11LegacyTranscript = 33554319
	p.paths[windowStoppedV13InventoryPath] = &historyReadEntry{digest: windowStoppedV13InventorySHA}
	p.paths[windowStoppedV14InventoryPath] = &historyReadEntry{digest: windowStoppedV14InventorySHA}
	p.roots[windowStoppedV14Root] = true
	p.locks[windowStoppedV13Root+"/campaign/.lock"] = historyLockEntry{epoch: 13}
	id := historyFileIdentity{Dev: 1, Ino: 2}
	p.locks[windowStoppedV14Root+"/campaign/.lock"] = historyLockEntry{epoch: 14, identity: id}
	// This sentinel exercises private admission checks only. The production lease
	// is supplied exclusively by a real WithStoppedLock callback, never this fixture.
	p.stoppedV14MetadataLock = &historyLockLease{file: new(os.File), path: windowStoppedV14Root + "/campaign/.lock", identity: id}
	previous := windowStoppedV13Baseline{base: expectedStoppedV13WindowBaseline(), evidencePath: windowStoppedV13InventoryPath, evidenceSHA256: windowStoppedV13InventorySHA, originalRoot: windowStoppedV13Root}
	return p, previous
}
func TestWindowStoppedV14MetadataFixedStageAndNoReset(t *testing.T) {
	p, old := stoppedV14MetadataStageFixture(t)
	before := p.Stats()
	legacy := p.stoppedV11LegacyTranscript
	if e := p.enableStoppedV14Metadata(old); e != nil {
		t.Fatal(e)
	}
	if !p.stoppedV14Metadata || p.Stats() != before || p.stoppedV11LegacyTranscript != legacy {
		t.Fatal("activation reset accounting")
	}
	if e := p.enableStoppedV14Metadata(old); e == nil {
		t.Fatal("repeated activation admitted")
	}
	if p.Stats() != before || p.stoppedV11LegacyTranscript != legacy {
		t.Fatal("repeated activation reset accounting")
	}
}
func TestWindowStoppedV14MetadataRejectInvalidStage(t *testing.T) {
	for _, mode := range []string{"old-stage", "wrong154", "old-path", "old-sha", "old-root", "old-ref", "new-ref", "old-lock", "new-lock", "inactive-lock", "nil-fd", "wrong-lock-path", "wrong-lock-identity", "unmarked-root", "canceled", "nil-context", "closed", "draining", "legacy-overflow", "total-overflow"} {
		t.Run(mode, func(t *testing.T) {
			p, old := stoppedV14MetadataStageFixture(t)
			switch mode {
			case "old-stage":
				p.stoppedV11Metadata = false
				p.stoppedV13Metadata = false
			case "wrong154":
				old.base.ClaimSHA256["changed"] = "changed"
			case "old-path":
				old.evidencePath = "/foreign"
			case "old-sha":
				old.evidenceSHA256 = windowStoppedV14InventorySHA
			case "old-root":
				old.originalRoot = "/foreign"
			case "old-ref":
				delete(p.paths, windowStoppedV13InventoryPath)
			case "new-ref":
				p.paths[windowStoppedV14InventoryPath].digest = windowStoppedV13InventorySHA
			case "old-lock":
				p.locks[windowStoppedV13Root+"/campaign/.lock"] = historyLockEntry{epoch: 11}
			case "new-lock":
				p.locks[windowStoppedV14Root+"/campaign/.lock"] = historyLockEntry{epoch: 13}
			case "inactive-lock":
				p.stoppedV14MetadataLock = nil
			case "nil-fd":
				p.stoppedV14MetadataLock.file = nil
			case "wrong-lock-path":
				p.stoppedV14MetadataLock.path = "/foreign/.lock"
			case "wrong-lock-identity":
				p.stoppedV14MetadataLock.identity.Ino++
			case "unmarked-root":
				delete(p.roots, windowStoppedV14Root)
			case "canceled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				p.ctx = ctx
			case "nil-context":
				p.ctx = nil
			case "closed":
				p.state = historyPassClosed
			case "draining":
				p.state = historyPassDraining
			case "legacy-overflow":
				p.stoppedV11LegacyTranscript = historyPassTranscriptLimit + 1
			case "total-overflow":
				p.stats.TranscriptBytes = 64<<20 + 1
			}
			before := p.Stats()
			legacy := p.stoppedV11LegacyTranscript
			if e := p.enableStoppedV14Metadata(old); e == nil || p.stoppedV14Metadata {
				t.Fatal("invalid156 stage admitted")
			}
			if p.Stats() != before || p.stoppedV11LegacyTranscript != legacy {
				t.Fatal("rejection reset accounting")
			}
		})
	}
}
func TestWindowStoppedV14MetadataRejectOldForeignAndAliasOverflow(t *testing.T) {
	for _, path := range []string{windowStoppedV10Root + "/records/old", "/foreign", windowStoppedV14Root + "-alias/file", windowStoppedV14Root + "/../foreign", windowStoppedV14Root + "//records/file", windowStoppedV14Root + "/records\\file", filepath.Dir(windowStoppedV14InventoryPath) + "/other.json"} {
		t.Run(path, func(t *testing.T) {
			p, old := stoppedV14MetadataStageFixture(t)
			if e := p.enableStoppedV14Metadata(old); e != nil {
				t.Fatal(e)
			}
			if stoppedV14MetadataPath(path) {
				t.Fatal("foreign/alias classified new156")
			}
			if e := stoppedV11MetadataCharge(t, p, path, 0); e == nil {
				t.Fatal("legacy32 overflow borrowed new156 pool")
			}
		})
	}
	for _, path := range []string{windowStoppedV14Root + "/records/new", windowStoppedV14SourceRoot + "/source.go", windowStoppedV14InventoryPath} {
		if stoppedV13MetadataPath(path) {
			t.Fatal("old154 predicate widened")
		}
	}
}
func TestWindowStoppedV14MetadataExistingCapsAndDefaults(t *testing.T) {
	for _, mode := range []string{"total", "records", "logical", "cache-policy", "default"} {
		t.Run(mode, func(t *testing.T) {
			p, old := stoppedV14MetadataStageFixture(t)
			if e := p.enableStoppedV14Metadata(old); e != nil {
				t.Fatal(e)
			}
			path := windowStoppedV14Root + "/records/new"
			switch mode {
			case "total":
				p.stats.TranscriptBytes = 64 << 20
				if stoppedV11MetadataCharge(t, p, path, 0) == nil {
					t.Fatal("total64 expanded")
				}
			case "records":
				p.stats.Records = historyPassRecordsLimit
				if stoppedV11MetadataCharge(t, p, path, 0) == nil {
					t.Fatal("records expanded")
				}
			case "logical":
				p.stats.Logical = historyPassLogicalLimit
				if p.begin(true) == nil {
					t.Fatal("logical expanded")
				}
			case "cache-policy":
				if !p.retainHistoryBytesLocked(historyReadSpec{kind: historyReadV7File, path: path}, 4) {
					t.Fatal("Root4 cache policy changed")
				}
			case "default":
				p.stoppedV11Metadata = false
				p.stoppedV13Metadata = false
				p.stats.TranscriptBytes = historyPassTranscriptLimit
				if stoppedV11MetadataCharge(t, p, path, 0) == nil {
					t.Fatal("new flag alone expanded default32")
				}
			}
		})
	}
	if historyPassTranscriptLimit != 32<<20 || historyPassCacheLimit != 128<<20 || historyPassEntryLimit != 1<<20 || historyPassRecordsLimit != 65536 || historyPassLogicalLimit != 1048576 {
		t.Fatal("old admission caps changed")
	}
}
func TestWindowStoppedV14MetadataPhysicalEndUnchanged(t *testing.T) {
	for _, mode := range []string{"normal", "bytes", "namespace", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, e := newHistoryReadPass(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Abort()
			p.stoppedV11Metadata = true
			p.stoppedV13Metadata = true
			p.stoppedV14Metadata = true
			root := stoppedEvidenceRoot(t)
			path := filepath.Join(root, "data")
			raw := []byte("data")
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			if e = historyStoppedRoot(p, root); e != nil {
				t.Fatal(e)
			}
			if _, e = historyStoppedV7File(p, path, conversationHash(raw), 4); e != nil {
				t.Fatal(e)
			}
			if e = historyStoppedDir(p, root, map[string]bool{"data": false}); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "bytes":
				os.WriteFile(path, []byte("edit"), 0600)
			case "namespace":
				os.WriteFile(filepath.Join(root, "extra"), nil, 0600)
			case "cancel":
				cancel()
			}
			e = p.Finish()
			if (e == nil) != (mode == "normal") {
				t.Fatalf("End boundary %s: %v", mode, e)
			}
		})
	}
}
