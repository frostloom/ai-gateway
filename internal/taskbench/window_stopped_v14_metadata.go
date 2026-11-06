package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// This changes classification only within the existing64MiB transcript pool.
// Legacy32MiB, total/record/logical/cache limits and all old paths are unchanged.
func stoppedV14MetadataPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.Contains(path, "\\") && (path == windowStoppedV14InventoryPath || historyInside(path, windowStoppedV14Root))
}

// Only the new178 constructor calls this, inside its genuine Root14 lock after
// concrete177 has completed. Existing charges are retained without a reset.
func (p *historyReadPass) enableStoppedV14Metadata(previous windowStoppedV13Baseline) error {
	if p == nil {
		return fmt.Errorf("stopped178 metadata stage rejected")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	oldEvidence, newEvidence := p.paths[windowStoppedV13InventoryPath], p.paths[windowStoppedV14InventoryPath]
	oldLock, oldHeld := p.locks[windowStoppedV13Root+"/campaign/.lock"]
	lock, registered := p.locks[windowStoppedV14Root+"/campaign/.lock"]
	live := p.stoppedV14MetadataLock
	if p.ctx == nil || p.ctx.Err() != nil || p.state != historyPassOpen || !p.stoppedV13Metadata || p.stoppedV14Metadata || p.stoppedV11LegacyTranscript > historyPassTranscriptLimit || p.stats.TranscriptBytes > 64<<20 || oldEvidence == nil || oldEvidence.digest != windowStoppedV13InventorySHA || newEvidence == nil || newEvidence.digest != windowStoppedV14InventorySHA || !oldHeld || oldLock.epoch != 13 || !p.roots[windowStoppedV14Root] || !registered || lock.epoch != 14 || live == nil || live.file == nil || live.path != windowStoppedV14Root+"/campaign/.lock" || live.identity != lock.identity || previous.evidencePath != windowStoppedV13InventoryPath || previous.evidenceSHA256 != windowStoppedV13InventorySHA || previous.originalRoot != windowStoppedV13Root || !reflect.DeepEqual(previous.base, expectedStoppedV13WindowBaseline()) {
		return p.failLocked(fmt.Errorf("stopped178 metadata stage rejected"))
	}
	p.stoppedV14Metadata = true
	return nil
}
