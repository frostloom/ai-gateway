package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

const windowStoppedV12Root = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full3660-completion-v12-freeze-20261104-r4"
const windowStoppedV12SourceRoot = windowStoppedV12Root + "/regression/source"
const windowStoppedV12InventoryPath = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-v12-stopped156-20261105/v12-stopped-original-inventory.json"
const windowStoppedV12InventorySHA = "5a3e76d6de7ecf9923563f59e67a29df4a03e1015c47fa01121bdb58a2dc4cc5"
const windowStoppedV12ManifestSHA = "385a297db6144cc35dd078bc3976832637b304e60e9364d5872ba3132938bb84"
const windowStoppedV12AuthorizationSHA = "5c7f43e5856e32f9f62e3c0376c504de6d2a33f6591668166de647b3f480cb04"
const windowStoppedV12SourceSHA = "773b903eb506ab82609f4fd833867a82a7dd92382734ae9532375c106e46aa61"

// Fixed stopped DATA only. No owner, source-replay, scoring or release API.
type windowStoppedV12Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV11Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

var windowStoppedV12ClaimHashes = [...]string{
	"00eac245507f785d5263a6be89654fccc5eb4e09d9468525bbc22830a6c8bd60",
	"f66f8b249372bdd767ebd0fbdf486ac8ab8f6e1035ada2946c3e54f08a7d9839",
}

func expectedStoppedV12WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV11WindowBaseline()
	b.PermanentModel += 353366
	b.PermanentJEV += 7541
	b.RemainingModel = b.TotalModel - b.PermanentModel
	b.RemainingJEV = b.TotalJEV - b.PermanentJEV
	b.ActualModelCalls += 32
	b.ActualJEVCalls += 8
	for i, sha := range windowStoppedV12ClaimHashes {
		b.ClaimSHA256[fmt.Sprintf("full3660-v12-r4-20261105/window-%06d", i+1)] = sha
	}
	return b
}
func (s windowStoppedV12Baseline) predecessorRoots() []string {
	return append(s.previous.predecessorRoots(), s.originalRoot)
}
func freezeWindowStoppedV12Predecessor(s windowStoppedV11Baseline) windowStoppedV11Baseline {
	s.base.ClaimSHA256 = cloneWindowHistoricalClaims(s.base.ClaimSHA256)
	s.previous = freezeWindowStoppedV11Predecessor(s.previous)
	return s
}
func loadWindowStoppedV12BaselineContext(ctx context.Context, path, sha string) (windowStoppedV12Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV12Baseline, error) {
		return loadWindowStoppedV12BaselineWithPass(p, path, sha)
	})
}
func loadWindowStoppedV12BaselineWithPass(p *historyReadPass, path, sha string) (out windowStoppedV12Baseline, err error) {
	reject := func(e any) (windowStoppedV12Baseline, error) {
		return windowStoppedV12Baseline{}, fmt.Errorf("stopped v12 DATA rejected: %v", e)
	}
	if p == nil || path != windowStoppedV12InventoryPath || sha != windowStoppedV12InventorySHA {
		return reject("fixed156 inventory required")
	}
	raw, err := historyStoppedV7File(p, path, sha, 4<<20)
	var inv windowStoppedV11Inventory
	if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "v12-stopped-original-inventory.v1" || inv.Root != windowStoppedV12Root || inv.Count != 323 || len(inv.Files) != inv.Count || inv.Bytes != 4159513 || inv.Normal || inv.Remote {
		return reject("fixed Root4 inventory")
	}
	previous, err := loadWindowStoppedV11BaselineWithPass(p, windowStoppedV11InventoryPath, windowStoppedV11InventorySHA)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV11WindowBaseline()) {
		return reject(fmt.Sprintf("concrete154: %v", err))
	}
	frozen := freezeWindowStoppedV12Predecessor(previous)
	for _, root := range previous.predecessorRoots() {
		if !windowPayloadDisjoint(root, inv.Root) {
			return reject("crossed predecessor")
		}
	}
	if err = historyStoppedRoot(p, inv.Root); err != nil {
		return reject(err)
	}
	err = historyStoppedLock(p, 12, inv.Root+"/campaign/.lock", func() error {
		if e := p.enableStoppedV12Metadata(previous); e != nil {
			return e
		}
		files, e := readWindowStoppedV11Inventory(p, inv.Root, inv)
		if e != nil {
			return e
		}
		var m windowActualManifest
		if conversationHash(files["actual-manifest.json"]) != windowStoppedV12ManifestSHA || decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil || validateWindowStoppedV12Manifest(m, previous) != nil {
			return fmt.Errorf("fixed Root4 manifest")
		}
		if e = readWindowStoppedV12Signatures(m, files); e != nil {
			return e
		}
		if e = readWindowStoppedV12CompanionRefs(p, m, files); e != nil {
			return e
		}
		if e = readWindowStoppedV12SourcesWithPass(p, m.SourceRoot, m.Sources, true); e != nil {
			return e
		}
		base, e := readWindowStoppedV12Bundle(inv.Root, m, previous.base, files)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(base, expectedStoppedV12WindowBaseline()) {
			return fmt.Errorf("derived156 mismatch")
		}
		out = windowStoppedV12Baseline{base, previous, path, sha, inv.Root, previous.historicalDir}
		return nil
	})
	if err != nil {
		return reject(err)
	}
	if !reflect.DeepEqual(frozen, previous) || p.ctx.Err() != nil {
		return reject("predecessor changed or canceled")
	}
	return out, nil
}
func validateWindowStoppedV12Manifest(m windowActualManifest, p windowStoppedV11Baseline) error {
	sources, _ := json.Marshal(m.Sources)
	if m.Schema != "full-window-actual-ready.v12" || m.EvidenceKind != "actual" || m.ArchiveCommit != "42d8ef8e5ca2471b18800e50f8e7a57fbcfdd47f" || m.ArchiveTree != "949855b726121fb40001525b5fc59afa6b5f3078" || m.SourceRoot != windowStoppedV12SourceRoot || m.SessionRoot != windowStoppedV12Root+"/records" || m.CampaignRoot != windowStoppedV12Root+"/campaign" || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != (windowActualFile{windowStoppedV11InventoryPath, windowStoppedV11InventorySHA}) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Limits != windowV4ExpectedLimits() || len(m.Sources) != 2452 || conversationHash(sources) != windowStoppedV12SourceSHA || validateWindowV12ExecutionShape(m.Scope) != nil || windowV8DeclarationSHA(m.Scope) != m.ScopeSHA256 || validateWindowV11SpoolBinding(m) != nil {
		return fmt.Errorf("stopped Root4 manifest identity")
	}
	return nil
}

// The only newly admitted lock is the fixed Root4 path, after the genuine154
// constructor has registered its last predecessor lock in this same pass.
func (p *historyReadPass) stoppedV12LockAllowed(epoch int, path string) bool {
	if epoch != 12 || path != windowStoppedV12Root+"/campaign/.lock" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	old, ok := p.locks[windowStoppedV11Root+"/campaign/.lock"]
	return p.stoppedV11Metadata && ok && old.epoch == 11 && p.roots[windowStoppedV12Root]
}
