package taskbench

import (
	"context"
	"fmt"
	"reflect"
)

const windowStoppedV14Root = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full3660-completion-v14-freeze-20261106-r1"
const windowStoppedV14SourceRoot = windowStoppedV14Root + "/regression/source"
const windowStoppedV14InventoryPath = "/Users/a1/.codex/worktrees/eval-window-policy/ai-gateway/.superpowers/sdd/2026-11-06-eval-window-policy/stopped178/descriptor-attempt001/v14-stopped-original-inventory.json"
const windowStoppedV14InventorySHA = "fec84f82af6951488d0886b42855f63ea2614d951f4c9f429836638933ea795c"
const windowStoppedV14ManifestSHA = "c87d0459d10bef36d827f2d8c6845f64d6ac4fc8376bf0911de21e11bae243e8"
const windowStoppedV14AuthorizationSHA = "fe700fc0d08e4d5b75ebc2c8dd2fa9d921c1800e966215dea441fd8da281d6dc"
const windowStoppedV14SourceSHA = "272f88edded55dbc35964a1284e73310f31187d486c079fb398015703dfb423f"
const windowStoppedV14BinarySHA = "ac969646d82701ef6e687b19a0c714426704c67332e2afdb466673d2481969dc"
const windowStoppedV14PublicKey = "40db4bca650631411333b5188958685bd960088937f0135ec91a0e96042b66e0"
const windowStoppedV14ClaimSHA = "0d4e4867c9dbe91153a9e6ce6a7a013cc018c03be7f88bf6948b51413a1ff450"
const windowStoppedV14ClaimKey = "full3660-v14-r1-20261106/window-000001"

// Incomplete stopped DATA only; it implements no execution interface.
type windowStoppedV14Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV13Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func expectedStoppedV14WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV13WindowBaseline()
	b.ClaimSHA256 = cloneWindowHistoricalClaims(b.ClaimSHA256)
	b.PermanentModel += 181782
	b.PermanentJEV += 3811
	b.ActualModelCalls += 8
	b.ActualJEVCalls += 2
	b.RemainingModel = b.TotalModel - b.PermanentModel
	b.RemainingJEV = b.TotalJEV - b.PermanentJEV
	b.ClaimSHA256[windowStoppedV14ClaimKey] = windowStoppedV14ClaimSHA
	return b
}
func (s windowStoppedV14Baseline) predecessorRoots() []string {
	return append(s.previous.predecessorRoots(), s.originalRoot)
}
func freezeWindowStoppedV14Predecessor(s windowStoppedV13Baseline) windowStoppedV13Baseline {
	s.base.ClaimSHA256 = cloneWindowHistoricalClaims(s.base.ClaimSHA256)
	s.previous = freezeWindowStoppedV13Predecessor(s.previous)
	return s
}
func loadWindowStoppedV14BaselineContext(ctx context.Context, path, sha string) (windowStoppedV14Baseline, error) {
	if ctx == nil {
		return windowStoppedV14Baseline{}, fmt.Errorf("stopped178 context required")
	}
	if e := ctx.Err(); e != nil {
		return windowStoppedV14Baseline{}, e
	}
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV14Baseline, error) {
		return loadWindowStoppedV14BaselineWithPass(p, path, sha)
	})
}
func loadWindowStoppedV14BaselineWithPass(p *historyReadPass, path, sha string) (out windowStoppedV14Baseline, err error) {
	if p == nil || p.ctx == nil {
		return out, fmt.Errorf("stopped178 pass required")
	}
	if e := p.ctx.Err(); e != nil {
		return out, e
	}
	if path != windowStoppedV14InventoryPath || sha != windowStoppedV14InventorySHA {
		return out, fmt.Errorf("fixed178 descriptor required")
	}
	raw, e := historyStoppedV7File(p, path, sha, 4<<20)
	if e != nil {
		return out, e
	}
	var inv windowStoppedV11Inventory
	if decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "v14-stopped-original-inventory.v1" || inv.Root != windowStoppedV14Root || inv.Count != 2703 || len(inv.Files) != 2703 || inv.Bytes != 150885902 || inv.Normal || inv.Remote {
		return out, fmt.Errorf("fixed178 inventory")
	}
	previous, e := loadWindowStoppedV13BaselineWithPass(p, windowStoppedV13InventoryPath, windowStoppedV13InventorySHA)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(previous.base, expectedStoppedV13WindowBaseline()) {
		return out, fmt.Errorf("concrete177 required")
	}
	frozen := freezeWindowStoppedV14Predecessor(previous)
	for _, root := range previous.predecessorRoots() {
		if !windowPayloadDisjoint(root, inv.Root) {
			return out, fmt.Errorf("crossed predecessor")
		}
	}
	if e = historyStoppedRoot(p, inv.Root); e != nil {
		return out, e
	}
	e = historyStoppedLock(p, 14, inv.Root+"/campaign/.lock", func() error {
		if e := p.enableStoppedV14Metadata(previous); e != nil {
			return e
		}
		files, e := readWindowStoppedV14Inventory(p, inv)
		if e != nil {
			return e
		}
		var m windowActualManifest
		if conversationHash(files["actual-manifest.json"]) != windowStoppedV14ManifestSHA || decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil || validateWindowStoppedV14Manifest(m, previous) != nil {
			return fmt.Errorf("fixed14 manifest")
		}
		if e = readWindowStoppedV14Signatures(m, files); e != nil {
			return e
		}
		if e = readWindowStoppedV14CompanionRefs(p, m, files); e != nil {
			return e
		}
		if e = readWindowStoppedV14SourceInventory(p, m, inv); e != nil {
			return e
		}
		base, e := readWindowStoppedV14Bundle(inv.Root, m, previous.base, files)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(base, expectedStoppedV14WindowBaseline()) {
			return fmt.Errorf("derived178 mismatch")
		}
		out = windowStoppedV14Baseline{base, previous, path, sha, inv.Root, previous.historicalDir}
		return nil
	})
	if e != nil {
		return windowStoppedV14Baseline{}, e
	}
	if !reflect.DeepEqual(frozen, previous) {
		return windowStoppedV14Baseline{}, fmt.Errorf("predecessor changed")
	}
	if e = p.ctx.Err(); e != nil {
		return windowStoppedV14Baseline{}, e
	}
	return out, nil
}
func validateWindowStoppedV14Manifest(m windowActualManifest, p windowStoppedV13Baseline) error {
	if m.Schema != "full-window-actual-ready.v14" || m.EvidenceKind != "actual" || m.ArchiveCommit != "aa84bb5e4cb56ae847f6a1649aa60ef1a77d21f3" || m.ArchiveTree != "077b52e02e4b00e9fe4b8165a626854ad2ee92b0" || m.SourceRoot != windowStoppedV14SourceRoot || m.SessionRoot != windowStoppedV14Root+"/records" || m.CampaignRoot != windowStoppedV14Root+"/campaign" || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != (windowActualFile{windowStoppedV13InventoryPath, windowStoppedV13InventorySHA}) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Limits != windowV4ExpectedLimits() || len(m.Sources) != 2547 || windowV8DeclarationSHA(m.Sources) != windowStoppedV14SourceSHA || m.BuildAttestationPublicKey != windowStoppedV14PublicKey || validateWindowV14ExecutionShape(m.Scope) != nil || windowV8DeclarationSHA(m.Scope) != m.ScopeSHA256 || validateWindowV11SpoolBinding(m) != nil {
		return fmt.Errorf("stopped14 manifest binding")
	}
	return nil
}
func (p *historyReadPass) stoppedV14LockAllowed(epoch int, path string) bool {
	if epoch != 14 || path != windowStoppedV14Root+"/campaign/.lock" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	old, ok := p.locks[windowStoppedV13Root+"/campaign/.lock"]
	return p.stoppedV13Metadata && ok && old.epoch == 13 && p.roots[windowStoppedV14Root]
}
