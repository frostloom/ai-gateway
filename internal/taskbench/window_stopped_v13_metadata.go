package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// This changes classification only within the existing64MiB transcript pool.
// Legacy32MiB, total/record/logical/cache limits and all old paths are unchanged.
func stoppedV13MetadataPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.Contains(path, "\\") && (path == windowStoppedV13InventoryPath || historyInside(path, windowStoppedV13Root))
}

// Only the new177 constructor calls this, inside its genuine Root4 lock after
// concrete156 has completed. Existing charges are retained without a reset.
func (p *historyReadPass) enableStoppedV13Metadata(previous windowStoppedV12Baseline) error {
	if p == nil {
		return fmt.Errorf("stopped177 metadata stage rejected")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	oldEvidence, newEvidence := p.paths[windowStoppedV12InventoryPath], p.paths[windowStoppedV13InventoryPath]
	oldLock, oldHeld := p.locks[windowStoppedV12Root+"/campaign/.lock"]
	lock, registered := p.locks[windowStoppedV13Root+"/campaign/.lock"]
	live := p.stoppedV13MetadataLock
	if p.ctx == nil || p.ctx.Err() != nil || p.state != historyPassOpen || !p.stoppedV12Metadata || p.stoppedV13Metadata || p.stoppedV11LegacyTranscript > historyPassTranscriptLimit || p.stats.TranscriptBytes > 64<<20 || oldEvidence == nil || oldEvidence.digest != windowStoppedV12InventorySHA || newEvidence == nil || newEvidence.digest != windowStoppedV13InventorySHA || !oldHeld || oldLock.epoch != 12 || !p.roots[windowStoppedV13Root] || !registered || lock.epoch != 13 || live == nil || live.file == nil || live.path != windowStoppedV13Root+"/campaign/.lock" || live.identity != lock.identity || previous.evidencePath != windowStoppedV12InventoryPath || previous.evidenceSHA256 != windowStoppedV12InventorySHA || previous.originalRoot != windowStoppedV12Root || !reflect.DeepEqual(previous.base, expectedStoppedV12WindowBaseline()) {
		return p.failLocked(fmt.Errorf("stopped177 metadata stage rejected"))
	}
	p.stoppedV13Metadata = true
	return nil
}
