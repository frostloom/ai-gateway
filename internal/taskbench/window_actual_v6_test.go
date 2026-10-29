package taskbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestActualV6EpochAndConsumedOpenBoundary(t *testing.T) {
	versions := []string{"v1", "v2", "v3", "v4", "v5", "v6"}
	for _, a := range versions {
		for _, b := range versions {
			m := windowActualManifest{Schema: "full-window-actual-ready." + a, Scope: fullWindowScope{Schema: "full-window-scope." + b}}
			if windowActualSupportedEpoch(m) != (a == b && a != "v1") {
				t.Errorf("readonly epoch %s/%s", a, b)
			}
			s, e := OpenFullWindowActual(context.Background(), &FullWindowActualReady{manifest: m}, "/never-read-settings")
			if s != nil || e == nil || !strings.Contains(e.Error(), "v7 scope required") {
				t.Errorf("open %s/%s: %v", a, b, e)
			}
		}
	}
}

func actualV6BindingFixture(t *testing.T) (windowActualManifest, windowStoppedV5Baseline) {
	t.Helper()
	m, prev := actualV5BindingFixture(t)
	p := windowStoppedV5Baseline{base: expectedStoppedV5WindowBaseline(), previous: prev, evidencePath: filepath.Join(stoppedEvidenceRoot(t), "missing32.json"), evidenceSHA256: windowV6BaselineSHA, originalRoot: filepath.Join(stoppedEvidenceRoot(t), "old-fifth"), historicalDir: prev.historicalDir}
	source, profiles, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV6(source, profiles, 28, windowV6TestCaps(), b, windowV6BaselineSHA, windowBudget130ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	m.Schema = "full-window-actual-ready.v6"
	m.Scope = scope
	m.BaselineEvidence = &windowActualFile{p.evidencePath, p.evidenceSHA256}
	m.HistoricalClaims = cloneWindowHistoricalClaims(p.base.ClaimSHA256)
	return m, p
}
func TestActualV6ConcreteBinding(t *testing.T) {
	m, p := actualV6BindingFixture(t)
	if err := validateWindowActualStoppedBindingV6(m, p); err != nil {
		t.Fatal(err)
	}
	for i, root := range p.windowStoppedPredecessorRootsV6() {
		for _, session := range []bool{true, false} {
			n := m
			if session {
				n.SessionRoot = root
			} else {
				n.CampaignRoot = root
			}
			if validateWindowActualStoppedBindingV6(n, p) == nil {
				t.Fatalf("root %d accepted", i)
			}
		}
	}
	n := m
	n.HistoricalClaims = cloneWindowHistoricalClaims(m.HistoricalClaims)
	for k := range n.HistoricalClaims {
		delete(n.HistoricalClaims, k)
		break
	}
	if validateWindowActualStoppedBindingV6(n, p) == nil {
		t.Fatal("missing claim")
	}
	n = m
	n.Scope.BudgetApprovalSHA256 = windowBudget105ApprovalSHA
	if validateWindowActualStoppedBindingV6(n, p) == nil {
		t.Fatal("old approval")
	}
	r := &FullWindowActualReady{manifest: m, stoppedV5: p, baseline: expectedCurrent130WindowBaseline()}
	if err := r.revalidateStoppedV6(); err == nil {
		t.Fatal("memory proof accepted")
	}
}
func TestActualV6ApprovalBasis(t *testing.T) {
	source := "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-budget130-20261029/budget-approval.private.json"
	raw, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		t.Skip("private approval fixture unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	dir := stoppedEvidenceRoot(t)
	path := filepath.Join(dir, "approval.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	approval := windowActualFile{path, windowBudget130ApprovalSHA}
	writeEnvelope := func(basis []windowActualFile) windowActualManifest {
		t.Helper()
		body, err := json.Marshal(windowActualProviderEvidence{Basis: basis})
		if err != nil {
			t.Fatal(err)
		}
		env := filepath.Join(dir, "envelope.json")
		if err = os.WriteFile(env, body, 0600); err != nil {
			t.Fatal(err)
		}
		return windowActualManifest{Envelope: windowActualFile{env, conversationHash(body)}}
	}
	if got, err := windowActualV6Approval(writeEnvelope([]windowActualFile{approval})); err != nil || got != approval {
		t.Fatal("approval", err)
	}
	for _, basis := range [][]windowActualFile{{}, {approval, approval}} {
		if _, err := windowActualV6Approval(writeEnvelope(basis)); err == nil {
			t.Fatal("missing or duplicate accepted")
		}
	}
	copyPath := filepath.Join(dir, "copy.json")
	if err = os.WriteFile(copyPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := windowActualV6Approval(writeEnvelope([]windowActualFile{approval, {copyPath, windowBudget130ApprovalSHA}})); err == nil {
		t.Fatal("duplicate digest accepted")
	}
	alias := filepath.Join(dir, "alias.json")
	if err = os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := windowActualV6Approval(writeEnvelope([]windowActualFile{{alias, windowBudget130ApprovalSHA}})); err == nil {
		t.Fatal("alias accepted")
	}
	if err = os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := windowActualV6Approval(writeEnvelope([]windowActualFile{approval})); err == nil {
		t.Fatal("tamper accepted")
	}
}

func TestActualV6ReadonlyOriginalProofAndCurrentApproval(t *testing.T) {
	const evidence = "/Users/a1/项目/ai-gateway/.tmp/stopped-v5-proof-work/stopped-v5.private.json"
	const approvalPath = "/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-budget130-20261029/budget-approval.private.json"
	if _, err := os.Stat(evidence); os.IsNotExist(err) {
		t.Skip("private stopped proof unavailable")
	}
	if _, err := os.Stat(approvalPath); os.IsNotExist(err) {
		t.Skip("private approval unavailable")
	}
	p, err := loadWindowStoppedV5Baseline(evidence, windowV6BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := actualV6BindingFixture(t)
	m.BaselineEvidence = &windowActualFile{evidence, windowV6BaselineSHA}
	m.ClaimsDir = p.historicalDir
	m.HistoricalClaims = cloneWindowHistoricalClaims(p.base.ClaimSHA256)
	dir := stoppedEvidenceRoot(t)
	body, err := json.Marshal(windowActualProviderEvidence{Basis: []windowActualFile{{approvalPath, windowBudget130ApprovalSHA}}})
	if err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, "envelope.json")
	if err = os.WriteFile(env, body, 0600); err != nil {
		t.Fatal(err)
	}
	m.Envelope = windowActualFile{env, conversationHash(body)}
	fresh, a, err := loadWindowActualStoppedBaselineV6(m)
	if err != nil || !reflect.DeepEqual(fresh, p) {
		t.Fatal("fresh concrete proof", err)
	}
	history := windowCurrent130History{stopped: fresh, approval: a}
	base, _, _, err := history.revalidateWindowStoppedHistory()
	if err != nil || !reflect.DeepEqual(base, expectedCurrent130WindowBaseline()) || !reflect.DeepEqual(p.base, expectedStoppedV5WindowBaseline()) {
		t.Fatal("budget projection", err)
	}
	r := &FullWindowActualReady{manifest: m, stoppedV5: fresh, approval130: a, baseline: base}
	if err = r.revalidateStoppedV6(); err != nil {
		t.Fatal(err)
	}
}
