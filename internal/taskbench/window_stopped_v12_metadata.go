package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// This changes classification only within the existing64MiB transcript pool.
// Legacy32MiB, total/record/logical/cache limits and all old paths are unchanged.
func stoppedV12MetadataPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.Contains(path, "\\") && (path == windowStoppedV12InventoryPath || historyInside(path, windowStoppedV12Root))
}

// Only the new156 constructor calls this, inside its genuine Root4 lock after
// concrete154 has completed. Existing charges are retained without a reset.
func (p *historyReadPass) enableStoppedV12Metadata(previous windowStoppedV11Baseline) error {
	if p == nil {
		return fmt.Errorf("stopped156 metadata stage rejected")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	oldEvidence, newEvidence := p.paths[windowStoppedV11InventoryPath], p.paths[windowStoppedV12InventoryPath]
	oldLock, oldHeld := p.locks[windowStoppedV11Root+"/campaign/.lock"]
	lock, registered := p.locks[windowStoppedV12Root+"/campaign/.lock"]
	live := p.stoppedV12MetadataLock
	if p.ctx == nil || p.ctx.Err() != nil || p.state != historyPassOpen || !p.stoppedV11Metadata || p.stoppedV12Metadata || p.stoppedV11LegacyTranscript > historyPassTranscriptLimit || p.stats.TranscriptBytes > 64<<20 || oldEvidence == nil || oldEvidence.digest != windowStoppedV11InventorySHA || newEvidence == nil || newEvidence.digest != windowStoppedV12InventorySHA || !oldHeld || oldLock.epoch != 11 || !p.roots[windowStoppedV12Root] || !registered || lock.epoch != 12 || live == nil || live.file == nil || live.path != windowStoppedV12Root+"/campaign/.lock" || live.identity != lock.identity || previous.evidencePath != windowStoppedV11InventoryPath || previous.evidenceSHA256 != windowStoppedV11InventorySHA || previous.originalRoot != windowStoppedV11Root || !reflect.DeepEqual(previous.base, expectedStoppedV11WindowBaseline()) {
		return p.failLocked(fmt.Errorf("stopped156 metadata stage rejected"))
	}
	p.stoppedV12Metadata = true
	return nil
}
