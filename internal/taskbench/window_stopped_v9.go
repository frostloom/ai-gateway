package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type windowStoppedV9Evidence windowStoppedV8Evidence

// Historical DATA only. There is intentionally no stoppedHistory reentry,
// acquisition, completion, quality, source-proof, or scoring issuer method.
type windowStoppedV9Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV8Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}

func validateWindowStoppedV9Descriptor(e windowStoppedV9Evidence) error {
	root, d := windowStoppedV9OriginalRoot, windowStoppedV9DataRoot
	if e.Schema != "full-window-stopped-baseline.v9" || e.OriginalRoot != root {
		return fmt.Errorf("stopped v9 descriptor identity")
	}
	refs := []struct {
		got       windowActualFile
		path, sha string
	}{
		{e.PreviousBaseline, "/Users/a1/项目/ai-gateway/.tmp/v9-stopped46-baseline-20261030/stopped-v8.private.json", windowV9BaselineSHA},
		{e.BudgetApproval, root + "/human-approval-reply.private.json", windowStoppedV8ApprovalSHA},
		{e.Manifest, root + "/actual-manifest.json", windowStoppedV9ManifestSHA},
		{e.StoppedAudit, d + "/r5-stop-h056-delivery-audit.json", "72e296c427039da615518050d96a9144af3ad3db69e1bbaf20e4ed8418386250"},
		{e.Inventory, d + "/r5-stopped102-preparation/original-inventory.json", "d94416208084bc50a81ac1aa69bea8c9b646d30e2c2d14239218903e58c95c0f"},
		{e.SourceConfirmation, d + "/r5-stopped102-preparation-v2/source-confirmation.json", "0be98c5fb6c13dbe01758a245df6fa755b679d5785338316d1721f09677a5dbf"},
		{e.OwnerExit, root + "/actual-owner-exit.json", "2373309ed9886d2ae2f77f9f37532e69630b07c44eab5541e6e06fe2eb04c00e"},
	}
	for _, r := range refs {
		if r.got != (windowActualFile{Path: r.path, SHA256: r.sha}) {
			return fmt.Errorf("stopped v9 fixed reference %s", r.path)
		}
	}
	return nil
}
func loadWindowStoppedV9BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV9Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV9Baseline, error) {
		return loadWindowStoppedV9BaselineWithPass(p, path, expectedSHA)
	})
}
func loadWindowStoppedV9BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV9Baseline, error) {
	reject := func(reason string) (windowStoppedV9Baseline, error) {
		return windowStoppedV9Baseline{}, fmt.Errorf("stopped v9 evidence rejected: %s", reason)
	}
	if p == nil || expectedSHA != windowStoppedV9DescriptorSHA {
		return reject("concrete descriptor and owned history pass required")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return reject("descriptor directory")
	}
	raw, err := historyStoppedV7File(p, path, expectedSHA, 65536)
	var e windowStoppedV9Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || validateWindowStoppedV9Descriptor(e) != nil {
		return reject("descriptor")
	}
	root := e.OriginalRoot
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root || historyStoppedRoot(p, root) != nil {
		return reject("root identity/alias")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return reject("root identity")
	}
	previous, err := loadWindowStoppedV8BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV8WindowBaseline()) {
		return reject("concrete predecessor")
	}
	frozenPrevious := freezeWindowStoppedV9Predecessor(previous)
	for _, old := range previous.windowStoppedPredecessorRootsV9() {
		if !windowPayloadDisjoint(root, old) {
			return reject("crossed historical roots")
		}
	}
	var out windowStoppedV9Baseline
	err = historyStoppedLock(p, 9, root+"/campaign/.lock", func() error {
		for _, ref := range []windowActualFile{e.BudgetApproval, e.StoppedAudit} {
			if _, err := historyStoppedV7File(p, ref.Path, ref.SHA256, 8<<20); err != nil {
				return err
			}
		}
		// A pinned persisted source replay remains an external reasoning dependency.
		// Reading its references does not reconstruct a live/opaque source proof.
		confirmation, err := historyStoppedV7File(p, e.SourceConfirmation.Path, e.SourceConfirmation.SHA256, 1<<20)
		if err != nil {
			return err
		}
		if err = readWindowStoppedV9SourceData(p, confirmation); err != nil {
			return err
		}
		raw, err := historyStoppedV7File(p, e.Inventory.Path, e.Inventory.SHA256, 8<<20)
		var inv windowStoppedV8Inventory
		if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "full1220-v9-stopped-original-inventory.v1" || inv.Root != root || inv.Count != 9055 || len(inv.Files) != 9055 || inv.Bytes != 47257122 || !inv.Closed || !inv.NoResume {
			return fmt.Errorf("stopped v9 fixed9055 inventory")
		}
		var total int64
		for _, entry := range inv.Files {
			if entry.Bytes < 0 || entry.Bytes > 8<<20 || total > 47257122-entry.Bytes {
				return fmt.Errorf("stopped v9 inventory lengths")
			}
			total += entry.Bytes
		}
		if total != 47257122 {
			return fmt.Errorf("stopped v9 inventory total")
		}
		files, before, err := readWindowStoppedV9InventoryWithPass(p, root, inv)
		if err != nil {
			return err
		}
		manifest, err := historyStoppedV7File(p, e.Manifest.Path, e.Manifest.SHA256, 8<<20)
		var m windowActualManifest
		if err != nil || decodeWindowActualJSON(manifest, &m) != nil || validateWindowStoppedV9Manifest(m, e, previous) != nil {
			return fmt.Errorf("stopped v9 original manifest")
		}
		sourceRaw, _ := json.Marshal(m.Sources)
		if len(m.Sources) != 2315 || conversationHash(sourceRaw) != "47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00" {
			return fmt.Errorf("stopped v9 fixed source map")
		}
		if err = readWindowStoppedV9SourcesWithPass(p, m.SourceRoot, m.Sources); err != nil {
			return err
		}
		attestation, err := historyStoppedV7File(p, e.Manifest.Path+".binary-attestation.json", "3bb493e438c746b2657b4c1f63fba60ecc564f22680f2c93a81ed3c8b4c23bae", 65536)
		if err != nil {
			return err
		}
		if err = historyStoppedBinaryAttestation(p, attestation, m, e.Manifest.SHA256, root+"/full-window-actual-v9"); err != nil {
			return err
		}
		owner, err := historyStoppedV7File(p, e.OwnerExit.Path, e.OwnerExit.SHA256, 65536)
		if err != nil {
			return err
		}
		files["actual-owner-exit.json"] = owner
		for i, sha := range windowStoppedV9ClaimPins() {
			if conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) != sha {
				return fmt.Errorf("stopped v9 claim anchor")
			}
		}
		base, err := readWindowStoppedV9Bundle(root, m, previous.base, files)
		if err != nil || !reflect.DeepEqual(base, expectedStoppedV9WindowBaseline()) {
			return fmt.Errorf("stopped v9 derived102 mismatch: %v", err)
		}
		again, after, err := readWindowStoppedV9InventoryWithPass(p, root, inv)
		delete(files, "actual-owner-exit.json")
		if err != nil || !reflect.DeepEqual(files, again) || compareWindowStoppedV7Identity(before, after) != nil {
			return fmt.Errorf("stopped v9 original bytes/identity changed")
		}
		out = windowStoppedV9Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: root, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return reject(err.Error())
	}
	if err = recheckWindowStoppedV9Predecessor(p, frozenPrevious, previous); err != nil {
		return windowStoppedV9Baseline{}, err
	}
	for _, ref := range []windowActualFile{{Path: path, SHA256: expectedSHA}, e.BudgetApproval, e.Manifest, e.StoppedAudit, e.Inventory, e.SourceConfirmation, e.OwnerExit} {
		if _, err := historyStoppedV7File(p, ref.Path, ref.SHA256, 8<<20); err != nil {
			return reject("reference changed")
		}
	}
	after, err := os.Lstat(root)
	if err != nil || !os.SameFile(rootInfo, after) {
		return reject("root replaced")
	}
	return out, nil
}
func validateWindowStoppedV9Manifest(m windowActualManifest, e windowStoppedV9Evidence, p windowStoppedV8Baseline) error {
	root := e.OriginalRoot
	if m.Schema != "full-window-actual-ready.v9" || m.Scope.Schema != "full-window-scope.v9" || m.ArchiveCommit != "4ddd37ad5ba44db630bee61ddfebba5402e09d45" || m.ArchiveTree != "170b19ea8f6e4f5080ac1a2953cf642fb1c2a7a4" || m.ScopeSHA256 != "e38eff5e9b79c7b1ad245cfd7e6eb09400260d7c579affece93ec1281e9976c9" || m.ExecutionSHA256 != "75157e24baab1c2315d8f800a22dbe998f58c3c3754827cfd8e027451e9fc6cd" || m.SourceRoot != root+"/regression/source" || m.CampaignRoot != root+"/campaign" || m.SessionRoot != root+"/records" || m.ClaimsDir != p.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.PreviousBaseline || !reflect.DeepEqual(m.HistoricalClaims, p.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || len(m.Scope.Slots) != 3660 || len(m.Scope.Windows) != 511 || validateWindowV9Declarations(m.Scope) != nil {
		return fmt.Errorf("stopped v9 manifest identity")
	}
	return nil
}
func (s windowStoppedV9Baseline) windowStoppedPredecessorRootsV10() []string {
	p := s.previous.windowStoppedPredecessorRootsV9()
	out := append([]string{}, p[:]...)
	return append(out, s.originalRoot)
}

func readWindowStoppedV9SourceData(p *historyReadPass, raw []byte) error {
	if conversationHash(raw) != "0be98c5fb6c13dbe01758a245df6fa755b679d5785338316d1721f09677a5dbf" {
		return fmt.Errorf("stopped v9 source confirmation anchor")
	}
	obj, err := windowJSONObject(raw)
	if err != nil || obj["schema"] != "full1220-v9-stopped-source-confirmation.v1" || obj["classification"] != "external_persisted_source_replay_DATA_only" || obj["original_root"] != windowStoppedV9OriginalRoot || obj["input_source_map_sha256"] != "47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00" || obj["noActualAuthority"] != true || obj["normal56WindowProof"] != false || obj["scoresHold"] != true || obj["new_opaque_reconstructed_from_JSON"] != false {
		return fmt.Errorf("stopped v9 source DATA boundary")
	}
	// This traversal is permitted only after the exact confirmation hash is read.
	// Its fixed references include original55 and the after-stop56 data replay,
	// their review, and the retained prior missing-source record.
	var refs []windowActualFile
	var visit func(any) error
	visit = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			if path, ok := x["path"]; ok {
				sha, ok := x["sha256"].(string)
				name, good := path.(string)
				if !ok || !good || len(x) != 2 || !filepath.IsAbs(name) || filepath.Clean(name) != name || !comparisonDigest(sha) || len(refs) >= 128 {
					return fmt.Errorf("stopped v9 source reference")
				}
				refs = append(refs, windowActualFile{Path: name, SHA256: sha})
				return nil
			}
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if err := visit(x[k]); err != nil {
					return err
				}
			}
		case []any:
			for _, a := range x {
				if err := visit(a); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(obj); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := historyStoppedV7File(p, ref.Path, ref.SHA256, 8<<20); err != nil {
			return fmt.Errorf("stopped v9 source data reference: %w", err)
		}
	}
	// Also retain the independent completed55 delivery reasoning bytes; no score IO.
	_, err = historyStoppedV7File(p, windowStoppedV9DataRoot+"/independent-first-055-delivery-summary.json", "3ed36a445a5e8be86773d707992974c690dba41f63e2967c2066c3948612ef06", 8<<20)
	return err
}

// Source DATA is streamed by the same pass: supplying-FD identity and digest,
// no aliases/hardlinks, protected roots, caller cancellation and fresh Finish.
func readWindowStoppedV9SourcesWithPass(p *historyReadPass, root string, sources map[string]string) error {
	if p == nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || len(sources) < 1 || len(sources) > 2315 {
		return fmt.Errorf("stopped v9 source specification")
	}
	if err := historyStoppedRoot(p, root); err != nil {
		return err
	}
	names := make([]string, 0, len(sources))
	for name, sha := range sources {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "\\") || !comparisonDigest(sha) {
			return fmt.Errorf("stopped v9 source path/hash")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := p.Stream(historyReadInventoryStream, filepath.Join(root, filepath.FromSlash(name)), sources[name], windowActualSourceMaxBytes); err != nil {
			return fmt.Errorf("stopped v9 source changed %s: %w", name, err)
		}
	}
	return nil
}

// Preserve a failed nested read separately from an actual value mismatch. Old
// loaders may replace an underlying pass failure with a generic rejection; the
// owned pass retains its first cause, which must remain inspectable by callers.
func checkWindowStoppedV9Predecessor(p *historyReadPass, previous, fresh windowStoppedV8Baseline, loadErr error) error {
	if p == nil {
		return fmt.Errorf("stopped v9 predecessor revalidation requires owned pass")
	}
	p.mu.Lock()
	passErr, stats := p.failure, p.stats
	p.mu.Unlock()
	if cause := errors.Join(loadErr, passErr); cause != nil {
		return fmt.Errorf("stopped v9 predecessor revalidation failed (logical=%d records=%d transcript_bytes=%d cache_bytes=%d): %w", stats.Logical, stats.Records, stats.TranscriptBytes, stats.CacheBytes, cause)
	}
	if !reflect.DeepEqual(previous, fresh) {
		return fmt.Errorf("stopped v9 evidence rejected: predecessor changed")
	}
	return nil
}

// This snapshot exists only on this invocation's stack after the unchanged old
// concrete46 constructor has validated every ancestor in this same pass. Strings
// and scalars are values; these eight claim maps are its only reference fields.
// No derived value is stored in the pass or reused by a later invocation.
func freezeWindowStoppedV9Predecessor(previous windowStoppedV8Baseline) windowStoppedV8Baseline {
	out := previous
	for _, b := range []*fullWindowBaseline{&out.base, &out.previous.base, &out.previous.previous.base, &out.previous.previous.previous.base, &out.previous.previous.previous.previous.base, &out.previous.previous.previous.previous.previous.base, &out.previous.previous.previous.previous.previous.previous.base, &out.previous.previous.previous.previous.previous.previous.first.base} {
		if b.ClaimSHA256 != nil {
			b.ClaimSHA256 = cloneWindowHistoricalClaims(b.ClaimSHA256)
		}
	}
	return out
}
func recheckWindowStoppedV9Predecessor(p *historyReadPass, frozen, previous windowStoppedV8Baseline) error {
	if p == nil {
		return checkWindowStoppedV9Predecessor(nil, frozen, previous, nil)
	}
	// Re-running the old recursive decoder here multiplies logical admission
	// without adding dependencies. Its first accepted reads remain registered;
	// Finish freshly re-reads every supplying FD, then final metadata, exact
	// namespaces and epoch locks. Keep the full typed value immutable meanwhile.
	return checkWindowStoppedV9Predecessor(p, frozen, previous, p.ctx.Err())
}
