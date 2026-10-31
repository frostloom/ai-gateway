package taskbench

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const windowStoppedV10Root = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-completion-v10-freeze-20261031"
const windowStoppedV10ManifestSHA = "0c8e9c5bc9d58f47efd572b25ffdca8410bef11fba5c868bc324908abbf9bfe4"
const windowStoppedV10AuthorizationSHA = "04eacd96cd0c6edcacc51b7f180c3c17c445044279699aba23ae4b3c7c87dd79"
const windowStoppedV10PreviousPath = "/Users/a1/项目/ai-gateway/.tmp/v9-live-score-horizon-20261031/r5-stopped102-preparation-v2/stopped-r5.private.json"

// Concrete historical DATA only: it has no source/score/owner issuer method.
type windowStoppedV10Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV9Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}
type windowStoppedV10Inventory struct {
	Schema string `json:"schema"`
	Files  []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"files"`
	Count          int             `json:"file_count"`
	Bytes          int64           `json:"total_bytes"`
	CanonicalPairs int             `json:"canonical_collected_pairs"`
	Claim3Absent   bool            `json:"claim3_absent"`
	GlobalJEV      int             `json:"global_jev_calls"`
	GlobalModel    int             `json:"global_model_calls"`
	Claims         int             `json:"permanent_claim_count"`
	Closed         bool            `json:"permanently_closed"`
	Restart        bool            `json:"restart_allowed"`
	Permanent      windowV8Amounts `json:"total_permanent"`
	Attempted      int             `json:"v10_attempted_arms"`
	Finished       int             `json:"v10_finished_arms"`
	JEV            int             `json:"v10_jev_calls"`
	Model          int             `json:"v10_model_calls"`
}

func expectedStoppedV10WindowBaseline() fullWindowBaseline {
	b := expectedStoppedV9WindowBaseline()
	b.PermanentModel += 353366
	b.PermanentJEV += 7541
	b.RemainingModel = b.TotalModel - b.PermanentModel
	b.RemainingJEV = b.TotalJEV - b.PermanentJEV
	b.ActualModelCalls += 27
	b.ActualJEVCalls += 7
	b.ClaimSHA256["full1220-v10-20261031/window-000001"] = "aef05f962503f1fca5f2917e3fde62a4ed15c65f773b61f450049c1ee94b6107"
	b.ClaimSHA256["full1220-v10-20261031/window-000002"] = "86c0af0a62a3d06276f4d1cf5fa6b8a07c009ce49cc5ae0e90fd3114b76a982a"
	return b
}
func (s windowStoppedV10Baseline) predecessorRoots() []string {
	return append(s.previous.windowStoppedPredecessorRootsV10(), s.originalRoot)
}
func loadWindowStoppedV10BaselineContext(ctx context.Context, path, sha string) (windowStoppedV10Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV10Baseline, error) {
		return loadWindowStoppedV10BaselineWithPass(p, path, sha)
	})
}
func loadWindowStoppedV10BaselineWithPass(p *historyReadPass, path, sha string) (out windowStoppedV10Baseline, err error) {
	reject := func(why string) (windowStoppedV10Baseline, error) {
		return windowStoppedV10Baseline{}, fmt.Errorf("stopped v10 DATA rejected: %s", why)
	}
	if p == nil || sha != windowV11BaselineSHA || path != windowStoppedV10Root+"/stopped-v10-original-inventory.json" {
		return reject("fixed104 inventory required")
	}
	if err = historyStoppedRoot(p, windowStoppedV10Root); err != nil {
		return reject(err.Error())
	}
	raw, err := historyStoppedV7File(p, path, sha, 1<<20)
	var inv windowStoppedV10Inventory
	if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "full1220-v10-stopped-original-inventory.v1" || inv.Count != 181 || len(inv.Files) != 181 || inv.Bytes != 1195270 || !inv.Closed || inv.Restart || !inv.Claim3Absent || inv.Claims != 104 || inv.Model != 27 || inv.JEV != 7 || inv.GlobalModel != 5164 || inv.GlobalJEV != 1315 || inv.Attempted != 14 || inv.Finished != 13 || inv.CanonicalPairs != 390 || inv.Permanent != (windowV8Amounts{66825427, 638655}) {
		return reject("fixed181 inventory")
	}
	previous, err := loadWindowStoppedV9BaselineWithPass(p, windowStoppedV10PreviousPath, windowV10BaselineSHA)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV9WindowBaseline()) {
		return reject(fmt.Sprintf("concrete102: %v", err))
	}
	frozen := previous
	frozen.base.ClaimSHA256 = cloneWindowHistoricalClaims(previous.base.ClaimSHA256)
	frozen.previous = freezeWindowStoppedV9Predecessor(previous.previous)
	for _, root := range previous.windowStoppedPredecessorRootsV10() {
		if !windowPayloadDisjoint(windowStoppedV10Root, root) {
			return reject("crossed predecessor")
		}
	}
	err = historyStoppedLock(p, 10, windowStoppedV10Root+"/campaign/.lock", func() error {
		manifest, err := historyStoppedV7File(p, windowStoppedV10Root+"/actual-manifest.json", windowStoppedV10ManifestSHA, 8<<20)
		var m windowActualManifest
		if err != nil || decodeWindowActualJSON(manifest, &m) != nil || m.Schema != "full-window-actual-ready.v10" || m.ArchiveCommit != "ab045e71f1ef459a2965794026d6688b55efbab0" || m.ArchiveTree != "7bc224a3d4d180b1a99832df89e64a0f8c3e578a" || m.SourceRoot != windowStoppedV10Root+"/regression/source" || m.SessionRoot != windowStoppedV10Root+"/records" || m.CampaignRoot != windowStoppedV10Root+"/campaign" || m.BaselineEvidence == nil || *m.BaselineEvidence != (windowActualFile{Path: windowStoppedV10PreviousPath, SHA256: windowV10BaselineSHA}) || !reflect.DeepEqual(m.HistoricalClaims, previous.base.ClaimSHA256) || m.ClaimsDir != previous.historicalDir || m.Limits != windowV4ExpectedLimits() || validateWindowV10ExecutionShape(m.Scope) != nil || windowV8DeclarationSHA(m.Scope) != m.ScopeSHA256 {
			return fmt.Errorf("fixed original manifest")
		}
		if len(m.Sources) != 2369 || windowV8DeclarationSHA(m.Sources) != "c0cc20cac58f6a6595d4cc586ffd233eee0b640a3a77f48e61bea7ca3b085629" {
			return fmt.Errorf("original source map")
		}
		if err = historyStoppedRoot(p, m.SourceRoot); err != nil {
			return err
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
			if _, err = p.Stream(historyReadInventoryStream, filepath.Join(m.SourceRoot, filepath.FromSlash(name)), m.Sources[name], windowActualSourceMaxBytes); err != nil {
				return err
			}
		}
		att, err := historyStoppedV7File(p, windowStoppedV10Root+"/actual-manifest.json.binary-attestation.json", "e00206e77b3fc30c2f402766e7f01e658c749466cd54a4c8d6100351ed78d345", 65536)
		if err != nil {
			return err
		}
		if err = historyStoppedBinaryAttestation(p, att, m, windowStoppedV10ManifestSHA, windowStoppedV10Root+"/full-window-actual-v10"); err != nil {
			return err
		}
		// Public prior signed bytes are evidence only; no private key/settings read.
		auth, err := historyStoppedV7File(p, windowStoppedV10Root+"/execution-authorization.json", windowStoppedV10AuthorizationSHA, 65536)
		if err != nil {
			return err
		}
		if _, err = verifyWindowV10ExecutionAuthorization(auth, m, windowStoppedV10ManifestSHA, "25560f532adf3776ba1515926ba4b6fc3b532c7fb00ebfe231ea0e2691ec615a"); err != nil {
			return err
		}
		for _, ref := range []windowActualFile{{Path: windowStoppedV10Root + "/autonomous-authorization.private.json", SHA256: windowV9AutonomousAuthorizationSHA}, {Path: windowStoppedV10Root + "/human-approval-reply.private.json", SHA256: windowStoppedV8ApprovalSHA}, *m.ExecutorEquivalence} {
			if _, err = historyStoppedV7File(p, ref.Path, ref.SHA256, 8<<20); err != nil {
				return err
			}
		}
		files, err := readWindowStoppedV10Inventory(p, windowStoppedV10Root, inv)
		if err != nil {
			return err
		}
		base, err := readWindowStoppedV10Bundle(windowStoppedV10Root, m, previous.base, files)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(base, expectedStoppedV10WindowBaseline()) {
			return fmt.Errorf("derived104 mismatch")
		}
		out = windowStoppedV10Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: sha, originalRoot: windowStoppedV10Root, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return reject(err.Error())
	}
	// One immutable typed snapshot, one pass. End rereads every supplying FD and
	// exact namespace/lock; no second recursive102 load or cross-pass cache.
	if !reflect.DeepEqual(frozen, previous) || p.ctx.Err() != nil {
		return reject("predecessor changed or canceled")
	}
	return out, nil
}
func readWindowStoppedV10Inventory(p *historyReadPass, root string, inv windowStoppedV10Inventory) (map[string][]byte, error) {
	files := map[string][]byte{}
	owned := windowStoppedV8Inventory{Root: root, Files: map[string]windowStoppedV8InventoryEntry{}}
	var total int64
	for _, f := range inv.Files {
		if !filepath.IsLocal(f.Path) || filepath.ToSlash(filepath.Clean(f.Path)) != f.Path || strings.Contains(f.Path, "\\") || files[f.Path] != nil || f.Size < 0 || f.Size > 8<<20 || !comparisonDigest(f.SHA256) {
			return nil, fmt.Errorf("inventory identity")
		}
		raw, e := historyStoppedV7File(p, filepath.Join(root, filepath.FromSlash(f.Path)), f.SHA256, max(1, f.Size))
		if e != nil || int64(len(raw)) != f.Size {
			return nil, fmt.Errorf("inventory original %s: %v", f.Path, e)
		}
		files[f.Path] = raw
		total += f.Size
		if strings.HasPrefix(f.Path, "campaign/") || strings.HasPrefix(f.Path, "records/") {
			owned.Files[f.Path] = windowStoppedV8InventoryEntry{SHA256: f.SHA256, Bytes: f.Size}
		}
	}
	if total != inv.Bytes || len(files) != inv.Count {
		return nil, fmt.Errorf("inventory totals")
	}
	if _, _, e := readWindowStoppedV9InventoryWithPass(p, root, owned); e != nil {
		return nil, e
	}
	return files, nil
}
