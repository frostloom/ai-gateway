package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const windowStoppedV11Root = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v11-freeze-r3-20261101"
const windowStoppedV11SourceRoot = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v11-freeze-r2-20261101/regression/source"
const windowStoppedV11InventoryPath = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-r3-stopped154-20261104/r3-stopped-original-inventory.json"
const windowStoppedV11InventorySHA = "2ff9ee20cbd11e51b5b31a63b9bf31f53625f602194b5bb0d123bff4a462df9c"
const windowStoppedV11ManifestSHA = "1756233539ccef3a0ca359c3ac64e4b1198558ad287d02b853665ae52eca0156"
const windowStoppedV11AuthorizationSHA = "0af219652542c136ce8d3c4e6cfa5d16686faa647ffdfe4c50869879a9348f6c"

// Concrete stopped DATA. No live owner, source, scoring, delivery or release API.
type windowStoppedV11Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV10Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}
type windowStoppedV11InventoryEntry struct {
	Path     string   `json:"path"`
	SHA256   string   `json:"sha256"`
	Bytes    int64    `json:"bytes"`
	Identity [7]int64 `json:"identity"`
}
type windowStoppedV11Inventory struct {
	Schema string                           `json:"schema"`
	Root   string                           `json:"root"`
	Files  []windowStoppedV11InventoryEntry `json:"files"`
	Count  int                              `json:"file_count"`
	Bytes  int64                            `json:"total_bytes"`
	Normal bool                             `json:"proves_normal_completion"`
	Remote bool                             `json:"proves_remote_delivery"`
}

var windowStoppedV11ClaimHashes = [...]string{
	"d6224ee8114ffe340002b69bf7d58389e45ed4b6bb56397435c3a039b8ad0f61",
	"7e0d012ef6ea5e1815bf488c5381b54180843a091377c05e36dee38f6648fc78",
	"84fcd7213f5a3b0418893d3de089d857e7cb5f6b447295bd53168191be6e90da",
	"4a76f4bfbe86e8eba55e75f9e435242eb658c84b3eea97b366bcbe03a90f99c5",
	"a7c350177dfe151b47375da3a70332bef5927f17d650bea5ec3056ca0c1ae975",
	"71ccd66b8c413dea98caf2c7b4e6e590bf47491908a73cdb564112b66d086fcc",
	"fd8435fea9a81f487ca50edb58ab78d1f266281954e469753f2d1911ac3e474e",
	"b49076e7c8b8e248baa2a7d46cb00103fee03f38e1c2953c0abccf155035724a",
	"2d1ea9b3e3f5f526476f8dd493f8b381f0184e27fba63da59798625efc4221e0",
	"8ce38a761c8ed4b0462753a62a40963c50be74314ab7dc1898bdfcf417807278",
	"0e5a36a8ef7dc18a3f9a364cd94d861ba31c2a5db0ab0314a67287bd0cebb273",
	"5ca87e08723c2efaa79e513dce80b2f45f30c82c406d9c73f43ec95b9ec1d7b1",
	"18971c1e482711d15135f91593457a1c5b0f6ece63da1dd77ec8bd0b02424ba2",
	"2081dc4a6323247decdae0544b0663e36a7f2591157469d169d127745a868ec1",
	"95cb50150d2fcccf1c596518bd92ad2fff04490688b10d578096b90fdbc4601d",
	"a7d649e2df889eeed1ab354b0291104db46140481a99f47a418f86b2ec49b0ed",
	"95f050a73f4f30a5de9d82d9987d135be3345eee5ddaf669e33e26987f742a34",
	"638ac5ec48af5a2684991550930ed71cef0ea9fe4d729a1d5c1ef931cbfb0904",
	"876c7b8569e19fcac65ee2890720996513e94d607b3fdb9878d6aa53ccb546ec",
	"10b61071c6f668d5326bc890baeb09999d3ce38b058278fa2ac772bc9ac79d38",
	"8b7df03d7a5f6f02c071f646b7d3fde5ce3c547273c5d1ebf7c799f94f4cd830",
	"4049f44ed1fb491745ea09fbf4813e537197f67130c9ba730567a074fd1e74d9",
	"5b931370a2ee961e59c2541d8a81ee3fd5dbbc1061ee0e2d08cd8b4e13addd8c",
	"58cb26df01bf7772c3c34efdc313b2e5bee9baa0affb571fc9a25741621dbb3f",
	"0fd52db4ccb17c06d533c3a9e82349a71519f6169166630b09ee2721c72240ae",
	"a34a974dea8aba37dfc54dc9b6c06e5a6fab1aff2241abf09f949fba0963f099",
	"0bc31d380aab341c7069e2e2d97ae25a1bbdaafa365e27a4e6d143686d1172fb",
	"3a0e27c51b754a4a33dc8beb9355b92fd1a5f11ccec5864f10258de8bdb6a8cd",
	"56b1760e47c131699c5ce457a5df252732a37ba2b8297dfab541f3ef57cbed45",
	"077f858c74dbb69f4ab3edd03e5cf6d91dd923e18a5c90cd456f8aa2de5b8ecd",
	"452092f8543a59ee920893739e1a8e083114ee5ebb1f2e5ba77c4171450146ed",
	"d61dfb6e3a2a697fd3f02468af17c3c883c693fbefe72b2c1bccc3c861cec408",
	"de191c234b68ca0afb5fa8aec4a7716296f8f0d91a4477c1cdc14459adcba738",
	"6f00a47ecf9daea15c8844509d0c387a29054f8da983bdb22a2f994290593f86",
	"c573e0c7254e592cef7a43320948f706bf2160be4b1b657f468ebe7db3c7ed3e",
	"766d2660008248b8dae603f75fbba92172293bdb06663244dc616424113f912c",
	"394d9f0f574089618aaeeff8c72308a64e63ec3367d6f2ef66759ab69ad9f3d9",
	"01f72d3f7b5e43c5eac4d478bb1e3668bba3fa438460966b19a05ff2ab4949e5",
	"04834e84bdfb3f5150c754e66c7f04aee2a7d7cb97b0b9912a40391974daf094",
	"cced181bb1262956c7ab11467d0a525a74cccdde772819c78023d27f9393cadb",
	"8cbc357403bcb8b73f3dfd6caf881b7e9e003271533b26509d2cc8b4c4f16140",
	"eb28018893d233a2737a80cd555278a249f9976ccc90eb83d1e58740bcd654ca",
	"04e918eb97081b721165924bc52b8313cd4eb477c08b2eecf4875c0e6a9d834e",
	"51435e5814104e890624f0840201410bbc1d64a24f68a4657fe6a939d2cf8515",
	"87e17d53a784557648083ff57c299bc56a18b439cdf4384b6143ee7f47162d62",
	"769a4dce654932f662543d1d3a46bae867553164ca7e23437f6a98a71389d358",
	"4aa37d17fb09f5dc00056cb36bb131ebb266a619b6a756c5e0d25f529bbacee2",
	"18cde025f4bdbc60c7cc5a1ce74070c8291f4fe07540322a140e22ca7c1642c8",
	"1aaaaac81303d1a2b672fa9e6423f3627244563a8c27067092f987c2d702558b",
	"df3a76b3374ba3ccebacf1fae1000379cc1db2afdea2c67a6c04d3b7b8108b26",
}

func expectedStoppedV11WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV10WindowBaseline()
	b.PermanentModel += 12795538
	b.PermanentJEV += 199708
	b.RemainingModel = b.TotalModel - b.PermanentModel
	b.RemainingJEV = b.TotalJEV - b.PermanentJEV
	b.ActualModelCalls += 1495
	b.ActualJEVCalls += 362
	for i, sha := range windowStoppedV11ClaimHashes {
		b.ClaimSHA256[fmt.Sprintf("full1220-v11-20261101/window-%06d", i+1)] = sha
	}
	return b
}
func (s windowStoppedV11Baseline) predecessorRoots() []string {
	return append(s.previous.predecessorRoots(), s.originalRoot)
}
func freezeWindowStoppedV11Predecessor(s windowStoppedV10Baseline) windowStoppedV10Baseline {
	s.base.ClaimSHA256 = cloneWindowHistoricalClaims(s.base.ClaimSHA256)
	s.previous.base.ClaimSHA256 = cloneWindowHistoricalClaims(s.previous.base.ClaimSHA256)
	s.previous.previous = freezeWindowStoppedV9Predecessor(s.previous.previous)
	return s
}
func loadWindowStoppedV11BaselineContext(ctx context.Context, path, sha string) (windowStoppedV11Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV11Baseline, error) {
		return loadWindowStoppedV11BaselineWithPass(p, path, sha)
	})
}
func loadWindowStoppedV11BaselineWithPass(p *historyReadPass, path, sha string) (out windowStoppedV11Baseline, err error) {
	reject := func(e any) (windowStoppedV11Baseline, error) {
		return windowStoppedV11Baseline{}, fmt.Errorf("stopped v11 DATA rejected: %v", e)
	}
	if p == nil || path != windowStoppedV11InventoryPath || sha != windowStoppedV11InventorySHA {
		return reject("fixed154 inventory required")
	}
	raw, err := historyStoppedV7File(p, path, sha, 4<<20)
	var inv windowStoppedV11Inventory
	if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "r3-stopped-original-inventory.v1" || inv.Root != windowStoppedV11Root || inv.Count != 13125 || len(inv.Files) != inv.Count || inv.Bytes != 60172507 || inv.Normal || inv.Remote {
		return reject("fixed original inventory")
	}
	previous, err := loadWindowStoppedV10BaselineWithPass(p, windowStoppedV10Root+"/stopped-v10-original-inventory.json", windowV11BaselineSHA)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV10WindowBaseline()) {
		return reject(fmt.Sprintf("concrete104: %v", err))
	}
	if err = p.enableStoppedV11Metadata(previous); err != nil {
		return reject(err)
	}
	frozen := freezeWindowStoppedV11Predecessor(previous)
	for _, root := range previous.predecessorRoots() {
		if !windowPayloadDisjoint(root, inv.Root) || !windowPayloadDisjoint(root, windowStoppedV11SourceRoot) {
			return reject("crossed predecessor")
		}
	}
	if err = historyStoppedRoot(p, inv.Root); err != nil {
		return reject(err)
	}
	err = historyStoppedLock(p, 11, inv.Root+"/campaign/.lock", func() error {
		files, e := readWindowStoppedV11Inventory(p, inv.Root, inv)
		if e != nil {
			return e
		}
		manifest := files["actual-manifest.json"]
		var m windowActualManifest
		if conversationHash(manifest) != windowStoppedV11ManifestSHA || decodeWindowActualJSON(manifest, &m) != nil || m.Schema != "full-window-actual-ready.v11" || m.ArchiveCommit != "63b9d1ed4d64bb583d15fa884c37468e5c53044c" || m.ArchiveTree != "43558c68a00122d90291aed34b05e134b7054197" || m.SourceRoot != windowStoppedV11SourceRoot || m.SessionRoot != inv.Root+"/records" || m.CampaignRoot != inv.Root+"/campaign" || m.BaselineEvidence == nil || *m.BaselineEvidence != (windowActualFile{Path: windowStoppedV10Root + "/stopped-v10-original-inventory.json", SHA256: windowV11BaselineSHA}) || m.ClaimsDir != previous.historicalDir || !reflect.DeepEqual(m.HistoricalClaims, previous.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || validateWindowV11ExecutionShape(m.Scope) != nil || windowV8DeclarationSHA(m.Scope) != m.ScopeSHA256 || validateWindowV11SpoolBinding(m) != nil {
			return fmt.Errorf("fixed original R3 manifest")
		}
		if e = historyStoppedRoot(p, m.SourceRoot); e != nil {
			return e
		}
		names := make([]string, 0, len(m.Sources))
		for name, sha := range m.Sources {
			if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "\\") || !comparisonDigest(sha) {
				return fmt.Errorf("source identity")
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, e = p.Stream(historyReadInventoryStream, filepath.Join(m.SourceRoot, filepath.FromSlash(name)), m.Sources[name], windowActualSourceMaxBytes); e != nil {
				return e
			}
		}
		base, e := readWindowStoppedV11Bundle(inv.Root, m, previous.base, files)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(base, expectedStoppedV11WindowBaseline()) {
			return fmt.Errorf("derived154 mismatch")
		}
		out = windowStoppedV11Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: sha, originalRoot: inv.Root, historicalDir: previous.historicalDir}
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
func stoppedV11IdentityMatches(id historyFileIdentity, w [7]int64) bool {
	return id.Mode.IsRegular() && uint64(w[0]) == id.Dev && uint64(w[1]) == id.Ino && w[2] == int64(id.Mode.Perm())|0100000 && uint64(w[3]) == id.Links && w[4] == id.Size && w[5] == id.Mtime && w[6] == id.Ctime
}
func readWindowStoppedV11Inventory(p *historyReadPass, root string, inv windowStoppedV11Inventory) (map[string][]byte, error) {
	files := map[string][]byte{}
	owned := windowStoppedV8Inventory{Root: root, Files: map[string]windowStoppedV8InventoryEntry{}}
	var total int64
	spoolDirs := map[string]map[string]bool{}
	for _, f := range inv.Files {
		if !filepath.IsLocal(f.Path) || filepath.ToSlash(filepath.Clean(f.Path)) != f.Path || strings.Contains(f.Path, "\\") || files[f.Path] != nil || f.Bytes < 0 || f.Bytes > 8<<20 || !comparisonDigest(f.SHA256) {
			return nil, fmt.Errorf("inventory identity")
		}
		path := filepath.Join(root, filepath.FromSlash(f.Path))
		raw, e := historyStoppedV7File(p, path, f.SHA256, max(1, f.Bytes))
		if e != nil || int64(len(raw)) != f.Bytes {
			return nil, fmt.Errorf("inventory original %s: %v", f.Path, e)
		}
		p.mu.Lock()
		entry := p.paths[path]
		valid := entry != nil && stoppedV11IdentityMatches(entry.identity, f.Identity)
		p.mu.Unlock()
		if !valid {
			return nil, fmt.Errorf("inventory original identity %s", f.Path)
		}
		if strings.HasPrefix(f.Path, "spool/") {
			parts := strings.Split(f.Path, "/")
			for i := 1; i < len(parts); i++ {
				dir := strings.Join(parts[:i], "/")
				if spoolDirs[dir] == nil {
					spoolDirs[dir] = map[string]bool{}
				}
				isDir := i < len(parts)-1
				if old, ok := spoolDirs[dir][parts[i]]; ok && old != isDir {
					return nil, fmt.Errorf("spool namespace conflict")
				}
				spoolDirs[dir][parts[i]] = isDir
			}
		}
		files[f.Path] = raw
		total += f.Bytes
		if strings.HasPrefix(f.Path, "campaign/") || strings.HasPrefix(f.Path, "records/") {
			owned.Files[f.Path] = windowStoppedV8InventoryEntry{SHA256: f.SHA256, Bytes: f.Bytes}
		}
	}
	if total != inv.Bytes || len(files) != inv.Count {
		return nil, fmt.Errorf("inventory totals")
	}
	dirNames := make([]string, 0, len(spoolDirs))
	for dir := range spoolDirs {
		dirNames = append(dirNames, dir)
	}
	sort.Strings(dirNames)
	for _, dir := range dirNames {
		if e := historyStoppedDir(p, filepath.Join(root, filepath.FromSlash(dir)), spoolDirs[dir]); e != nil {
			return nil, e
		}
	}
	if _, _, e := readWindowStoppedV9InventoryWithPass(p, root, owned); e != nil {
		return nil, e
	}
	return files, nil
}

// This private stage transition has no caller-selectable limit or roots. The
// only production caller follows the genuine concrete104 constructor in this
// same pass. Pre-activation metadata remains entirely charged to legacy32MiB.
func (p *historyReadPass) enableStoppedV11Metadata(previous windowStoppedV10Baseline) error {
	if p == nil {
		return fmt.Errorf("stopped154 metadata stage rejected")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	oldPath := windowStoppedV10Root + "/stopped-v10-original-inventory.json"
	oldEvidence, newEvidence := p.paths[oldPath], p.paths[windowStoppedV11InventoryPath]
	oldLock, hasLock := p.locks[windowStoppedV10Root+"/campaign/.lock"]
	if p.ctx == nil || p.ctx.Err() != nil || p.state != historyPassOpen || p.stoppedV11Metadata || p.stats.TranscriptBytes > historyPassTranscriptLimit || oldEvidence == nil || oldEvidence.digest != windowV11BaselineSHA || newEvidence == nil || newEvidence.digest != windowStoppedV11InventorySHA || !hasLock || oldLock.epoch != 10 || previous.evidencePath != oldPath || previous.evidenceSHA256 != windowV11BaselineSHA || previous.originalRoot != windowStoppedV10Root || !reflect.DeepEqual(previous.base, expectedStoppedV10WindowBaseline()) {
		return p.failLocked(fmt.Errorf("stopped154 metadata stage rejected"))
	}
	p.stoppedV11LegacyTranscript = p.stats.TranscriptBytes
	p.stoppedV11Metadata = true
	return nil
}

func stoppedV11MetadataPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.Contains(path, "\\") && (path == windowStoppedV11InventoryPath || historyInside(path, windowStoppedV11Root) || historyInside(path, windowStoppedV11SourceRoot))
}

// Only the newly pinned R3 DATA pool avoids duplicate pass byte retention.
// Old references retain the unchanged128MiB admission limit. Uncached references
// still use the same supplying-FD reader and fresh End metadata/hash sweep.
func (p *historyReadPass) retainHistoryBytesLocked(s historyReadSpec, size int64) bool {
	return !s.isStream() && size <= historyPassEntryLimit && !(p.stoppedV11Metadata && filepath.IsAbs(s.path) && filepath.Clean(s.path) == s.path && !strings.Contains(s.path, "\\") && historyInside(s.path, windowStoppedV11Root))
}
