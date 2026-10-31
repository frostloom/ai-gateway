package taskbench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWindowStoppedV7Exact44Baseline(t *testing.T) {
	b := expectedStoppedV7WindowBaseline()
	if len(b.ClaimSHA256) != 44 || b.PermanentModel != 51200203 || b.PermanentJEV != 399529 || b.TotalModel != 130000000 || b.TotalJEV != 1000000 || b.RemainingModel != 78799797 || b.RemainingJEV != 600471 || b.ActualModelCalls != 3489 || b.ActualJEVCalls != 890 {
		t.Fatal("missing exact stopped44 derivation", b)
	}
	for k, v := range expectedStoppedV6WindowBaseline().ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("ancestor changed", k)
		}
	}
	delete(b.ClaimSHA256, "full1220-v7-20261030/window-000001")
	if len(expectedStoppedV7WindowBaseline().ClaimSHA256) != 44 {
		t.Fatal("shared map")
	}
}

func stoppedV7BundleFixture(t *testing.T) (string, windowActualManifest, map[string][]byte) {
	t.Helper()
	root := stoppedEvidenceRoot(t)
	m := windowActualManifest{ScopeSHA256: strings.Repeat("a", 64), ExecutionSHA256: strings.Repeat("b", 64), CampaignRoot: filepath.Join(root, "campaign"), Limits: windowV4ExpectedLimits()}
	m.Scope.Schema = "full-window-scope.v7"
	m.Scope.ScheduleSHA256 = strings.Repeat("c", 64)
	m.Scope.Bindings.PolicySHA256 = strings.Repeat("d", 64)
	m.Scope.DefinitionSHA256 = strings.Repeat("e", 64)
	m.Scope.ManifestSHA256 = strings.Repeat("f", 64)
	m.Scope.SelectionSHA256 = strings.Repeat("1", 64)
	m.Scope.ProfileSHA256 = strings.Repeat("2", 64)
	files := map[string][]byte{}
	put := func(k string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[k] = raw
	}
	first := expectedStoppedV6WindowBaseline()
	model, jev := first.PermanentModel, first.PermanentJEV
	prevClaim, prevCP := "", ""
	audits := make([]windowAccountingAudit, 11)
	for i := 1; i <= 11; i++ {
		w := fullWindowDeclaration{ID: fmt.Sprintf("window-%03d", i), FirstOrdinal: (i-1)*28 + 1, LastOrdinal: i * 28, fullWindowCaps: fullWindowCaps{ModelCap: 700000, JEVCap: 4586}, MaxModelCalls: 580, MaxJEVCalls: 58}
		if i == 11 {
			w.ModelCap = 734238
			w.JEVCap = 4589
		}
		m.Scope.Windows = append(m.Scope.Windows, w)
		model += w.ModelCap
		jev += w.JEVCap
		c := windowCampaignClaim{Schema: "full-window-campaign-claim.v7", BaselineEvidenceSHA256: windowStoppedV7PreviousSHA, BudgetApprovalSHA256: windowBudget130ApprovalSHA, ParentModel: 130000000, ParentJEV: 1000000, Root: m.CampaignRoot, Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: m.Scope.DefinitionSHA256, ManifestSHA256: m.Scope.ManifestSHA256, SelectionSHA256: m.Scope.SelectionSHA256, ProfileSHA256: m.Scope.ProfileSHA256, ScheduleSHA256: m.Scope.ScheduleSHA256, PolicySHA256: m.Scope.Bindings.PolicySHA256, PreviousClaimSHA256: prevClaim, PreviousCheckpointSHA256: prevCP, Window: w, HistoricalClaims: first.ClaimSHA256, ModelPermanent: model, JEVPermanent: jev}
		name := fmt.Sprintf("campaign/claim-%06d.json", i)
		put(name, c)
		prevClaim = conversationHash(files[name])
		contract := contractWindowCampaign(m.Scope, m.ScopeSHA256, w, m.Limits)
		contract.ClaimSHA256 = prevClaim
		dir := filepath.Join(root, fmt.Sprintf("fixture-ledger-%06d", i))
		l, err := newWindowAccounting(dir, contract)
		if err != nil {
			t.Fatal(err)
		}
		if i == 11 {
			for seq := 1; seq <= 15; seq++ {
				provider, mod, arm := "model", contract.Model.Model, "model"
				if seq > 12 {
					provider, mod, arm = "jev", contract.JEV.Model, "jev_selective"
				}
				id := windowRequestIdentity{Provider: provider, Model: mod, CaseKey: fmt.Sprintf("balance-%03d/answer_complete", seq), Pass: 1, Arm: arm, Turn: 1, Attempt: 1, BodySHA256: strings.Repeat("e", 64), Bytes: 64}
				ticket, err := l.Begin(id)
				if err != nil {
					t.Fatal(err)
				}
				if seq == 15 {
					if err = l.Fail(ticket, "upstream_failure"); err == nil {
						t.Fatal("missing retained failure")
					}
				} else {
					usage := windowUsage{Input: windowInt(1), Output: windowInt(1)}
					if provider == "model" {
						usage.CacheHit = windowInt(0)
						usage.CacheMiss = windowInt(1)
					}
					if err = l.Settle(ticket, strings.Repeat("f", 64), &usage); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		_ = l.Close()
		raw, err := os.ReadFile(filepath.Join(dir, "accounting.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		ledger := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		files[ledger] = raw
		audits[i-1], err = readWindowAccountingBytes(raw)
		if err != nil {
			t.Fatal(err)
		}
		if i < 11 {
			cp := windowCampaignCheckpoint{Evidence: []windowCampaignArmEvidence{}, Schema: "full-window-campaign-owned-checkpoint.v1", Index: i, ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, DefinitionSHA256: m.Scope.DefinitionSHA256, ScheduleSHA256: m.Scope.ScheduleSHA256, PolicySHA256: m.Scope.Bindings.PolicySHA256, ClaimSHA256: prevClaim, LedgerSHA256: conversationHash(raw)}
			name = fmt.Sprintf("campaign/checkpoint-%06d.json", i)
			put(name, cp)
			prevCP = conversationHash(files[name])
		}
	}
	status := FullWindowActualStatus{EvidenceKind: "recorded_run", PlannedPairs: 3660, PlannedArms: 7320, DefinedTurns: 11772, BoundariesExcluded: 1851}
	lastSummary := ""
	for i := 1; i <= 11; i++ {
		w := m.Scope.Windows[i-1]
		a := audits[i-1]
		ledger := fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i)
		r := windowAcquisitionReceipt{Rows: []windowAcquisitionRow{}, Schema: "full-window-acquisition-receipt.v1", EvidenceKind: "recorded_run", Status: "collected_window", ScopeSHA256: m.ScopeSHA256, ExecutionSHA256: m.ExecutionSHA256, WindowIndex: i, WindowID: w.ID, Accounting: a.State, LedgerSHA256: conversationHash(files[ledger]), AccountingSource: "live_owner_snapshot", LedgerCompletionProof: "unavailable_from_journal", Advanced: true, Cleanup: true, Observer: true, PayloadVerified: true, PayloadStatus: "verified_ledger_linkage"}
		if i < 11 {
			r.NextClaimSHA256 = conversationHash(files[fmt.Sprintf("campaign/claim-%06d.json", i+1)])
		} else {
			r.Status = "stopped"
			r.Advanced = false
			r.Cleanup = false
			r.Observer = false
			r.PayloadVerified = false
			r.PayloadStatus = "audit_only"
			r.FirstFailureStage = "arm_proof"
			r.FirstFailureOrdinal = 283
			r.FirstFailureArm = "jev_selective"
			r.AttemptedArms = 6
			r.FinishedArms = 5
			r.ObservedTurns = 15
		}
		payload := windowPayloadManifest{Contract: a.Contract, Packets: []windowPayloadPacket{}}
		pn := fmt.Sprintf("records/window-%06d/payload-manifest.json", i)
		put(pn, payload)
		r.PayloadManifestSHA256 = conversationHash(files[pn])
		rn := fmt.Sprintf("records/window-%06d/receipt.json", i)
		put(rn, r)
		status.LastReceiptSHA256 = lastSummary
		status.NextWindowIndex = i + 1
		status.ModelAttempts += a.State.ModelAttempts
		status.JEVAttempts += a.State.JEVAttempts
		if i == 11 {
			status.Closed = true
			status.NextWindowIndex = 11
		}
		s := FullWindowActualReceipt{FullWindowActualStatus: status, WindowIndex: i, ReportSHA256: conversationHash(files[rn]), PayloadManifestSHA256: r.PayloadManifestSHA256, LedgerSHA256: r.LedgerSHA256, ReportStatus: r.Status}
		sn := fmt.Sprintf("records/window-%06d-summary.json", i)
		put(sn, s)
		lastSummary = conversationHash(files[sn])
	}
	status.LastReceiptSHA256 = lastSummary
	put("records/terminal.json", status)
	put("actual-owner-exit.json", map[string]any{"exit_code": 1, "ended_epoch": 123.0, "relaunch_allowed": false})
	return root, m, files
}
func TestWindowStoppedV7BundleDerives44AndRetainsPending(t *testing.T) {
	root, m, files := stoppedV7BundleFixture(t)
	b, err := readWindowStoppedV7Bundle(root, m, expectedStoppedV6WindowBaseline(), files)
	if err != nil || len(b.ClaimSHA256) != 44 || b.PermanentModel != 51200203 || b.PermanentJEV != 399529 || b.ActualModelCalls != 2351 || b.ActualJEVCalls != 610 {
		t.Fatal(b, err)
	}
}
func TestWindowStoppedV7InventoryOwnedFixtures(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	if err := os.Mkdir(filepath.Join(root, "campaign"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "campaign/a")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	inv := windowStoppedV7Inventory{Root: root, Files: map[string]windowStoppedV7InventoryEntry{"campaign/a": {SHA256: conversationHash([]byte("abc")), Bytes: 3}}}
	_, _, err := readWindowStoppedV7Inventory(root, inv)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWindowStoppedV7BundleRejects(t *testing.T) {
	modes := []string{"old_claim", "old_amount", "missing_claim", "new_claim", "root", "scope", "execution", "approval", "baseline", "previous_claim", "previous_checkpoint", "checkpoint", "cumulative_cny", "cumulative_usd", "overflow", "extra_claim12", "extra_checkpoint11", "terminal_open", "terminal_acquisition", "exit0", "relaunch", "missing_exit", "pending_cleared", "pending_sequence", "held", "settled", "false_cleanup", "false_observer", "false_advanced", "false_payload", "completion", "missing_ledger", "ledger_mutation", "summary", "payload"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			root, m, files := stoppedV7BundleFixture(t)
			first := expectedStoppedV6WindowBaseline()
			change := func(name string, edit func(map[string]any)) {
				var v map[string]any
				if err := json.Unmarshal(files[name], &v); err != nil {
					t.Fatal(err)
				}
				edit(v)
				raw, _ := json.Marshal(v)
				files[name] = raw
			}
			cn := "campaign/claim-000011.json"
			rn := "records/window-000011/receipt.json"
			originalReceipt := string(files[rn])
			switch mode {
			case "old_claim":
				delete(first.ClaimSHA256, "full1220-v6-20261029/window-000001")
			case "old_amount":
				first.PermanentModel--
			case "missing_claim":
				delete(files, cn)
			case "new_claim":
				change(cn, func(v map[string]any) { v["unknown"] = true })
			case "root":
				change(cn, func(v map[string]any) { v["root"] = root })
			case "scope":
				change(cn, func(v map[string]any) { v["scope_sha256"] = strings.Repeat("0", 64) })
			case "execution":
				change(cn, func(v map[string]any) { v["execution_sha256"] = strings.Repeat("0", 64) })
			case "approval":
				change(cn, func(v map[string]any) { v["budget_approval_sha256"] = strings.Repeat("0", 64) })
			case "baseline":
				change(cn, func(v map[string]any) { v["baseline_evidence_sha256"] = strings.Repeat("0", 64) })
			case "previous_claim":
				change(cn, func(v map[string]any) { v["previous_claim_sha256"] = strings.Repeat("0", 64) })
			case "previous_checkpoint":
				change(cn, func(v map[string]any) { v["previous_checkpoint_sha256"] = strings.Repeat("0", 64) })
			case "checkpoint":
				change("campaign/checkpoint-000010.json", func(v map[string]any) { v["claim_sha256"] = strings.Repeat("0", 64) })
			case "cumulative_cny":
				change(cn, func(v map[string]any) { v["permanent_model_micro_cny"] = float64(51200202) })
			case "cumulative_usd":
				change(cn, func(v map[string]any) { v["permanent_jev_micro_usd"] = float64(399528) })
			case "overflow":
				m.Scope.Windows[10].ModelCap = math.MaxInt64
			case "extra_claim12":
				files["campaign/claim-000012.json"] = files[cn]
			case "extra_checkpoint11":
				files["campaign/checkpoint-000011.json"] = files["campaign/checkpoint-000010.json"]
			case "terminal_open":
				change("records/terminal.json", func(v map[string]any) { v["closed"] = false })
			case "terminal_acquisition":
				change("records/terminal.json", func(v map[string]any) { v["acquisition_done"] = true })
			case "exit0":
				change("actual-owner-exit.json", func(v map[string]any) { v["exit_code"] = float64(0) })
			case "relaunch":
				change("actual-owner-exit.json", func(v map[string]any) { v["relaunch_allowed"] = true })
			case "missing_exit":
				delete(files, "actual-owner-exit.json")
			case "pending_cleared":
				change(rn, func(v map[string]any) { delete(v["accounting"].(map[string]any), "pending") })
			case "pending_sequence":
				change(rn, func(v map[string]any) {
					v["accounting"].(map[string]any)["pending"].(map[string]any)["sequence"] = float64(14)
				})
			case "held":
				change(rn, func(v map[string]any) { v["accounting"].(map[string]any)["held_jev_micro_usd"] = float64(0) })
			case "settled":
				change(rn, func(v map[string]any) { delete(v["accounting"].(map[string]any), "halt") })
			case "false_cleanup", "false_observer", "false_advanced", "false_payload":
				key := map[string]string{"false_cleanup": "cleanup", "false_observer": "observer", "false_advanced": "advanced", "false_payload": "payload_verified"}[mode]
				change(rn, func(v map[string]any) { v[key] = true })
			case "completion":
				change("records/window-000011-summary.json", func(v map[string]any) { v["report_status"] = "collected_window" })
			case "missing_ledger":
				delete(files, "campaign/ledger-000011/accounting.jsonl")
			case "ledger_mutation":
				files["campaign/ledger-000011/accounting.jsonl"] = append(files["campaign/ledger-000011/accounting.jsonl"], []byte("{}\n")...)
			case "summary":
				delete(files, "records/window-000010-summary.json")
			case "payload":
				delete(files, "records/window-000011/payload-manifest.json")
			}
			// Rebind the synthetic summary and terminal after receipt edits, so tests
			// reach state/flag checks instead of stopping only at a stale digest.
			if string(files[rn]) != originalReceipt {
				var summary FullWindowActualReceipt
				if err := json.Unmarshal(files["records/window-000011-summary.json"], &summary); err != nil {
					t.Fatal(err)
				}
				summary.ReportSHA256 = conversationHash(files[rn])
				files["records/window-000011-summary.json"], _ = json.Marshal(summary)
				var terminal FullWindowActualStatus
				if err := json.Unmarshal(files["records/terminal.json"], &terminal); err != nil {
					t.Fatal(err)
				}
				terminal.LastReceiptSHA256 = conversationHash(files["records/window-000011-summary.json"])
				files["records/terminal.json"], _ = json.Marshal(terminal)
			}
			if _, err := readWindowStoppedV7Bundle(root, m, first, files); err == nil {

				t.Fatal("accepted", mode)
			}
		})
	}
}
func TestWindowStoppedV7InventoryRejects(t *testing.T) {
	for _, mode := range []string{"modified", "missing", "extra", "symlink", "hardlink", "escape", "absolute", "noncanonical", "wrong_root", "hash", "size", "mutation", "replacement", "directory_alias"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			dir := filepath.Join(root, "campaign")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "a")
			if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			inv := windowStoppedV7Inventory{Root: root, Files: map[string]windowStoppedV7InventoryEntry{"campaign/a": {SHA256: conversationHash([]byte("abc")), Bytes: 3}}}
			_, before, err := readWindowStoppedV7Inventory(root, inv)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "modified":
				err = os.WriteFile(path, []byte("abd"), 0600)
			case "missing":
				err = os.Remove(path)
			case "extra":
				err = os.WriteFile(filepath.Join(dir, "extra"), nil, 0600)
			case "symlink":
				if err = os.Rename(path, filepath.Join(root, "saved")); err == nil {
					err = os.Symlink(filepath.Join(root, "saved"), path)
				}
			case "hardlink":
				err = os.Link(path, filepath.Join(root, "link"))
			case "escape":
				inv.Files["../outside"] = inv.Files["campaign/a"]
			case "absolute":
				inv.Files[path] = inv.Files["campaign/a"]
			case "noncanonical":
				inv.Files["campaign/../campaign/a"] = inv.Files["campaign/a"]
			case "wrong_root":
				inv.Root = dir
			case "hash":
				v := inv.Files["campaign/a"]
				v.SHA256 = strings.Repeat("a", 64)
				inv.Files["campaign/a"] = v
			case "size":
				v := inv.Files["campaign/a"]
				v.Bytes = 4
				inv.Files["campaign/a"] = v
			case "mutation":
				err = os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0))
			case "replacement":
				// Remove-then-create can reuse the inode and hide the identity change.
				replacement := path + ".replacement"
				if err = os.WriteFile(replacement, []byte("abc"), 0600); err == nil {
					err = os.Rename(replacement, path)
				}
			case "directory_alias":
				if err = os.Rename(dir, filepath.Join(root, "saved")); err == nil {
					err = os.Symlink(filepath.Join(root, "saved"), dir)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			_, after, err := readWindowStoppedV7Inventory(root, inv)
			if mode == "mutation" || mode == "replacement" {
				if err != nil {
					t.Fatal(err)
				}
				err = compareWindowStoppedV7Identity(before, after)
			}
			if err == nil {
				t.Fatal("accepted", mode)
			}
		})
	}
}
func TestWindowStoppedV7DescriptorAndCachedFailClosed(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	for _, raw := range []string{`{}`, `{"schema":"full-window-stopped-baseline.v7","human_approved":true}`, `{"schema":"full-window-stopped-baseline.v7","raw_revalidated":true}`} {
		path := filepath.Join(root, "descriptor.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadWindowStoppedV7Baseline(path, conversationHash([]byte(raw))); err == nil {
			t.Fatal("caller hash substituted")
		}
		if _, err := loadWindowStoppedV7Baseline(path, windowStoppedV7DescriptorSHA); err == nil {
			t.Fatal("source hash bypassed")
		}
	}
	for _, mode := range []string{"claim", "sum", "parent", "calls", "unchanged_without_proof"} {
		b := expectedStoppedV7WindowBaseline()
		switch mode {
		case "claim":
			delete(b.ClaimSHA256, "full1220-v7-20261030/window-000001")
		case "sum":
			b.PermanentJEV--
		case "parent":
			b.TotalModel = 140000000
		case "calls":
			b.ActualJEVCalls++
		}
		if _, _, _, err := (windowStoppedV7Baseline{base: b}).revalidateWindowStoppedHistory(); err == nil {
			t.Fatal("cached proof accepted", mode)
		}
	}
}
func TestWindowStoppedV7ActualFixedPathReadOnly(t *testing.T) {
	if os.Getenv("TASKBENCH_STOPPED_V7_ACTUAL_READONLY") != "1" {
		t.Skip("explicit fixed original read-only verification only")
	}
	path := "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v7-freeze-20261030/continuation-design/stopped-v7.private.json"
	b, err := loadWindowStoppedV7Baseline(path, windowStoppedV7DescriptorSHA)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.base, expectedStoppedV7WindowBaseline()) || b.windowStoppedPredecessorRootsV8()[6] != b.originalRoot {
		t.Fatal("wrong read-only data")
	}
	t.Log("fixed original6395 authenticated to reviewed audit; original44 derived; prior audit reasoning remains trusted, no new native/quality proof or readiness")
}

func TestWindowStoppedV7TopReferenceHardlinksRejected(t *testing.T) {
	for _, name := range []string{"descriptor.json", "audit.json", "inventory.json", "approval.json"} {
		t.Run(name, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			path := filepath.Join(root, name)
			raw := []byte("source-bound bytes")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readWindowStoppedV7File(path, conversationHash(raw), 1024); err != nil {
				t.Fatal("single-link positive", err)
			}
			if err := os.Link(path, filepath.Join(root, "external-alias")); err != nil {
				t.Fatal(err)
			}
			if _, err := readWindowStoppedV7File(path, conversationHash(raw), 1024); err == nil {
				t.Fatal("stable hardlink accepted")
			}
		})
	}
}
func TestWindowStoppedV7ReadAfterHardlinkRejected(t *testing.T) {
	for _, stage := range []string{"opened", "final", "after"} {
		t.Run(stage, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			path := filepath.Join(root, "original")
			if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Link(path, filepath.Join(root, "external-alias")); err != nil {
				t.Fatal(err)
			}
			linked, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Same inode/content/mtime/size: the only changed read metadata is Nlink.
			if !os.SameFile(before, linked) || before.Size() != linked.Size() || !before.ModTime().Equal(linked.ModTime()) {
				t.Fatal("fixture altered other identity fields")
			}
			opened, final, after := before, before, before
			switch stage {
			case "opened":
				opened = linked
			case "final":
				final = linked
			case "after":
				after = linked
			}
			if validateWindowStoppedV7ReadMetadata(before, opened, final, after) == nil {
				t.Fatal("read metadata accepted post-read hardlink", stage)
			}
		})
	}
}
func TestWindowStoppedV7PostStreamHardlinkIdentityRejected(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	dir := filepath.Join(root, "campaign")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	inv := windowStoppedV7Inventory{Root: root, Files: map[string]windowStoppedV7InventoryEntry{"campaign/a": {SHA256: conversationHash([]byte("abc")), Bytes: 3}}}
	_, before, err := readWindowStoppedV7Inventory(root, inv)
	if err != nil {
		t.Fatal(err)
	}
	_, after, err := readWindowStoppedV7Inventory(root, inv)
	if err != nil {
		t.Fatal(err)
	}
	if compareWindowStoppedV7Identity(before, after) != nil {
		t.Fatal("stable positive rejected")
	}
	if err = os.Link(path, filepath.Join(root, "external-alias")); err != nil {
		t.Fatal(err)
	}
	linked, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	after["campaign/a"] = linked
	if compareWindowStoppedV7Identity(before, after) == nil {
		t.Fatal("second-stream after metadata hardlink accepted")
	}
}
