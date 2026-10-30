package taskbench

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)

type windowStoppedV7Evidence struct {
	Schema           string           `json:"schema"`
	PreviousBaseline windowActualFile `json:"previous_baseline"`
	OriginalRoot     string           `json:"original_root"`
	BudgetApproval   windowActualFile `json:"budget_approval"`
	StoppedAudit     windowActualFile `json:"stopped_audit"`
	Inventory        windowActualFile `json:"inventory"`
}

// Read-only historical data. This type implements no issuer or ready capability.
type windowStoppedV7Baseline struct {
	base                                                      fullWindowBaseline
	previous                                                  windowStoppedV6Baseline
	evidencePath, evidenceSHA256, originalRoot, historicalDir string
}
type windowStoppedV7InventoryEntry struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type windowStoppedV7Inventory struct {
	Schema              string                                   `json:"schema"`
	Root                string                                   `json:"root"`
	Files               map[string]windowStoppedV7InventoryEntry `json:"files"`
	OwnedFiles          int                                      `json:"actual_campaign_record_files"`
	NoCacheWalk         bool                                     `json:"no_cache_directory_walk"`
	PrivateSettingsRead bool                                     `json:"private_settings_or_keys_read"`
}

func loadWindowStoppedV7Baseline(path, expectedSHA string) (windowStoppedV7Baseline, error) {
	return loadWindowStoppedV7BaselineContext(context.Background(), path, expectedSHA)
}

// Context carries the caller's deadline through every nested historical read.
func loadWindowStoppedV7BaselineContext(ctx context.Context, path, expectedSHA string) (windowStoppedV7Baseline, error) {
	return runHistoryReadPass(ctx, func(p *historyReadPass) (windowStoppedV7Baseline, error) {
		return loadWindowStoppedV7BaselineWithPass(p, path, expectedSHA)
	})
}

func loadWindowStoppedV7BaselineWithPass(p *historyReadPass, path, expectedSHA string) (windowStoppedV7Baseline, error) {
	if p == nil {
		return windowStoppedV7Baseline{}, fmt.Errorf("historical pass required")
	}
	reject := func(reason string) (windowStoppedV7Baseline, error) {
		return windowStoppedV7Baseline{}, fmt.Errorf("stopped v7 evidence rejected: %s", reason)
	}
	// Even a newly hashed descriptor/audit cannot replace the source anchors.
	if expectedSHA != windowStoppedV7DescriptorSHA {
		return reject("descriptor anchor")
	}
	if err := historyStoppedRoot(p, filepath.Dir(path)); err != nil {
		return windowStoppedV7Baseline{}, err
	}
	raw, err := historyStoppedV7File(p, path, expectedSHA, 65536)
	var e windowStoppedV7Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || e.Schema != "full-window-stopped-baseline.v7" || e.PreviousBaseline.SHA256 != windowStoppedV7PreviousSHA || e.BudgetApproval.SHA256 != windowBudget130ApprovalSHA || e.StoppedAudit.SHA256 != windowStoppedV7AuditSHA || e.Inventory.SHA256 != windowStoppedV7InventorySHA {
		return reject("descriptor")
	}
	root := e.OriginalRoot
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || canonical != root || e.StoppedAudit.Path != filepath.Join(root, "stopped-audit/result.json") || e.Inventory.Path != filepath.Join(root, "stopped-audit/inventory.json") {
		return reject("root")
	}
	if err := historyStoppedRoot(p, root); err != nil {
		return windowStoppedV7Baseline{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return reject("root identity")
	}
	previous, err := loadWindowStoppedV6BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous.base, expectedStoppedV6WindowBaseline()) {
		return reject("concrete predecessor")
	}
	for _, p := range previous.windowStoppedPredecessorRootsV7() {
		if !windowPayloadDisjoint(root, p) {
			return reject("crossed roots")
		}
	}
	if _, err = historyStoppedV7File(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject("historical approval")
	}
	var out windowStoppedV7Baseline
	err = historyStoppedLock(p, 7, filepath.Join(root, "campaign/.lock"), func() error {
		// The reviewed audit's reasoning is an explicit trust dependency. Exact bytes
		// authenticate that audit; this does not re-prove its native/quality reasoning.
		if _, err := historyStoppedV7File(p, e.StoppedAudit.Path, e.StoppedAudit.SHA256, 8<<20); err != nil {
			return err
		}
		raw, err := historyStoppedV7File(p, e.Inventory.Path, e.Inventory.SHA256, 8<<20)
		var inv windowStoppedV7Inventory
		if err != nil || decodeWindowActualJSON(raw, &inv) != nil || inv.Schema != "full1220-v7-stopped-explicit-sha-inventory.v1" || inv.Root != root || len(inv.Files) != 6395 || inv.OwnedFiles != 6376 || !inv.NoCacheWalk || inv.PrivateSettingsRead {
			return fmt.Errorf("stopped v7 fixed inventory rejected")
		}
		files, before, err := readWindowStoppedV7InventoryWithPass(p, root, inv)
		if err != nil {
			return err
		}
		var m windowActualManifest
		if conversationHash(files["actual-manifest.json"]) != windowStoppedV7ManifestSHA || decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil {
			return fmt.Errorf("stopped v7 manifest rejected")
		}
		if m.Schema != "full-window-actual-ready.v7" || m.Scope.Schema != "full-window-scope.v7" || m.ArchiveCommit != "616c0abd16451182fbe2f320f73164604363c018" || m.ArchiveTree != "cf1570f382d1e8c2638c5d3e558f3caa4f55ecb1" || m.SourceRoot != filepath.Join(root, "regression/source") || m.ScopeSHA256 != windowStoppedV7ScopeSHA || m.ExecutionSHA256 != windowStoppedV7ExecutionSHA || m.CampaignRoot != filepath.Join(root, "campaign") || m.SessionRoot != filepath.Join(root, "records") || m.ClaimsDir != previous.historicalDir || m.BaselineEvidence == nil || *m.BaselineEvidence != e.PreviousBaseline || !reflect.DeepEqual(m.HistoricalClaims, previous.base.ClaimSHA256) || m.Scope.BaselineEvidenceSHA256 != windowStoppedV7PreviousSHA || m.Scope.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || m.EvidenceKind != "actual" || m.KnowledgeModel != "off" || m.KnowledgeJEV != "off" || m.Scope.AllowPaid || m.Scope.ReadyForPaidRun || m.Limits != windowV4ExpectedLimits() || len(m.Scope.Windows) != 131 || len(m.Scope.Slots) != 3660 {
			return fmt.Errorf("stopped v7 original namespace changed")
		}
		if err = historyStoppedBinaryAttestation(p, files["actual-manifest.json.binary-attestation.json"], m, windowStoppedV7ManifestSHA, filepath.Join(root, "full-window-actual-v7")); err != nil {
			return err
		}
		for i, sha := range windowStoppedV7ClaimPins() {
			if conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) != sha {
				return fmt.Errorf("stopped v7 original claim changed")
			}
		}
		base, err := readWindowStoppedV7Bundle(root, m, previous.base, files)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(base, expectedStoppedV7WindowBaseline()) {
			return fmt.Errorf("stopped v7 derived44 mismatch")
		}
		again, after, err := readWindowStoppedV7InventoryWithPass(p, root, inv)
		if err != nil || !reflect.DeepEqual(files, again) || compareWindowStoppedV7Identity(before, after) != nil {
			return fmt.Errorf("stopped v7 original bytes/identity changed")
		}
		for _, ref := range []windowActualFile{e.StoppedAudit, e.Inventory} {
			if _, err = historyStoppedV7File(p, ref.Path, ref.SHA256, 8<<20); err != nil {
				return err
			}
		}
		out = windowStoppedV7Baseline{base: base, previous: previous, evidencePath: path, evidenceSHA256: expectedSHA, originalRoot: root, historicalDir: previous.historicalDir}
		return nil
	})
	if err != nil {
		return reject(err.Error())
	}
	fresh, err := loadWindowStoppedV6BaselineWithPass(p, e.PreviousBaseline.Path, e.PreviousBaseline.SHA256)
	if err != nil || !reflect.DeepEqual(previous, fresh) {
		return reject("predecessor changed")
	}
	if _, err = historyStoppedV7File(p, path, expectedSHA, 65536); err != nil {
		return reject("descriptor changed")
	}
	if _, err = historyStoppedV7File(p, e.BudgetApproval.Path, e.BudgetApproval.SHA256, 65536); err != nil {
		return reject("approval changed")
	}
	after, err := os.Stat(root)
	if err != nil || !os.SameFile(rootInfo, after) {
		return reject("root replaced")
	}
	return out, nil
}
func (s windowStoppedV7Baseline) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	if !reflect.DeepEqual(s.base, expectedStoppedV7WindowBaseline()) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v7 cached baseline changed")
	}
	fresh, err := loadWindowStoppedV7Baseline(s.evidencePath, s.evidenceSHA256)
	if err != nil || !reflect.DeepEqual(s, fresh) {
		return fullWindowBaseline{}, "", "", fmt.Errorf("stopped v7 historical proof changed")
	}
	return fresh.base, fresh.evidenceSHA256, fresh.originalRoot, nil
}
func (s windowStoppedV7Baseline) windowStoppedPredecessorRootsV8() [7]string {
	p := s.previous.windowStoppedPredecessorRootsV7()
	return [7]string{p[0], p[1], p[2], p[3], p[4], p[5], s.originalRoot}
}

// Limited bundle validator for owned portable fixtures as well as anchored real
// bytes. It cannot issue a concrete baseline; only the loader calls old loaders.
func readWindowStoppedV7Bundle(root string, m windowActualManifest, first fullWindowBaseline, files map[string][]byte) (fullWindowBaseline, error) {
	reject := func(reason string) (fullWindowBaseline, error) {
		return fullWindowBaseline{}, fmt.Errorf("stopped v7 bundle rejected: %s", reason)
	}
	if !reflect.DeepEqual(first, expectedStoppedV6WindowBaseline()) || len(m.Scope.Windows) < 11 || m.CampaignRoot != filepath.Join(root, "campaign") {
		return reject("predecessor/declaration")
	}
	var actor struct {
		ExitCode        *int     `json:"exit_code"`
		EndedEpoch      *float64 `json:"ended_epoch"`
		RelaunchAllowed *bool    `json:"relaunch_allowed"`
	}
	if decodeWindowActualJSON(files["actual-owner-exit.json"], &actor) != nil || actor.ExitCode == nil || *actor.ExitCode != 1 || actor.EndedEpoch == nil || *actor.EndedEpoch <= 0 || math.IsInf(*actor.EndedEpoch, 0) || math.IsNaN(*actor.EndedEpoch) || actor.RelaunchAllowed == nil || *actor.RelaunchAllowed {
		return reject("owner exit")
	}
	base := first
	base.ClaimSHA256 = cloneWindowHistoricalClaims(first.ClaimSHA256)
	previousClaim, previousCheckpoint, previousSummary := "", "", ""
	models, jevs := 0, 0
	var last FullWindowActualReceipt
	for i := 1; i <= 11; i++ {
		cn := fmt.Sprintf("campaign/claim-%06d.json", i)
		sn := fmt.Sprintf("records/window-%06d-summary.json", i)
		rn := fmt.Sprintf("records/window-%06d/receipt.json", i)
		ln := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		pn := fmt.Sprintf("records/window-%06d/payload-manifest.json", i)
		var c windowCampaignClaim
		var summary FullWindowActualReceipt
		var receipt windowAcquisitionReceipt
		var payload windowPayloadManifest
		for name, target := range map[string]any{cn: &c, sn: &summary, rn: &receipt, pn: &payload} {
			if decodeWindowActualJSON(files[name], target) != nil {
				return reject("decode " + name)
			}
		}
		w := m.Scope.Windows[i-1]
		if w.ModelCap <= 0 || w.JEVCap <= 0 || base.PermanentModel > math.MaxInt64-w.ModelCap || base.PermanentJEV > math.MaxInt64-w.JEVCap {
			return reject("cap overflow")
		}
		base.PermanentModel += w.ModelCap
		base.PermanentJEV += w.JEVCap
		if c.Schema != "full-window-campaign-claim.v7" || c.BaselineEvidenceSHA256 != windowStoppedV7PreviousSHA || c.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || c.ParentModel != 130000000 || c.ParentJEV != 1000000 || c.Root != m.CampaignRoot || c.Index != i || c.ScopeSHA256 != m.ScopeSHA256 || c.ExecutionSHA256 != m.ExecutionSHA256 || c.DefinitionSHA256 != m.Scope.DefinitionSHA256 || c.ManifestSHA256 != m.Scope.ManifestSHA256 || c.SelectionSHA256 != m.Scope.SelectionSHA256 || c.ProfileSHA256 != m.Scope.ProfileSHA256 || c.ScheduleSHA256 != m.Scope.ScheduleSHA256 || c.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || c.Window != w || !reflect.DeepEqual(c.HistoricalClaims, first.ClaimSHA256) || c.PreviousClaimSHA256 != previousClaim || c.PreviousCheckpointSHA256 != previousCheckpoint || c.ModelPermanent != base.PermanentModel || c.JEVPermanent != base.PermanentJEV {
			return reject("claim chain")
		}
		claimSHA := conversationHash(files[cn])
		base.ClaimSHA256[fmt.Sprintf("full1220-v7-20261030/window-%06d", i)] = claimSHA
		audit, err := readWindowAccountingBytes(files[ln])
		if err != nil {
			return reject("ledger replay")
		}
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = claimSHA
		if !reflect.DeepEqual(audit.Contract, contract) || !reflect.DeepEqual(payload.Contract, contract) || !reflect.DeepEqual(receipt.Accounting, audit.State) || !audit.State.Closed || audit.State.EnvelopeViolation || audit.JournalSHA256 != conversationHash(files[ln]) || receipt.LedgerSHA256 != audit.JournalSHA256 || receipt.AccountingSource != "live_owner_snapshot" || receipt.LedgerCompletionProof != "unavailable_from_journal" {
			return reject("journal/contract linkage")
		}
		if summary.WindowIndex != i || summary.ReportSHA256 != conversationHash(files[rn]) || summary.LedgerSHA256 != audit.JournalSHA256 || summary.PayloadManifestSHA256 != conversationHash(files[pn]) || receipt.PayloadManifestSHA256 != summary.PayloadManifestSHA256 || receipt.ScopeSHA256 != m.ScopeSHA256 || receipt.ExecutionSHA256 != m.ExecutionSHA256 || receipt.WindowIndex != i || receipt.WindowID != w.ID || receipt.Schema != "full-window-acquisition-receipt.v1" || receipt.EvidenceKind != "recorded_run" || summary.EvidenceKind != "recorded_run" || summary.AcquisitionDone || summary.LastReceiptSHA256 != previousSummary {
			return reject("summary/receipt linkage")
		}
		models += audit.State.ModelAttempts
		jevs += audit.State.JEVAttempts
		if summary.ModelAttempts != models || summary.JEVAttempts != jevs {
			return reject("attempt counters")
		}
		if i < 11 {
			var cp windowCampaignCheckpoint
			cpn := fmt.Sprintf("campaign/checkpoint-%06d.json", i)
			if decodeWindowActualJSON(files[cpn], &cp) != nil || cp.Schema != "full-window-campaign-owned-checkpoint.v1" || cp.Index != i || cp.ScopeSHA256 != m.ScopeSHA256 || cp.ExecutionSHA256 != m.ExecutionSHA256 || cp.DefinitionSHA256 != m.Scope.DefinitionSHA256 || cp.ScheduleSHA256 != m.Scope.ScheduleSHA256 || cp.PolicySHA256 != m.Scope.Bindings.PolicySHA256 || cp.ClaimSHA256 != claimSHA || cp.LedgerSHA256 != audit.JournalSHA256 {
				return reject("checkpoint")
			}
			if summary.Closed || summary.NextWindowIndex != i+1 || summary.ReportStatus != "collected_window" || receipt.Status != "collected_window" || !receipt.Advanced || !receipt.Cleanup || !receipt.Observer || !receipt.PayloadVerified || receipt.NextClaimSHA256 != conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)]) || audit.State.Pending != nil || audit.State.Halt != "" || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 {
				return reject("completed prefix")
			}
			previousCheckpoint = conversationHash(files[cpn])
		} else {
			p := audit.State.Pending
			if !summary.Closed || summary.NextWindowIndex != 11 || summary.ReportStatus != "stopped" || receipt.Status != "stopped" || receipt.Advanced || receipt.Cleanup || receipt.Observer || receipt.PayloadVerified || receipt.PayloadStatus != "audit_only" || receipt.NextClaimSHA256 != "" || receipt.FirstFailureStage != "arm_proof" || receipt.FirstFailureOrdinal != 283 || receipt.FirstFailureArm != "jev_selective" || receipt.AttemptedArms != 6 || receipt.FinishedArms != 5 || receipt.ObservedTurns != 15 || audit.State.Halt != "upstream_failure" || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 2688 || audit.State.ModelAttempts != 12 || audit.State.JEVAttempts != 3 || p == nil || p.Sequence != 15 || p.Request.Provider != "jev" || p.Request.Arm != "jev_selective" || p.Request.Attempt != 1 || len(audit.State.UnknownIntents) != 0 || audit.State.RetryRequired != nil {
				return reject("permanent pending stop")
			}
		}
		previousClaim = claimSHA
		previousSummary = conversationHash(files[sn])
		last = summary
	}
	// Reject later acquisition even in a synthetic bundle; real filesystem inventory
	// also rejects any added owned path before and after the complete stream pass.
	for name := range files {
		if strings.HasPrefix(name, "campaign/claim-") || strings.HasPrefix(name, "campaign/checkpoint-") {
			allowed := false
			for i := 1; i <= 11; i++ {
				if name == fmt.Sprintf("campaign/claim-%06d.json", i) || i < 11 && name == fmt.Sprintf("campaign/checkpoint-%06d.json", i) {
					allowed = true
				}
			}
			if !allowed {
				return reject("later claim/checkpoint")
			}
		}
	}
	var terminal FullWindowActualStatus
	want := last.FullWindowActualStatus
	want.LastReceiptSHA256 = previousSummary
	if decodeWindowActualJSON(files["records/terminal.json"], &terminal) != nil || !reflect.DeepEqual(terminal, want) {
		return reject("terminal")
	}
	if base.PermanentModel > base.TotalModel || base.PermanentJEV > base.TotalJEV {
		return reject("parent overflow")
	}
	base.RemainingModel = base.TotalModel - base.PermanentModel
	base.RemainingJEV = base.TotalJEV - base.PermanentJEV
	base.ActualModelCalls += models
	base.ActualJEVCalls += jevs
	return base, nil
}

// Streaming verification follows only explicit inventory paths. Directory reads
// are restricted to the owned campaign/records namespace, never cache/source.
func readWindowStoppedV7Inventory(root string, inv windowStoppedV7Inventory) (map[string][]byte, map[string]os.FileInfo, error) {
	return readWindowStoppedV7InventoryWithPass(nil, root, inv)
}

func readWindowStoppedV7InventoryWithPass(p *historyReadPass, root string, inv windowStoppedV7Inventory) (map[string][]byte, map[string]os.FileInfo, error) {
	reject := func(reason string) (map[string][]byte, map[string]os.FileInfo, error) {
		return nil, nil, fmt.Errorf("stopped v7 inventory rejected: %s", reason)
	}
	if inv.Root != root || !filepath.IsAbs(root) || filepath.Clean(root) != root || len(inv.Files) == 0 || len(inv.Files) > 6395 {
		return reject("root/count")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return reject("root alias")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return reject("root identity")
	}
	identities := map[string]os.FileInfo{".": rootInfo}
	files := map[string][]byte{}
	dirs := map[string]map[string]bool{}
	for name, entry := range inv.Files {
		if name == "." || !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "\\") || entry.Bytes < 0 || entry.Bytes > 256<<20 || !comparisonDigest(entry.SHA256) {
			return reject("entry path/size/hash")
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		canonical, err = filepath.EvalSymlinks(path)
		if err != nil || canonical != path {
			return reject("entry alias " + name)
		}
		before, err := os.Lstat(path)
		if err != nil || !before.Mode().IsRegular() || before.Size() != entry.Bytes {
			return reject("entry missing/type/size " + name)
		}
		if !windowStoppedV7SingleLink(before) {
			return reject("entry hardlink " + name)
		}
		if p != nil {
			// Uncached direct stream: every original traversal still performs IO.
			// The pass's stream obtains the full identity from its supplying FD,
			// and registers that same observation for the fresh End sweep.
			_, streamErr := p.Stream(historyReadInventoryStream, path, entry.SHA256, max(1, entry.Bytes))
			after, ae := os.Lstat(path)
			if streamErr != nil || ae != nil || validateWindowStoppedV7ReadMetadata(before, before, after, after) != nil || after.Size() != entry.Bytes {
				return reject("entry changed " + name)
			}
			identities[name] = after
		} else {
			fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				return reject("open " + name)
			}
			f := os.NewFile(uintptr(fd), path)
			opened, oe := f.Stat()
			h := sha256.New()
			n, copyErr := io.Copy(h, io.LimitReader(f, entry.Bytes+1))
			final, fe := f.Stat()
			closeErr := f.Close()
			after, ae := os.Lstat(path)
			if oe != nil || fe != nil || ae != nil || copyErr != nil || closeErr != nil || validateWindowStoppedV7ReadMetadata(before, opened, final, after) != nil || n != entry.Bytes || final.Size() != entry.Bytes || fmt.Sprintf("%x", h.Sum(nil)) != entry.SHA256 {
				return reject("entry changed " + name)
			}
			identities[name] = after
		}
		// Retain only bounded structural files, not native/body payloads or source.
		if name == "actual-manifest.json" || name == "actual-manifest.json.binary-attestation.json" || name == "actual-owner-exit.json" || strings.HasPrefix(name, "campaign/claim-") || strings.HasPrefix(name, "campaign/checkpoint-") || strings.HasSuffix(name, "/accounting.jsonl") || name == "records/terminal.json" || strings.HasSuffix(name, "-summary.json") || strings.HasSuffix(name, "/receipt.json") || strings.HasSuffix(name, "/payload-manifest.json") {
			raw, err := historyStoppedV7File(p, path, entry.SHA256, 8<<20)
			if err != nil {
				return reject("structural bytes " + name)
			}
			files[name] = raw
		}
		if strings.HasPrefix(name, "campaign/") || strings.HasPrefix(name, "records/") {
			parts := strings.Split(name, "/")
			for i := 1; i < len(parts); i++ {
				dir := strings.Join(parts[:i], "/")
				if dirs[dir] == nil {
					dirs[dir] = map[string]bool{}
				}
				isDir := i < len(parts)-1
				if old, exists := dirs[dir][parts[i]]; exists && old != isDir {
					return reject("namespace conflict")
				}
				dirs[dir][parts[i]] = isDir
			}
		}
	}
	for dir, names := range dirs {
		path := filepath.Join(root, filepath.FromSlash(dir))
		before, err := os.Lstat(path)
		if err != nil || !before.IsDir() || historyStoppedDir(p, path, names) != nil {
			return reject("owned namespace " + dir)
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
			return reject("owned directory changed")
		}
		identities[dir] = after
	}
	after, err := os.Lstat(root)
	if err != nil || !os.SameFile(rootInfo, after) {
		return reject("root changed")
	}
	return files, identities, nil
}
func compareWindowStoppedV7Identity(a, b map[string]os.FileInfo) error {
	if len(a) != len(b) {
		return fmt.Errorf("stopped v7 identity count changed")
	}
	for name, x := range a {
		y, ok := b[name]
		if !ok || !os.SameFile(x, y) || x.Mode() != y.Mode() || x.Size() != y.Size() || !x.ModTime().Equal(y.ModTime()) || (!x.IsDir() && (!windowStoppedV7SingleLink(x) || !windowStoppedV7SingleLink(y))) {
			return fmt.Errorf("stopped v7 identity changed: %s", name)
		}
	}
	return nil
}

// Local boundary for the v7 anchors; legacy loaders retain their behavior.
// The direct descriptor/audit/inventory/approval references are outside the
// historical inventory, so they require the same single-link checks locally.
func readWindowStoppedV7File(path, sha string, max int64) ([]byte, error) {
	if !comparisonDigest(sha) || !filepath.IsAbs(path) || filepath.Clean(path) != path || max < 1 || max > 256<<20 {
		return nil, fmt.Errorf("stopped v7 bounded anchored file required")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return nil, fmt.Errorf("stopped v7 file alias")
	}
	before, err := os.Lstat(path)
	if err != nil || !windowStoppedV7SingleLink(before) || before.Size() > max {
		return nil, fmt.Errorf("stopped v7 single-link file required")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	opened, oe := f.Stat()
	if oe != nil || validateWindowStoppedV7ReadMetadata(before, opened, opened, opened) != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stopped v7 opened file changed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, max+1))
	final, fe := f.Stat()
	after, ae := os.Lstat(path)
	closeErr := f.Close()
	if readErr != nil || fe != nil || ae != nil || closeErr != nil || validateWindowStoppedV7ReadMetadata(before, opened, final, after) != nil || int64(len(raw)) > max || int64(len(raw)) != opened.Size() || conversationHash(raw) != sha {
		return nil, fmt.Errorf("stopped v7 file bytes/identity/link count changed")
	}
	return raw, nil
}
func windowStoppedV7SingleLink(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
func validateWindowStoppedV7ReadMetadata(before, opened, final, after os.FileInfo) error {
	for _, info := range []os.FileInfo{before, opened, final, after} {
		if !windowStoppedV7SingleLink(info) {
			return fmt.Errorf("stopped v7 file single-link metadata required")
		}
	}
	if !os.SameFile(before, opened) || !os.SameFile(opened, final) || !os.SameFile(opened, after) || before.Mode() != opened.Mode() || before.Mode() != final.Mode() || before.Mode() != after.Mode() || before.Size() != opened.Size() || before.Size() != final.Size() || before.Size() != after.Size() || !before.ModTime().Equal(opened.ModTime()) || !before.ModTime().Equal(final.ModTime()) || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("stopped v7 read metadata changed")
	}
	return nil
}
