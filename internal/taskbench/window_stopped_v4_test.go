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

func TestWindowStoppedV4PinsAnd28Baseline(t *testing.T) {
	pins := windowStoppedV4ArtifactPins()
	if len(pins) != 17 {
		t.Fatalf("pins%d want17", len(pins))
	}
	b := expectedStoppedV4WindowBaseline()
	if len(b.ClaimSHA256) != 28 || b.PermanentModel != 40625891 || b.PermanentJEV != 323825 || b.RemainingModel != 64374109 || b.RemainingJEV != 676175 || b.ActualModelCalls != 1985 || b.ActualJEVCalls != 501 || b.TotalModel != 105000000 || b.TotalJEV != 1000000 {
		t.Fatal("wrong28baseline", b)
	}
	for id, sha := range expectedStoppedV3WindowBaseline().ClaimSHA256 {
		if b.ClaimSHA256[id] != sha {
			t.Fatal("changed old25", id)
		}
	}
	for i := 1; i <= 3; i++ {
		if b.ClaimSHA256[fmt.Sprintf("full1220-v4-20261029/window-%06d", i)] != pins[fmt.Sprintf("campaign/claim-%06d.json", i)] {
			t.Fatal("claim omitted", i)
		}
	}
}
func stoppedV4NamespaceFixture() (windowActualManifest, windowStoppedV4Evidence, windowStoppedV3Baseline) {
	_, _, p2 := stoppedV3NamespaceFixture()
	p := windowStoppedV3Baseline{base: expectedStoppedV3WindowBaseline(), previous: p2, originalRoot: "/old/third", historicalDir: p2.historicalDir, evidencePath: "/old/baseline-v3.json", evidenceSHA256: windowStoppedV4PreviousSHA}
	e := windowStoppedV4Evidence{Schema: "full-window-stopped-baseline.v4", PreviousBaseline: windowActualFile{Path: p.evidencePath, SHA256: p.evidenceSHA256}, OriginalRoot: "/old/fourth", BudgetApproval: windowActualFile{Path: "/old/approval.json", SHA256: windowBudget105ApprovalSHA}}
	m := windowActualManifest{Schema: "full-window-actual-ready.v4", EvidenceKind: "actual", ArchiveCommit: "933057fef2aeddd8065e49016b29648eeb3a42e0", KnowledgeModel: "off", KnowledgeJEV: "off", CampaignRoot: "/old/fourth/campaign", SessionRoot: "/old/fourth/records", ClaimsDir: p.historicalDir, BaselineEvidence: &e.PreviousBaseline, HistoricalClaims: cloneWindowHistoricalClaims(p.base.ClaimSHA256), Scope: fullWindowScope{Schema: "full-window-scope.v4", BudgetApprovalSHA256: windowBudget105ApprovalSHA, BaselineEvidenceSHA256: p.evidenceSHA256}, Limits: windowV4ExpectedLimits()}
	return m, e, p
}
func TestWindowStoppedV4FourRootsAndNoOldInterfacePromotion(t *testing.T) {
	m, e, p := stoppedV4NamespaceFixture()
	if err := validateWindowStoppedV4Namespace(m, e, p); err != nil {
		t.Fatal("validfourrootnamespace", err)
	}
	proof := windowStoppedV4Baseline{previous: p, originalRoot: e.OriginalRoot}
	if proof.windowStoppedPredecessorRootsV5() != [4]string{"/old/first", "/old/second", "/old/third", "/old/fourth"} {
		t.Fatal("roots")
	}
	if _, ok := any(proof).(windowStoppedV4History); ok {
		t.Fatal("new28mustnotpromoteold25interface")
	}
	if _, ok := any(proof).(windowStoppedV3History); ok {
		t.Fatal("new28mustnotpromoteold16interface")
	}
}
func stoppedV4NativeFixture(t *testing.T) (string, windowPayloadManifest, []byte) {
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
	if err = l.Close(); err != nil {
		t.Fatal(err)
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
func TestWindowStoppedV4WeightedNativeBytes(t *testing.T) {
	dir, m, raw := stoppedV4NativeFixture(t)
	before := append([]byte{}, raw...)
	a, err := readWindowStoppedV4Native(dir, m, raw)
	if err != nil || a.State.ModelAttempts != 1 || a.State.ModelKnown != 768 || a.State.ModelHeld != 0 || a.State.Pending != nil {
		t.Fatal("validweightednative", a.State, err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatal("changedinput")
	}
	if a.Contract.Model.cost(1757, 33) != 3778 || a.Contract.Model.cost(36864, 128) != 74752 {
		t.Fatal("oldcost/holdchanged")
	}
}

func stoppedV4QualityFixture(t *testing.T) (string, fullWindowSlot, windowAcquisitionRow, collectionRecording) {
	t.Helper()
	in := qualityPreviewExample(t)
	in.Record.Result.Case.Scenario.Turns = in.Record.Result.Case.Scenario.Turns[:1]
	qualityRebind(t, &in)
	candidate, err := qualityPreviewCandidate(in)
	if err != nil {
		t.Fatal(err)
	}
	dir := stoppedEvidenceRoot(t)
	slot := fullWindowSlot{Ordinal: 1, Pass: 1, CaseKey: in.Record.Result.Case.Key(), UserTurns: 1}
	q := windowQualityEndEvidence{PolicySHA256: windowOwnedQualityPolicySHA(), SourceSHA256: candidate.SourceSHA256, RuleSHA256: candidate.RuleSHA256, CandidateFile: "quality-000001-jev_selective-candidate.json", CleanupFile: "quality-000001-jev_selective-cleanup.json", ObservedTurns: 1, UnrunTurns: 0}
	raw, _ := json.Marshal(candidate)
	q.CandidateSHA256 = conversationHash(raw)
	if err = os.WriteFile(filepath.Join(dir, q.CandidateFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	cleanup := struct {
		Post    qualityEffectCapture `json:"post_cleanup"`
		Deleted int64                `json:"deleted"`
		Absent  bool                 `json:"session_absent"`
	}{candidate.Evidence.After, 1, true}
	raw, _ = json.Marshal(cleanup)
	q.CleanupSHA256 = conversationHash(raw)
	if err = os.WriteFile(filepath.Join(dir, q.CleanupFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	owned := &windowQualityOwnedEnd{Evidence: q, Candidate: candidate, PostCleanup: cleanup.Post, Deleted: 1, SessionAbsent: true}
	q.EnvelopeSHA256 = windowQualityEnvelopeSHA(owned)
	row := windowAcquisitionRow{Ordinal: 1, Pass: 1, CaseKey: slot.CaseKey, Arm: "jev_selective", DefinedTurns: 1, ObservedTurns: 1, Status: "failed", Disposition: "halt", QualityEnd: &q, ExcludeCompleteTaskComparison: true}
	return dir, slot, row, in.Record
}
func TestWindowStoppedV4QualityEndWithZeroUnrunIsRetained(t *testing.T) {
	dir, slot, row, record := stoppedV4QualityFixture(t)
	q, err := readWindowStoppedV4Quality(dir, windowOwnedQualityPolicySHA(), slot, row, record)
	if err != nil || q == nil || q.Evidence.UnrunTurns != 0 || q.Evidence.ObservedTurns != 1 || q.Deleted != 1 || !q.SessionAbsent || q.Candidate.Evidence.Record.Result.Success {
		t.Fatal("legalzero suffixquality rejected", q, err)
	}
}

func TestWindowStoppedV4NamespaceRejectsCrossedRootsEpochRatesAndOld25(t *testing.T) {
	for _, mode := range []string{"epoch", "budget", "cache", "jev-cache", "root-first", "root-second", "root-third", "crossed-prior", "relative", "old16", "claimsdir"} {
		t.Run(mode, func(t *testing.T) {
			m, e, p := stoppedV4NamespaceFixture()
			switch mode {
			case "epoch":
				m.Schema = "full-window-actual-ready.v3"
			case "budget":
				p.base.TotalModel = 100000000
			case "cache":
				m.Limits.Model.CacheHitInputRate = 0
			case "jev-cache":
				m.Limits.JEV.CacheHitInputRate = 1
			case "root-first":
				e.OriginalRoot = "/old/first"
			case "root-second":
				e.OriginalRoot = "/old/second/child"
			case "root-third":
				e.OriginalRoot = "/old/third"
			case "crossed-prior":
				p.originalRoot = p.previous.originalRoot
			case "relative":
				p.originalRoot = "relative"
			case "old16":
				p.base = expectedStoppedV2WindowBaseline()
			case "claimsdir":
				m.ClaimsDir = "/other/claims"
			}
			if validateWindowStoppedV4Namespace(m, e, p) == nil {
				t.Fatal("invalid namespace accepted")
			}
		})
	}
	for id := range expectedStoppedV3WindowBaseline().ClaimSHA256 {
		for _, mode := range []string{"missing", "changed"} {
			t.Run(mode+"/"+id, func(t *testing.T) {
				m, e, p := stoppedV4NamespaceFixture()
				if mode == "missing" {
					delete(p.base.ClaimSHA256, id)
				} else {
					p.base.ClaimSHA256[id] = strings.Repeat("f", 64)
				}
				if validateWindowStoppedV4Namespace(m, e, p) == nil {
					t.Fatal("badprior25accepted")
				}
			})
		}
	}
}
func TestWindowStoppedV4Cached28CannotBecomeOld25OrLoseThirdClaim(t *testing.T) {
	for id := range expectedStoppedV4WindowBaseline().ClaimSHA256 {
		for _, mode := range []string{"missing", "changed"} {
			t.Run(mode+"/"+id, func(t *testing.T) {
				b := expectedStoppedV4WindowBaseline()
				if mode == "missing" {
					delete(b.ClaimSHA256, id)
				} else {
					b.ClaimSHA256[id] = strings.Repeat("f", 64)
				}
				if _, _, _, err := (windowStoppedV4Baseline{base: b}).revalidateWindowStoppedHistory(); err == nil {
					t.Fatal("changedcachedclaimaccepted")
				}
			})
		}
	}
	for _, mode := range []string{"old25", "model", "jev", "modelcalls", "jevcalls", "100parent", "remaining"} {
		t.Run(mode, func(t *testing.T) {
			b := expectedStoppedV4WindowBaseline()
			switch mode {
			case "old25":
				b = expectedStoppedV3WindowBaseline()
			case "model":
				b.PermanentModel--
			case "jev":
				b.PermanentJEV--
			case "modelcalls":
				b.ActualModelCalls--
			case "jevcalls":
				b.ActualJEVCalls--
			case "100parent":
				b.TotalModel = 100000000
			case "remaining":
				b.RemainingJEV++
			}
			if _, _, _, err := (windowStoppedV4Baseline{base: b}).revalidateWindowStoppedHistory(); err == nil {
				t.Fatal("cachedbaselineaccepted")
			}
		})
	}
}
func TestWindowStoppedV4DescriptorCannotAssertAProofOrCreateRoot(t *testing.T) {
	for _, mode := range []string{"schema", "previous", "approval", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			e := windowStoppedV4Evidence{Schema: "full-window-stopped-baseline.v4", PreviousBaseline: windowActualFile{Path: filepath.Join(root, "missing25"), SHA256: windowStoppedV4PreviousSHA}, OriginalRoot: filepath.Join(root, "absent-owner"), BudgetApproval: windowActualFile{Path: filepath.Join(root, "missingapproval"), SHA256: windowBudget105ApprovalSHA}}
			switch mode {
			case "schema":
				e.Schema = "full-window-stopped-baseline.v3"
			case "previous":
				e.PreviousBaseline.SHA256 = strings.Repeat("a", 64)
			case "approval":
				e.BudgetApproval.SHA256 = strings.Repeat("a", 64)
			}
			raw, _ := json.Marshal(e)
			if mode == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"verified":true}`)...)
			}
			path := filepath.Join(root, "descriptor.json")
			os.WriteFile(path, raw, 0600)
			if _, err := loadWindowStoppedV4Baseline(path, conversationHash(raw)); err == nil {
				t.Fatal("assertedproofaccepted")
			}
			if _, err := os.Lstat(e.OriginalRoot); !os.IsNotExist(err) {
				t.Fatal("readcreatedowner")
			}
		})
	}
}
func TestWindowStoppedV4QualitySidecarMissingOrChangedRejected(t *testing.T) {
	for _, field := range []string{"candidate", "cleanup"} {
		for _, mode := range []string{"missing", "changed"} {
			t.Run(field+"/"+mode, func(t *testing.T) {
				dir, slot, row, record := stoppedV4QualityFixture(t)
				name := row.QualityEnd.CandidateFile
				if field == "cleanup" {
					name = row.QualityEnd.CleanupFile
				}
				if mode == "missing" {
					os.Remove(filepath.Join(dir, name))
				} else {
					os.WriteFile(filepath.Join(dir, name), []byte("changed"), 0600)
				}
				if _, err := readWindowStoppedV4Quality(dir, windowOwnedQualityPolicySHA(), slot, row, record); err == nil {
					t.Fatal("badqualitysidecaraccepted")
				}
			})
		}
	}
}
func TestWindowStoppedV4NativeSchemaAndUsageMustMatchRawBytes(t *testing.T) {
	oldDir, oldManifest, oldRaw := stoppedV3NativeFixture(t)
	if _, err := readWindowStoppedV4Native(oldDir, oldManifest, oldRaw); err == nil {
		t.Fatal("schema1acceptedforP4")
	}
	for _, name := range []string{"request-000001.body", "response-000001.body", "request-000001.json", "response-000001.json", "store.json"} {
		for _, mode := range []string{"missing", "changed"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				dir, m, raw := stoppedV4NativeFixture(t)
				if mode == "missing" {
					os.Remove(filepath.Join(dir, name))
				} else {
					os.WriteFile(filepath.Join(dir, name), []byte("changed"), 0600)
				}
				if _, err := readWindowStoppedV4Native(dir, m, raw); err == nil {
					t.Fatal("badnativebytesaccepted")
				}
			})
		}
	}
	dir, m, raw := stoppedV4NativeFixture(t)
	var events []windowAccountingEvent
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var event windowAccountingEvent
		json.Unmarshal(line, &event)
		events = append(events, event)
	}
	*events[2].Usage.CacheHit = 0
	*events[2].Usage.CacheMiss = *events[2].Usage.Input
	var changed bytes.Buffer
	previous := ""
	for i := range events {
		events[i].PreviousSHA256 = previous
		line, _ := json.Marshal(events[i])
		changed.Write(line)
		changed.WriteByte('\n')
		previous = conversationHash(line)
	}
	if _, err := readWindowStoppedV4Native(dir, m, changed.Bytes()); err == nil {
		t.Fatal("coherentledgerrepricedrawnative")
	}
}
