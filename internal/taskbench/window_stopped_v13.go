package taskbench

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

const windowStoppedV13Root = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full3660-completion-v13-freeze-20261105-r4"
const windowStoppedV13SourceRoot = windowStoppedV13Root + "/regression/source"
const windowStoppedV13InventoryPath = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-v13-stopped177-20261106/v13-stopped-original-inventory.json"
const windowStoppedV13InventorySHA = "3d5f19325c69165c603a070518d2a4b25faa43a1845323d494866486abe7230f"
const windowStoppedV13ManifestSHA = "22861d4d687545387d3dfd6abe7c93830ad87062daa875f3f6082bf85b0cdad9"
const windowStoppedV13AuthorizationSHA = "0ca087c1260335fe03e3ed6535fbf0114fbd571719cdbcd8987acde7e8c120df"
const windowStoppedV13SourceSHA = "156feda4eb2114878d1764c7160b1ab2b468e94d965d826f8036ea86a55639d0"

// Fixed stopped DATA only. No owner, source-replay, scoring or release API.
type windowStoppedV13Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV12Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

var windowStoppedV13ClaimHashes = [...]string{
	"2316b0e98bbf8fda4de9051a2697f6ab3c19c55b237eccd18712b5b9fd3af4d7",
	"ff909abace120ce5f38bdab874addc75b123921f21a3ca2df0033ee0bd227319",
	"9e452e97a17c77a8f6d0e3e07b1cac894a5de62372e8d64c32d75945101f82c3",
	"affafc338d861f61389f7977026cd8544bf6bd06cfcf5f8569ed5180c8de6ffd",
	"ca15d703bccfdba46380a739460ba009776777c066425f2d8f87a72abfcee26b",
	"142a901f6fd2af6d3d39a490cb43439355214e4c611c8735c0185a181a38ec71",
	"11330778f31f0d4ef9f7847f8cb71c3369c6805a6ce321e242105a4222f4fe9f",
	"306719dea1269a76984055e4849d71f72fb56a6e590d0187ded3a52e76dfa50b",
	"a2888c4f8270a10057e2731b82b16552df059663a9da9e84ea4ac82ad49a2fbc",
	"f47d737c0877486411a647cf4d9c57aff3fe98ab8b550393669744cec8513ec4",
	"6dfe9dff31f1a3c1cf58bae63430437805aa69e98cd323650becb7e7e52f587a",
	"282a5bd1c7fa1430948fbea30790f35c14bdf615978f12fb04459a56163e4156",
	"f7c38c061a3c9a993c53547736de0d92c6a86fbd6bfb947c97a46c4e427cf1ae",
	"3ff69c174b8dc3cc41c0c7977a085ef394bbf7ddfc922300730faf40f4f6f566",
	"28ec03667eda71a8c2258fec471d0a9093cb343ed58990b267f0893f1e7a805f",
	"dece326b2cafb7ca9ac1d476e3907b4cfe4211f895543381525bb2f283daefd5",
	"71379df8f71050e11ba0bf3afe4fe3d300c857cb4caf56df7d0ce591be441c77",
	"1bc66d1c1275ab013c9ef5e011341ff4f6ae4e4ab2ea8a88eafebb1bcc35d144",
	"e105fa0ce5eaecc31ea6830b1cdb454a85824508f1f68ea576c68f85becc7874",
	"aa075cb6037e8229a08d7e1a4ab3af6984af701fd27f510716d301c776a35399",
	"812c1621f4bfff0a278f7d08489ed192840777cae76775ac4911c23c0970e29c",
}

func expectedStoppedV13WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV12WindowBaseline()
	b.PermanentModel += 5063645
	b.PermanentJEV += 86469
	b.RemainingModel = b.TotalModel - b.PermanentModel
	b.RemainingJEV = b.TotalJEV - b.PermanentJEV
	b.ActualModelCalls += 546
	b.ActualJEVCalls += 184
	for i, sha := range windowStoppedV13ClaimHashes {
		b.ClaimSHA256[fmt.Sprintf("full3660-v13-r4-20261105/window-%06d", i+1)] = sha
	}
	return b
}
func (s windowStoppedV13Baseline) predecessorRoots() []string {
	return append(s.previous.predecessorRoots(), s.originalRoot)
}
func freezeWindowStoppedV13Predecessor(s windowStoppedV12Baseline) windowStoppedV12Baseline {
	s.base.ClaimSHA256 = cloneWindowHistoricalClaims(s.base.ClaimSHA256)
	s.previous = freezeWindowStoppedV12Predecessor(s.previous)
	return s
}
func loadWindowStoppedV13BaselineContext(ctx context.Context, path, sha string) (windowStoppedV13Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV13Baseline, error) {
		return loadWindowStoppedV13BaselineWithPass(p, path, sha)
	})
}
func loadWindowStoppedV13BaselineWithPass(p *historyReadPass, path, sha string) (out windowStoppedV13Baseline, err error) {
	reject := func(e any) (windowStoppedV13Baseline, error) {
		return windowStoppedV13Baseline{}, fmt.Errorf("stopped v13 DATA rejected: %v", e)
	}
	if p == nil || path != windowStoppedV13InventoryPath || sha != windowStoppedV13InventorySHA {
		return reject("fixed177 inventory required")
	}
	raw, err := historyStoppedV7File(p, path, sha, 4<<20)
	var inv windowStoppedV11Inventory
	if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "v13-stopped-original-inventory.v1" || inv.Root != windowStoppedV13Root || inv.Count != 3828 || len(inv.Files) != inv.Count || inv.Bytes != 31649862 || inv.Normal || inv.Remote {
		return reject("fixed Root4 inventory")
	}
	previous, err := loadWindowStoppedV12BaselineWithPass(p, windowStoppedV12InventoryPath, windowStoppedV12InventorySHA)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV12WindowBaseline()) {
		return reject(fmt.Sprintf("concrete156: %v", err))
	}
	frozen := freezeWindowStoppedV13Predecessor(previous)
	for _, root := range previous.predecessorRoots() {
		if !windowPayloadDisjoint(root, inv.Root) {
			return reject("crossed predecessor")
		}
	}
	if err = historyStoppedRoot(p, inv.Root); err != nil {
		return reject(err)
	}
	err = historyStoppedLock(p, 13, inv.Root+"/campaign/.lock", func() error {
		if e := p.enableStoppedV13Metadata(previous); e != nil {
			return e
		}
		files, e := readWindowStoppedV11Inventory(p, inv.Root, inv)
		if e != nil {
			return e
		}
		var m windowActualManifest
		if conversationHash(files["actual-manifest.json"]) != windowStoppedV13ManifestSHA || decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil || validateWindowStoppedV13Manifest(m, previous) != nil {
			return fmt.Errorf("fixed Root4 manifest")
		}
		if e = readWindowStoppedV13Signatures(m, files); e != nil {
			return e
		}
		if e = readWindowStoppedV13CompanionRefs(p, m, files); e != nil {
			return e
		}
		if e = readWindowStoppedV13SourcesWithPass(p, m.SourceRoot, m.Sources, true); e != nil {
			return e
		}
		base, e := readWindowStoppedV13Bundle(inv.Root, m, previous.base, files)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(base, expectedStoppedV13WindowBaseline()) {
			return fmt.Errorf("derived177 mismatch")
		}
		out = windowStoppedV13Baseline{base, previous, path, sha, inv.Root, previous.historicalDir}
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
func validateWindowStoppedV13Manifest(m windowActualManifest, p windowStoppedV12Baseline) error {
	sources, _ := json.Marshal(m.Sources)
	if m.Schema != "full-window-actual-ready.v13" || m.EvidenceKind != "actual" || m.ArchiveCommit != "c20606b7ea4ed04fd7523e869b4258405fa1def6" || m.ArchiveTree != "50a137e0b1702ebbfe5d47138dd37df730b635af" || m.SourceRoot != windowStoppedV13SourceRoot || m.SessionRoot != windowStoppedV13Root+"/records" || m.CampaignRoot != windowStoppedV13Root+"/campaign" || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != (windowActualFile{windowStoppedV12InventoryPath, windowStoppedV12InventorySHA}) || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Limits != windowV4ExpectedLimits() || len(m.Sources) != 2500 || conversationHash(sources) != windowStoppedV13SourceSHA || validateWindowV13ExecutionShape(m.Scope) != nil || windowV8DeclarationSHA(m.Scope) != m.ScopeSHA256 || validateWindowV11SpoolBinding(m) != nil {
		return fmt.Errorf("stopped Root4 manifest identity")
	}
	return nil
}

// The only newly admitted lock is the fixed Root4 path, after the genuine154
// constructor has registered its last predecessor lock in this same pass.
func (p *historyReadPass) stoppedV13LockAllowed(epoch int, path string) bool {
	if epoch != 13 || path != windowStoppedV13Root+"/campaign/.lock" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	old, ok := p.locks[windowStoppedV12Root+"/campaign/.lock"]
	return p.stoppedV12Metadata && ok && old.epoch == 12 && p.roots[windowStoppedV13Root]
}
