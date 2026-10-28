package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowStoppedV3PinsAndIndependentBaseline(t *testing.T) {
	pins := windowStoppedV3ArtifactPins()
	if len(pins) != 57 {
		t.Fatalf("pins=%d want57", len(pins))
	}
	names := []string{"actual-manifest.json", "actual-owner-exit.json", "records/startup.json", "records/terminal.json"}
	for i := 1; i <= 9; i++ {
		names = append(names, fmt.Sprintf("campaign/claim-%06d.json", i), fmt.Sprintf("campaign/ledger-%06d/accounting.jsonl", i), fmt.Sprintf("records/window-%06d-summary.json", i), fmt.Sprintf("records/window-%06d/receipt.json", i), fmt.Sprintf("records/window-%06d/payload-manifest.json", i))
		if i < 9 {
			names = append(names, fmt.Sprintf("campaign/checkpoint-%06d.json", i))
		}
	}
	for _, name := range names {
		if !comparisonDigest(pins[name]) {
			t.Fatal("missing fixed pin", name)
		}
	}
	b := expectedStoppedV3WindowBaseline()
	if len(b.ClaimSHA256) != 25 || b.PermanentModel != 38888170 || b.PermanentJEV != 308003 || b.RemainingModel != 66111830 || b.RemainingJEV != 691997 || b.ActualModelCalls != 1791 || b.ActualJEVCalls != 445 || b.TotalModel != 105000000 || b.TotalJEV != 1000000 {
		t.Fatal("wrong stopped25 baseline", b)
	}
	for id, sha := range expectedStoppedV2WindowBaseline().ClaimSHA256 {
		if b.ClaimSHA256[id] != sha {
			t.Fatal("ancestor changed", id)
		}
	}
	for i := 1; i <= 9; i++ {
		if b.ClaimSHA256[fmt.Sprintf("full1220-v3-20261028/window-%06d", i)] != pins[fmt.Sprintf("campaign/claim-%06d.json", i)] {
			t.Fatal("new claim omitted", i)
		}
	}
	delete(pins, "actual-owner-exit.json")
	delete(b.ClaimSHA256, "full1220-v3-20261028/window-000009")
	if len(windowStoppedV3ArtifactPins()) != 57 || len(expectedStoppedV3WindowBaseline().ClaimSHA256) != 25 {
		t.Fatal("mutable expected evidence")
	}
}
func stoppedV3NamespaceFixture() (windowActualManifest, windowStoppedV3Evidence, windowStoppedV2Baseline) {
	previous := windowStoppedV2Baseline{base: expectedStoppedV2WindowBaseline(), first: windowStoppedBaseline{base: expectedStoppedWindowBaseline(), originalRoot: "/old/first", historicalDir: "/old/claims", liabilityMicroCNY: 3832}, originalRoot: "/old/second", historicalDir: "/old/claims", evidencePath: "/old/baseline-v2.json", evidenceSHA256: "857bc856db8ee7837f9043a97fd98080d7e881c78fae22a6f6a789d0bbbd4ab3"}
	e := windowStoppedV3Evidence{Schema: "full-window-stopped-baseline.v3", PreviousBaseline: windowActualFile{Path: previous.evidencePath, SHA256: previous.evidenceSHA256}, OriginalRoot: "/old/third", BudgetApproval: windowActualFile{Path: "/old/approval.json", SHA256: windowBudget105ApprovalSHA}}
	m := windowActualManifest{Schema: "full-window-actual-ready.v3", EvidenceKind: "actual", ArchiveCommit: "f48bb0ac8ecc690605c4f2662b7ad3250b6888a7", KnowledgeModel: "off", KnowledgeJEV: "off", CampaignRoot: "/old/third/campaign", SessionRoot: "/old/third/records", ClaimsDir: previous.historicalDir, BaselineEvidence: &e.PreviousBaseline, HistoricalClaims: cloneWindowHistoricalClaims(previous.base.ClaimSHA256), Scope: fullWindowScope{Schema: "full-window-scope.v3", BudgetApprovalSHA256: windowBudget105ApprovalSHA, BaselineEvidenceSHA256: previous.evidenceSHA256}}
	return m, e, previous
}
func TestWindowStoppedV3ConcreteNamespaceAndThreeDistinctRoots(t *testing.T) {
	m, e, p := stoppedV3NamespaceFixture()
	if err := validateWindowStoppedV3Namespace(m, e, p); err != nil {
		t.Fatal("concrete namespace rejected", err)
	}
	proof := windowStoppedV3Baseline{previous: p, originalRoot: e.OriginalRoot}
	if proof.windowStoppedPredecessorRootsV4() != [3]string{"/old/first", "/old/second", "/old/third"} {
		t.Fatal("three roots lost")
	}
	if _, ok := any(proof).(windowStoppedV3History); ok {
		t.Fatal("new25proof must not satisfy old16 executing interface")
	}
	for _, mutate := range []func(*windowActualManifest, *windowStoppedV3Evidence, *windowStoppedV2Baseline){
		func(m *windowActualManifest, _ *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			m.Schema = "full-window-actual-ready.v2"
		},
		func(m *windowActualManifest, _ *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			m.Scope.ReadyForPaidRun = true
		},
		func(m *windowActualManifest, _ *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			m.KnowledgeModel = "tool"
		},
		func(m *windowActualManifest, _ *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			m.SessionRoot = "/new/records"
		},
		func(_ *windowActualManifest, e *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			e.OriginalRoot = "/old/first"
		},
		func(_ *windowActualManifest, e *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			e.OriginalRoot = "/old/second/child"
		},
		func(m *windowActualManifest, _ *windowStoppedV3Evidence, _ *windowStoppedV2Baseline) {
			m.Scope.BudgetApprovalSHA256 = strings.Repeat("a", 64)
		},
		func(_ *windowActualManifest, _ *windowStoppedV3Evidence, p *windowStoppedV2Baseline) {
			p.base.TotalModel = 100000000
		},
		func(_ *windowActualManifest, _ *windowStoppedV3Evidence, p *windowStoppedV2Baseline) {
			p.base.TotalJEV = 105000000
		},
	} {
		a, b, c := stoppedV3NamespaceFixture()
		mutate(&a, &b, &c)
		if validateWindowStoppedV3Namespace(a, b, c) == nil {
			t.Fatal("invalid namespace accepted")
		}
	}
	for id := range p.base.ClaimSHA256 {
		for _, mode := range []string{"missing", "changed"} {
			a, b, c := stoppedV3NamespaceFixture()
			if mode == "missing" {
				delete(c.base.ClaimSHA256, id)
			} else {
				c.base.ClaimSHA256[id] = strings.Repeat("f", 64)
			}
			if validateWindowStoppedV3Namespace(a, b, c) == nil {
				t.Fatal("bad ancestor", id, mode)
			}
		}
	}
}
func stoppedV3NativeFixture(t *testing.T) (string, windowPayloadManifest, []byte) {
	t.Helper()
	root := stoppedEvidenceRoot(t)
	c := windowTestContract()
	c.Model = windowProtocolLimits("model")
	c.Model.MaxInput = 36864
	c.JEV = windowProtocolLimits("jev")
	c.ModelCap = 1000000
	c.JEVCap = 1000000
	store, err := newWindowPayloadStore(filepath.Join(root, "payload"), filepath.Join(root, "campaign"), c, "recorded_run")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ledgerDir := filepath.Join(root, "ledger")
	ledger, err := newWindowAccounting(ledgerDir, c)
	if err != nil {
		t.Fatal(err)
	}
	request, response := []byte(windowProtocolModelBody), []byte(windowStandardTierResponse)
	id := payloadRequest("model", request)
	id.Model = c.Model.Model
	ticket, err := ledger.Begin(id)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := parseWindowResponseUsage("model", response, c.Model)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.writeWindowRequest(1, id, request); err != nil {
		t.Fatal(err)
	}
	if err = store.writeWindowResponse(1, id, 200, response, true, "complete"); err != nil {
		t.Fatal(err)
	}
	if err = ledger.Settle(ticket, conversationHash(response), usage); err != nil {
		t.Fatal(err)
	}
	if err = ledger.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := verifyWindowPayloadStore(store)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(ledgerDir, "accounting.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return store.dir, manifest, raw
}
func TestWindowStoppedV3NativeExactLedgerUsageWithoutWrites(t *testing.T) {
	dir, m, raw := stoppedV3NativeFixture(t)
	before := append([]byte{}, raw...)
	audit, err := readWindowStoppedV3Native(dir, m, raw)
	if err != nil || audit.State.ModelAttempts != 1 || audit.State.JEVAttempts != 0 || audit.State.Pending != nil || audit.State.ModelHeld != 0 {
		t.Fatal("complete ledger/native rejected", err, audit.State)
	}
	if !bytes.Equal(before, raw) {
		t.Fatal("ledger input mutated")
	}
	for _, name := range []string{"request-000001.body", "response-000001.body", "request-000001.json", "response-000001.json", "store.json"} {
		for _, mode := range []string{"missing", "changed"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				d, p, l := stoppedV3NativeFixture(t)
				path := filepath.Join(d, name)
				if mode == "missing" {
					os.Remove(path)
				} else {
					os.WriteFile(path, []byte("bad"), 0600)
				}
				if _, err := readWindowStoppedV3Native(d, p, l); err == nil {
					t.Fatal("descendant change accepted")
				}
			})
		}
	}
	for _, name := range []string{"response-000002.body", "extra-sidecar.json", "window-010"} {
		t.Run("extra/"+name, func(t *testing.T) {
			d, p, l := stoppedV3NativeFixture(t)
			os.WriteFile(filepath.Join(d, name), []byte("unaccounted"), 0600)
			if _, err := readWindowStoppedV3Native(d, p, l); err == nil {
				t.Fatal("extra accepted")
			}
		})
	}
}
func TestWindowStoppedV3RejectsNonmatchingLedgerEvenIfNativeManifestMatches(t *testing.T) {
	for _, mode := range []string{"response_hash", "usage", "intent", "status"} {
		t.Run(mode, func(t *testing.T) {
			dir, m, raw := stoppedV3NativeFixture(t)
			var events []windowAccountingEvent
			for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
				var event windowAccountingEvent
				if json.Unmarshal(line, &event) != nil {
					t.Fatal("fixture")
				}
				events = append(events, event)
			}
			switch mode {
			case "response_hash":
				events[2].ResponseSHA256 = strings.Repeat("a", 64)
			case "usage":
				*events[2].Usage.Output++
			case "intent":
				events[1].Request.CaseKey = "other/task"
			case "status":
				m.Packets[0].Response.Available = false
			}
			var changed bytes.Buffer
			for i := range events {
				if i > 0 {
					events[i].PreviousSHA256 = conversationHash(bytes.TrimSuffix(changed.Bytes(), []byte("\n"))[bytes.LastIndexByte(bytes.TrimSuffix(changed.Bytes(), []byte("\n")), '\n')+1:])
				}
				line, _ := json.Marshal(events[i])
				changed.Write(line)
				changed.WriteByte('\n')
			}
			if reflect.DeepEqual(raw, changed.Bytes()) && mode != "status" {
				t.Fatal("mutation ineffective")
			}
			if _, err := readWindowStoppedV3Native(dir, m, changed.Bytes()); err == nil {
				t.Fatal("ledger/native mismatch accepted", mode)
			}
		})
	}
}

func TestWindowStoppedV3DescriptorFailClosedWithoutCreatingOwners(t *testing.T) {
	for _, mode := range []string{"schema", "previous_sha", "approval_sha", "approval_missing", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			e := windowStoppedV3Evidence{Schema: "full-window-stopped-baseline.v3", PreviousBaseline: windowActualFile{Path: filepath.Join(root, "missing-previous"), SHA256: windowStoppedV3PreviousSHA}, OriginalRoot: filepath.Join(root, "absent-owner"), BudgetApproval: windowActualFile{Path: filepath.Join(root, "missing-approval"), SHA256: windowBudget105ApprovalSHA}}
			switch mode {
			case "schema":
				e.Schema = "full-window-stopped-baseline.v2"
			case "previous_sha":
				e.PreviousBaseline.SHA256 = strings.Repeat("b", 64)
			case "approval_sha":
				e.BudgetApproval.SHA256 = strings.Repeat("b", 64)
			}
			raw, _ := json.Marshal(e)
			if mode == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"verified":true}`)...)
			}
			path := filepath.Join(root, "descriptor.json")
			os.WriteFile(path, raw, 0600)
			if _, err := loadWindowStoppedV3Baseline(path, conversationHash(raw)); err == nil {
				t.Fatal("caller assertion accepted")
			}
			if _, err := os.Lstat(e.OriginalRoot); !os.IsNotExist(err) {
				t.Fatal("reader created owner root")
			}
		})
	}
}
func TestWindowStoppedV3RejectsCachedClaimOrCurrencyChanges(t *testing.T) {
	for id := range expectedStoppedV3WindowBaseline().ClaimSHA256 {
		for _, mode := range []string{"missing", "changed"} {
			t.Run(mode+"/"+id, func(t *testing.T) {
				b := expectedStoppedV3WindowBaseline()
				if mode == "missing" {
					delete(b.ClaimSHA256, id)
				} else {
					b.ClaimSHA256[id] = strings.Repeat("f", 64)
				}
				p := windowStoppedV3Baseline{base: b}
				if _, _, _, err := p.revalidateWindowStoppedHistory(); err == nil {
					t.Fatal("changed cached claim accepted")
				}
			})
		}
	}
	for _, mode := range []string{"100cny", "currencies", "model_count", "jev_count"} {
		t.Run(mode, func(t *testing.T) {
			b := expectedStoppedV3WindowBaseline()
			switch mode {
			case "100cny":
				b.TotalModel = 100000000
			case "currencies":
				b.TotalModel, b.TotalJEV = b.TotalJEV, b.TotalModel
			case "model_count":
				b.ActualModelCalls++
			case "jev_count":
				b.ActualJEVCalls++
			}
			if _, _, _, err := (windowStoppedV3Baseline{base: b}).revalidateWindowStoppedHistory(); err == nil {
				t.Fatal("changed cached totals accepted")
			}
		})
	}
}
func TestWindowStoppedV3NativeReadonlyExistingLockAndNullableRecord(t *testing.T) {
	path := filepath.Join(stoppedEvidenceRoot(t), ".lock")
	os.WriteFile(path, nil, 0600)
	os.Chmod(path, 0400)
	if err := withWindowStoppedLock(path, func() error {
		info, err := os.Stat(path)
		if err != nil || info.Size() != 0 || info.Mode().Perm() != 0400 {
			t.Fatal("lock modified")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(collectionRecording{PolicyID: collectionPolicyID, PolicySHA256: collectionPolicySHA()})
	if _, err := decodeWindowStoppedV2Recording(raw); err != nil {
		t.Fatal("legal null changed", err)
	}
	var ready collectionRecording
	if decodeWindowActualJSON(raw, &ready) == nil {
		t.Fatal("readiness decoder weakened")
	}
}
