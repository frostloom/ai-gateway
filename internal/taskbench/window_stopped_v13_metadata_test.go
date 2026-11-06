package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Admission fixtures below are synthetic private pass states, never historical
// producer proofs. They must not read or create anything in an original root.
func TestWindowStoppedV13MetadataMeasuredLegacyBoundary(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, path := range []string{windowStoppedV13Root + "/records/new", windowStoppedV13SourceRoot + "/source.go", windowStoppedV13InventoryPath} {
			t.Run(path+map[bool]string{false: "/before", true: "/after"}[active], func(t *testing.T) {
				p, e := newHistoryReadPass(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				defer p.Abort()
				p.stoppedV11Metadata = true
				p.stoppedV12Metadata = true
				p.stoppedV13Metadata = active
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

func stoppedV13MetadataStageFixture(t *testing.T) (*historyReadPass, windowStoppedV12Baseline) {
	t.Helper()
	p, e := newHistoryReadPass(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Abort() })
	p.stoppedV11Metadata = true
	p.stoppedV12Metadata = true
	p.stats.TranscriptBytes = 49174400
	p.stoppedV11LegacyTranscript = 33554319
	p.paths[windowStoppedV12InventoryPath] = &historyReadEntry{digest: windowStoppedV12InventorySHA}
	p.paths[windowStoppedV13InventoryPath] = &historyReadEntry{digest: windowStoppedV13InventorySHA}
	p.roots[windowStoppedV13Root] = true
	p.locks[windowStoppedV12Root+"/campaign/.lock"] = historyLockEntry{epoch: 12}
	id := historyFileIdentity{Dev: 1, Ino: 2}
	p.locks[windowStoppedV13Root+"/campaign/.lock"] = historyLockEntry{epoch: 13, identity: id}
	// This sentinel exercises private admission checks only. The production lease
	// is supplied exclusively by a real WithStoppedLock callback, never this fixture.
	p.stoppedV13MetadataLock = &historyLockLease{file: new(os.File), path: windowStoppedV13Root + "/campaign/.lock", identity: id}
	previous := windowStoppedV12Baseline{base: expectedStoppedV12WindowBaseline(), evidencePath: windowStoppedV12InventoryPath, evidenceSHA256: windowStoppedV12InventorySHA, originalRoot: windowStoppedV12Root}
	return p, previous
}
func TestWindowStoppedV13MetadataFixedStageAndNoReset(t *testing.T) {
	p, old := stoppedV13MetadataStageFixture(t)
	before := p.Stats()
	legacy := p.stoppedV11LegacyTranscript
	if e := p.enableStoppedV13Metadata(old); e != nil {
		t.Fatal(e)
	}
	if !p.stoppedV13Metadata || p.Stats() != before || p.stoppedV11LegacyTranscript != legacy {
		t.Fatal("activation reset accounting")
	}
	if e := p.enableStoppedV13Metadata(old); e == nil {
		t.Fatal("repeated activation admitted")
	}
	if p.Stats() != before || p.stoppedV11LegacyTranscript != legacy {
		t.Fatal("repeated activation reset accounting")
	}
}
func TestWindowStoppedV13MetadataRejectInvalidStage(t *testing.T) {
	for _, mode := range []string{"old-stage", "wrong154", "old-path", "old-sha", "old-root", "old-ref", "new-ref", "old-lock", "new-lock", "inactive-lock", "nil-fd", "wrong-lock-path", "wrong-lock-identity", "unmarked-root", "canceled", "nil-context", "closed", "draining", "legacy-overflow", "total-overflow"} {
		t.Run(mode, func(t *testing.T) {
			p, old := stoppedV13MetadataStageFixture(t)
			switch mode {
			case "old-stage":
				p.stoppedV11Metadata = false
				p.stoppedV12Metadata = false
			case "wrong154":
				old.base.ClaimSHA256["changed"] = "changed"
			case "old-path":
				old.evidencePath = "/foreign"
			case "old-sha":
				old.evidenceSHA256 = windowStoppedV13InventorySHA
			case "old-root":
				old.originalRoot = "/foreign"
			case "old-ref":
				delete(p.paths, windowStoppedV12InventoryPath)
			case "new-ref":
				p.paths[windowStoppedV13InventoryPath].digest = windowStoppedV12InventorySHA
			case "old-lock":
				p.locks[windowStoppedV12Root+"/campaign/.lock"] = historyLockEntry{epoch: 11}
			case "new-lock":
				p.locks[windowStoppedV13Root+"/campaign/.lock"] = historyLockEntry{epoch: 12}
			case "inactive-lock":
				p.stoppedV13MetadataLock = nil
			case "nil-fd":
				p.stoppedV13MetadataLock.file = nil
			case "wrong-lock-path":
				p.stoppedV13MetadataLock.path = "/foreign/.lock"
			case "wrong-lock-identity":
				p.stoppedV13MetadataLock.identity.Ino++
			case "unmarked-root":
				delete(p.roots, windowStoppedV13Root)
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
			if e := p.enableStoppedV13Metadata(old); e == nil || p.stoppedV13Metadata {
				t.Fatal("invalid156 stage admitted")
			}
			if p.Stats() != before || p.stoppedV11LegacyTranscript != legacy {
				t.Fatal("rejection reset accounting")
			}
		})
	}
}
func TestWindowStoppedV13MetadataRejectOldForeignAndAliasOverflow(t *testing.T) {
	for _, path := range []string{windowStoppedV10Root + "/records/old", "/foreign", windowStoppedV13Root + "-alias/file", windowStoppedV13Root + "/../foreign", windowStoppedV13Root + "//records/file", windowStoppedV13Root + "/records\\file", filepath.Dir(windowStoppedV13InventoryPath) + "/other.json"} {
		t.Run(path, func(t *testing.T) {
			p, old := stoppedV13MetadataStageFixture(t)
			if e := p.enableStoppedV13Metadata(old); e != nil {
				t.Fatal(e)
			}
			if stoppedV13MetadataPath(path) {
				t.Fatal("foreign/alias classified new156")
			}
			if e := stoppedV11MetadataCharge(t, p, path, 0); e == nil {
				t.Fatal("legacy32 overflow borrowed new156 pool")
			}
		})
	}
	for _, path := range []string{windowStoppedV13Root + "/records/new", windowStoppedV13SourceRoot + "/source.go", windowStoppedV13InventoryPath} {
		if stoppedV12MetadataPath(path) {
			t.Fatal("old154 predicate widened")
		}
	}
}
func TestWindowStoppedV13MetadataExistingCapsAndDefaults(t *testing.T) {
	for _, mode := range []string{"total", "records", "logical", "cache-policy", "default"} {
		t.Run(mode, func(t *testing.T) {
			p, old := stoppedV13MetadataStageFixture(t)
			if e := p.enableStoppedV13Metadata(old); e != nil {
				t.Fatal(e)
			}
			path := windowStoppedV13Root + "/records/new"
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
				p.stoppedV12Metadata = false
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
func TestWindowStoppedV13MetadataPhysicalEndUnchanged(t *testing.T) {
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
			p.stoppedV12Metadata = true
			p.stoppedV13Metadata = true
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
