package taskbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowStoppedV6Exact33Baseline(t *testing.T) {
	b := expectedStoppedV6WindowBaseline()
	if len(b.ClaimSHA256) != 33 || b.TotalModel != 130000000 || b.TotalJEV != 1000000 || b.PermanentModel != 43465965 || b.PermanentJEV != 349080 || b.RemainingModel != 86534035 || b.RemainingJEV != 650920 || b.ActualModelCalls != 2339 || b.ActualJEVCalls != 607 {
		t.Fatal("wrong historical33", b)
	}
	raw, _ := json.Marshal(b.ClaimSHA256)
	if conversationHash(raw) != "2b67c23b0b3b6c48e676fff5dfbc3587ab5f2432e96acb3824f931130fd75ede" {
		t.Fatal("map33 changed")
	}
	for k, v := range expectedStoppedV5WindowBaseline().ClaimSHA256 {
		if b.ClaimSHA256[k] != v {
			t.Fatal("ancestor changed", k)
		}
	}
	delete(b.ClaimSHA256, "full1220-v6-20261029/window-000001")
	if len(expectedStoppedV6WindowBaseline().ClaimSHA256) != 33 || expectedStoppedV5WindowBaseline().TotalModel != 105000000 {
		t.Fatal("shared map or rewritten parent")
	}
}
func TestWindowStoppedV6CachedTamperingRejected(t *testing.T) {
	for _, mode := range []string{"claim", "liability", "parent", "calls", "remaining"} {
		t.Run(mode, func(t *testing.T) {
			b := expectedStoppedV6WindowBaseline()
			switch mode {
			case "claim":
				delete(b.ClaimSHA256, "full1220-v6-20261029/window-000001")
			case "liability":
				b.PermanentModel--
			case "parent":
				b.TotalModel = 105000000
			case "calls":
				b.ActualJEVCalls--
			case "remaining":
				b.RemainingJEV++
			}
			if _, _, _, err := (windowStoppedV6Baseline{base: b}).revalidateWindowStoppedHistory(); err == nil {
				t.Fatal("tamper accepted")
			}
		})
	}
}
func TestWindowStoppedV6DescriptorFailClosed(t *testing.T) {
	dir := stoppedEvidenceRoot(t)
	for _, raw := range []string{`{}`, `{"schema":"full-window-stopped-baseline.v6","human_approved":true}`, `{"schema":"full-window-stopped-baseline.v5"}`} {
		path := filepath.Join(dir, "descriptor.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadWindowStoppedV6Baseline(path, conversationHash([]byte(raw))); err == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
}
func TestWindowStoppedV6ActualReadOnly(t *testing.T) {
	parallelWindowV7HeavyTest(t, true)
	path := os.Getenv("TASKBENCH_STOPPED_V6_EVIDENCE")
	if path == "" {
		t.Skip("explicit original readonly evidence not supplied")
	}
	p, err := loadWindowStoppedV6Baseline(path, "ccf528106789daf05a61c8d8f4e5f72d3a29eea65d78a6f81afb85f4e1ea45ad")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.base, expectedStoppedV6WindowBaseline()) {
		t.Fatal("wrong base")
	}
	fresh, sha, root, err := p.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(fresh, p.base) || sha != p.evidenceSHA256 || root != p.originalRoot {
		t.Fatal("fresh history", err)
	}
	roots := p.windowStoppedPredecessorRootsV7()
	if len(roots) != 6 || roots[5] != p.originalRoot {
		t.Fatal("roots")
	}
	if _, ok := any(p).(windowStoppedV4History); ok {
		t.Fatal("old capability promoted")
	}
	for _, name := range []string{"checkpoint-000001.json", "claim-000002.json"} {
		if _, err := os.Lstat(filepath.Join(p.originalRoot, "campaign", name)); !os.IsNotExist(err) {
			t.Fatal("unexpected history file", name, err)
		}
	}
}

func stoppedV6NativeFixture(t *testing.T) (string, windowPayloadManifest, []byte) {
	t.Helper()
	root := stoppedEvidenceRoot(t)
	c := windowCacheContract()
	c.Model.Model = "deepseek-flash"
	c.Model.MaxRequestBytes = 32768
	c.JEV = windowProviderLimits{Model: "jev-1.13.0", MaxAttempts: 10, MaxRequestBytes: 4096, MaxInput: 64000, MaxOutput: 1000000, InputRate: 42000}
	c.ModelCap = 1000000
	c.JEVCap = 1000000
	store, err := newWindowPayloadStore(filepath.Join(root, "payload"), filepath.Join(root, "campaign"), c, "recorded_run")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dir := filepath.Join(root, "ledger")
	l, err := newWindowAccounting(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(windowProtocolModelBody)
	response := []byte(`{"model":"deepseek-flash","usage":{"input_tokens":221,"cache_read_input_tokens":1536,"output_tokens":33}}`)
	id := payloadRequest("model", request)
	id.Model = c.Model.Model
	ticket, err := l.Begin(id)
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
	if err = l.Settle(ticket, conversationHash(response), usage); err != nil {
		t.Fatal(err)
	}
	_ = l.halt("invalid_request")
	if err = l.Close(); err == nil || err.Error() != "window halted: invalid_request" {
		t.Fatal("expected retained halt on close", err)
	}
	m, _, err := verifyWindowPayloadStore(store)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "accounting.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return store.dir, m, raw
}
func TestWindowStoppedV6NativeWeightedStop(t *testing.T) {
	for _, mode := range []string{"valid", "request", "response", "ledger", "pending", "extra", "alias"} {
		t.Run(mode, func(t *testing.T) {
			dir, payload, ledger := stoppedV6NativeFixture(t)
			switch mode {
			case "request":
				if err := os.WriteFile(filepath.Join(dir, "request-000001.body"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "response":
				if err := os.WriteFile(filepath.Join(dir, "response-000001.body"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "ledger":
				ledger = append(ledger, []byte("{}\n")...)
			case "pending":
				ledger = ledger[:len(ledger)/2]
			case "extra":
				if err := os.WriteFile(filepath.Join(dir, "extra"), []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
			case "alias":
				target := filepath.Join(dir, "response-000001.body")
				if err := os.Rename(target, target+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target+"-saved", target); err != nil {
					t.Fatal(err)
				}
			}
			a, err := readWindowStoppedV6Native(dir, payload, ledger)
			if mode == "valid" {
				if err != nil || a.State.ModelKnown != 768 || a.State.ModelAttempts != 1 || a.State.ModelHeld != 0 || a.State.Halt != "invalid_request" || !a.State.Closed {
					t.Fatal(a.State, err)
				}
			} else if err == nil {
				t.Fatal("tamper accepted")
			}
		})
	}
}
func stoppedV6NamespaceFixture(t *testing.T) (windowActualManifest, windowStoppedV6Evidence, windowStoppedV5Baseline) {
	m, p := actualV6BindingFixture(t)
	root := filepath.Join(stoppedEvidenceRoot(t), "old-sixth")
	e := windowStoppedV6Evidence{Schema: "full-window-stopped-baseline.v6", PreviousBaseline: *m.BaselineEvidence, OriginalRoot: root, BudgetApproval: windowActualFile{Path: filepath.Join(stoppedEvidenceRoot(t), "approval.json"), SHA256: windowBudget130ApprovalSHA}}
	m.ArchiveCommit = "4ccec396f628a15050376eb1b34946e39d79a34a"
	m.ArchiveTree = "64b6a1b0caa8497e74dc9c56fd670fe1aa4d01ed"
	m.SourceRoot = filepath.Join(root, "regression/source")
	m.CampaignRoot = filepath.Join(root, "campaign")
	m.SessionRoot = filepath.Join(root, "records")
	m.ClaimsDir = p.historicalDir
	m.Observer.ExportTimeoutMS = 2000
	m.EvidenceKind = "actual"
	m.KnowledgeModel = "off"
	m.KnowledgeJEV = "off"
	m.Limits = windowV4ExpectedLimits()
	return m, e, p
}
func TestWindowStoppedV6NamespaceRejections(t *testing.T) {
	for _, mode := range []string{"valid", "prior-root", "nested-root", "prior-map", "current-approval", "old-approval", "source", "tree", "epoch", "paid", "caps", "timeout", "claimsdir", "descriptor"} {
		t.Run(mode, func(t *testing.T) {
			m, e, p := stoppedV6NamespaceFixture(t)
			switch mode {
			case "prior-root":
				e.OriginalRoot = p.originalRoot
			case "nested-root":
				e.OriginalRoot = filepath.Join(p.previous.originalRoot, "child")
			case "prior-map":
				delete(p.base.ClaimSHA256, "full1220-v5-20261029/window-000004")
			case "current-approval":
				e.BudgetApproval.SHA256 = windowBudget105ApprovalSHA
			case "old-approval":
				m.Scope.BudgetApprovalSHA256 = windowBudget105ApprovalSHA
			case "source":
				m.ArchiveCommit = "740db25818eccebe8363b282a109a14ca610a0be"
			case "tree":
				m.ArchiveTree = "changed"
			case "epoch":
				m.Schema = "full-window-actual-ready.v7"
			case "paid":
				m.Scope.ReadyForPaidRun = true
			case "caps":
				m.Scope.Windows[0].ModelCap--
			case "timeout":
				m.Observer.ExportTimeoutMS = 10000
			case "claimsdir":
				m.ClaimsDir = "/other/claims"
			case "descriptor":
				e.PreviousBaseline.SHA256 = "changed"
			}
			err := validateWindowStoppedV6Namespace(m, e, p)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid namespace accepted")
			}
		})
	}
}

// Exercise the stopped namespace with owned copies, never mutate old evidence.
func TestWindowStoppedV6ActualCopiedInventory(t *testing.T) {
	parallelWindowV7HeavyTest(t, true)
	path := os.Getenv("TASKBENCH_STOPPED_V6_EVIDENCE")
	if path == "" {
		t.Skip("explicit original readonly evidence not supplied")
	}
	raw, err := readWindowActualFile(path, "ccf528106789daf05a61c8d8f4e5f72d3a29eea65d78a6f81afb85f4e1ea45ad", 65536)
	if err != nil {
		t.Fatal(err)
	}
	var e windowStoppedV6Evidence
	if decodeWindowActualJSON(raw, &e) != nil {
		t.Fatal("descriptor")
	}
	files, err := readWindowStoppedV6Files(e.OriginalRoot)
	if err != nil {
		t.Fatal(err)
	}
	var m windowActualManifest
	if decodeWindowActualJSON(files["actual-manifest.json"], &m) != nil {
		t.Fatal("manifest")
	}
	root := stoppedEvidenceRoot(t)
	for _, name := range []string{"campaign", "records", "full-window-actual-v6"} {
		src := filepath.Join(e.OriginalRoot, name)
		if err := filepath.WalkDir(src, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(e.OriginalRoot, p)
			if err != nil {
				return err
			}
			dst := filepath.Join(root, rel)
			if d.IsDir() {
				return os.MkdirAll(dst, 0700)
			}
			if !d.Type().IsRegular() {
				t.Fatalf("unexpected original type: %s", rel)
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, body, 0600)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readWindowStoppedV6Bundle(root, m, expectedStoppedV5WindowBaseline(), files); err != nil {
		t.Fatal("valid owned copy", err)
	}
	for _, name := range []string{"campaign/claim-000002.json", "campaign/checkpoint-000001.json", "records/window-000002", "records/window-000001/extra.json", "campaign/ledger-000001/extra"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(root, name)
			if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readWindowStoppedV6Bundle(root, m, expectedStoppedV5WindowBaseline(), files); err == nil {
				t.Fatal("unrecorded descendant accepted")
			}
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, name := range []string{"campaign/claim-000001.json", "records/window-000001/receipt.json", "records/terminal.json", "actual-owner-exit.json"} {
		t.Run("tamper/"+name, func(t *testing.T) {
			changed := make(map[string][]byte, len(files))
			for key, body := range files {
				changed[key] = body
			}
			changed[name] = append(append([]byte{}, files[name]...), byte(' '))
			if _, err := readWindowStoppedV6Bundle(root, m, expectedStoppedV5WindowBaseline(), changed); err == nil {
				t.Fatal("rehashed foreign evidence accepted")
			}
		})
	}
}
