package taskbench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

type windowStoppedV8Evidence struct {
	Schema             string           `json:"schema"`
	PreviousBaseline   windowActualFile `json:"previous_baseline"`
	OriginalRoot       string           `json:"original_root"`
	BudgetApproval     windowActualFile `json:"budget_approval"`
	Manifest           windowActualFile `json:"manifest"`
	StoppedAudit       windowActualFile `json:"stopped_audit"`
	Inventory          windowActualFile `json:"inventory"`
	SourceConfirmation windowActualFile `json:"source_confirmation"`
	OwnerExit          windowActualFile `json:"owner_exit"`
}

// Data only; deliberately implements no issuer, ready or acquisition capability.
type windowStoppedV8Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV7Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}
type windowStoppedV8InventoryEntry struct {
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	MtimeNS int64  `json:"mtime_ns"`
}
type windowStoppedV8Inventory struct {
	Schema   string                                   `json:"schema"`
	Root     string                                   `json:"root"`
	Files    map[string]windowStoppedV8InventoryEntry `json:"files"`
	Count    int                                      `json:"file_count"`
	Bytes    int64                                    `json:"total_bytes"`
	Closed   bool                                     `json:"scope_permanently_closed"`
	NoResume bool                                     `json:"no_relaunch_no_resume_no_release"`
}

func validateWindowStoppedV8Descriptor(e windowStoppedV8Evidence) error {
	root := e.OriginalRoot
	if e.Schema != "full-window-stopped-baseline.v8" || !filepath.IsAbs(root) || filepath.Clean(root) != root || e.PreviousBaseline.SHA256 != windowV8BaselineSHA || !filepath.IsAbs(e.PreviousBaseline.Path) || filepath.Clean(e.PreviousBaseline.Path) != e.PreviousBaseline.Path {
		return fmt.Errorf("stopped v8 descriptor identity")
	}
	refs := []struct {
		ref       windowActualFile
		name, sha string
	}{
		{e.BudgetApproval, "human-approval-reply.private.json", windowStoppedV8ApprovalSHA},
		{e.Manifest, "actual-manifest.json", windowStoppedV8ManifestSHA},
		{e.StoppedAudit, "score-preparation/stopped-delivery/audit-source/data-audit.json", windowStoppedV8AuditSHA},
		{e.Inventory, "stop-diagnosis/original-inventory.json", windowStoppedV8InventorySHA},
		{e.SourceConfirmation, "semantic-review/source-binding-confirmation.json", windowStoppedV8SourceConfirmationSHA},
		{e.OwnerExit, "actual-owner-exit.json", windowStoppedV8OwnerExitSHA},
	}
	for _, r := range refs {
		if r.ref.Path != filepath.Join(root, r.name) || r.ref.SHA256 != r.sha {
			return fmt.Errorf("stopped v8 fixed reference %s", r.name)
		}
	}
	return nil
}
func loadWindowStoppedV8Baseline(path, expectedSHA string) (windowStoppedV8Baseline, error) {
	return loadWindowStoppedV8BaselineContext(context.Background(), path, expectedSHA)
}
func loadWindowStoppedV8BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV8Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV8Baseline, error) {
		return loadWindowStoppedV8BaselineWithPass(p, path, expectedSHA)
	})
}
func loadWindowStoppedV8BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV8Baseline, error) {
	reject := func(reason string) (windowStoppedV8Baseline, error) {
		return windowStoppedV8Baseline{}, fmt.Errorf("stopped v8 evidence rejected: %s", reason)
	}
	if p == nil || expectedSHA != windowStoppedV8DescriptorSHA {
		return reject("concrete descriptor and owned history pass required")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return reject("descriptor directory")
	}
	raw, err := historyStoppedV7File(p, path, expectedSHA, 65536)
	var e windowStoppedV8Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || validateWindowStoppedV8Descriptor(e) != nil {
		return reject("descriptor")
	}
	root := e.OriginalRoot
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return reject("root alias")
	}
	if err = historyStoppedRoot(p, root); err != nil {
		return reject("root identity")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return reject("root identity")
	}
	previous, err := loadWindowStoppedV7BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV7WindowBaseline()) {
		return reject("concrete predecessor")
	}
	for _, old := range previous.windowStoppedPredecessorRootsV8() {
		if !windowPayloadDisjoint(root, old) {
			return reject("crossed historical roots")
		}
	}
	var out windowStoppedV8Baseline
	err = historyStoppedLock(p, 8, filepath.Join(root, "campaign/.lock"), func() error {
		// These exact external audit/source-confirmation bytes are reviewed reasoning
		// dependencies. They do not confer completed-window/quality/score authority.
		for _, ref := range []windowActualFile{e.BudgetApproval, e.StoppedAudit, e.SourceConfirmation} {
			if _, err := historyStoppedV7File(p, ref.Path, ref.SHA256, 8<<20); err != nil {
				return err
			}
		}
		raw, err := historyStoppedV7File(p, e.Inventory.Path, e.Inventory.SHA256, 8<<20)
		var inv windowStoppedV8Inventory
		if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "full1220-v8-stopped-original-inventory.v1" || inv.Root != root || inv.Count != 451 || len(inv.Files) != 451 || inv.Bytes != 2283033 || !inv.Closed || !inv.NoResume {
			return fmt.Errorf("stopped v8 fixed451 inventory")
		}
		converted := windowStoppedV7Inventory{Root: root, Files: map[string]windowStoppedV7InventoryEntry{}}
		var total int64
		for name, entry := range inv.Files {
			if entry.Bytes < 0 || entry.Bytes > 256<<20 || total > 2283033-entry.Bytes {
				return fmt.Errorf("stopped v8 inventory length")
			}
			total += entry.Bytes
			converted.Files[name] = windowStoppedV7InventoryEntry{SHA256: entry.SHA256, Bytes: entry.Bytes}
		}
		if total != 2283033 {
			return fmt.Errorf("stopped v8 inventory total")
		}
		files, before, err := readWindowStoppedV7InventoryWithPass(p, root, converted)
		if err != nil {
			return err
		}
		manifest, err := historyStoppedV7File(p, e.Manifest.Path, e.Manifest.SHA256, 8<<20)
		var m windowActualManifest
		if err != nil || decodeWindowActualJSON(manifest, &m) != nil || m.Schema != "full-window-actual-ready.v8" || m.Scope.Schema != "full-window-scope.v8" || m.ArchiveCommit != "aa9147b90406559b9ea9181f9db75f166dd39fce" || m.ArchiveTree != "231610519a199403eb445ab6d4135723d748b468" || m.ScopeSHA256 != "d5acd131228872cd476d73e04782829e9311894d8928715a531b87e7ae03cd6e" || m.ExecutionSHA256 != "43363c8a6ff43448157f4cd51fb7fa12d123e0da06c91db590121b2eb3f1aee8" || m.SourceRoot != filepath.Join(root, "regression/source") || m.CampaignRoot != filepath.Join(root, "campaign") || m.SessionRoot != filepath.Join(root, "records") || m.ClaimsDir != previous.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.PreviousBaseline || !reflect.DeepEqual(m.HistoricalClaims, previous.base.ClaimSHA256) || m.Limits != windowV4ExpectedLimits() || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || validateWindowV8Declarations(m.Scope) != nil {
			return fmt.Errorf("stopped v8 original manifest")
		}
		attestation, err := historyStoppedV7File(p, e.Manifest.Path+".binary-attestation.json", "2bad34053ff3c71d66110399c68678f8807182f81b8b5aa49ec511d475052e95", 65536)
		if err != nil {
			return err
		}
		if err = historyStoppedBinaryAttestation(p, attestation, m, e.Manifest.SHA256, filepath.Join(root, "full-window-actual-v8")); err != nil {
			return err
		}
		owner, err := historyStoppedV7File(p, e.OwnerExit.Path, e.OwnerExit.SHA256, 65536)
		if err != nil {
			return err
		}
		files["actual-owner-exit.json"] = owner
		for i, sha := range windowStoppedV8ClaimPins() {
			if conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) != sha {
				return fmt.Errorf("stopped v8 claim anchor")
			}
		}
		base, err := readWindowStoppedV8Bundle(root, m, previous.base, files)
		if err != nil || !reflect.DeepEqual(base, expectedStoppedV8WindowBaseline()) {
			return fmt.Errorf("stopped v8 derived46 mismatch: %v", err)
		}
		again, after, err := readWindowStoppedV7InventoryWithPass(p, root, converted)
		delete(files, "actual-owner-exit.json")
		if err != nil || !reflect.DeepEqual(files, again) || compareWindowStoppedV7Identity(before, after) != nil {
			return fmt.Errorf("stopped v8 original bytes/identity changed")
		}
		out = windowStoppedV8Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: root, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return reject(err.Error())
	}
	// Same explicit pass: semantic predecessors still run twice, physical reads
	// are fresh at both pass endpoints rather than starting a recursive new pass.
	fresh, err := loadWindowStoppedV7BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous, fresh) {
		return reject("predecessor changed")
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
func (s windowStoppedV8Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if !reflect.DeepEqual(s.base, expectedStoppedV8WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v8 cached baseline changed")
	}
	fresh, err := loadWindowStoppedV8Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v8 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
func (s windowStoppedV8Baseline) windowStoppedPredecessorRootsV9() [8]string {
	p := s.previous.windowStoppedPredecessorRootsV8()
	return [8]string{p[0], p[1], p[2], p[3], p[4], p[5], p[6], s.originalRoot}
}
