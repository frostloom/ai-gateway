package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowV4TestLimits() windowCampaignLimits {
	return windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxRequestBytes: 32768, MaxInput: 36864, MaxOutput: 128, InputRate: 2000000, OutputRate: 8000000, CacheHitInputRate: 40000}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0", MaxRequestBytes: 4096, MaxInput: 64000, MaxOutput: 1000000, InputRate: 42000}}
}
func TestWindowCampaignV4SelectsAccounting2Explicitly(t *testing.T) {
	scope := fullWindowScope{Schema: "full-window-scope.v4"}
	w := fullWindowDeclaration{ID: "window-001", MaxModelCalls: 100, MaxJEVCalls: 50, fullWindowCaps: fullWindowCaps{500000, 10000}}
	c := contractWindowCampaign(scope, "", w, windowV4TestLimits())
	if c.Schema != "full-window-accounting.v2" || c.Model.CacheHitInputRate != 40000 {
		t.Fatal("new epoch lost", c)
	}
	for _, v := range []string{"v1", "v2", "v3"} {
		scope.Schema = "full-window-scope." + v
		c = contractWindowCampaign(scope, "", w, windowV4TestLimits())
		if c.Schema != "full-window-accounting.v1" || c.Model.CacheHitInputRate != 40000 {
			t.Fatal("old epoch upgraded or cache field silently lost", c)
		}
	}
}

// The mirror is local_stub history data for the private closed test primitive;
// it is not a concrete file proof and cannot enter newWindowCampaignV4.
type fixtureStoppedV4LocalStub struct {
	fixtureStoppedHistory
	roots [3]string
}

func (h *fixtureStoppedV4LocalStub) windowStoppedPredecessorRootsV4() [3]string { return h.roots }
func fixtureV4LocalStubCampaign(t *testing.T) (windowCampaignPrepared, fullWindowBaseline, *fixtureStoppedV4LocalStub) {
	t.Helper()
	s, profiles, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV4(s, profiles, 28, windowV4TestCaps(t), b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(stoppedEvidenceRoot(t), "local_stub-campaign")
	sha, _, err := validateFullWindowScope(scope, s, profiles, b)
	if err != nil {
		t.Fatal(err)
	}
	limits := windowV4TestLimits()
	execution, err := executionSHAWindowCampaign(sha, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	p, err := prepareWindowCampaign(root, root, scope, s, profiles, b, sha, execution, limits)
	if err != nil {
		t.Fatal(err)
	}
	parent := stoppedEvidenceRoot(t)
	roots := [3]string{filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(parent, "third")}
	h := &fixtureStoppedV4LocalStub{fixtureStoppedHistory: fixtureStoppedHistory{base: expectedStoppedV3WindowBaseline(), sha: windowV4BaselineSHA, root: roots[2]}, roots: roots}
	p.stoppedHistory = h
	return p, expectedStoppedV3WindowBaseline(), h
}
func TestWindowCampaignV4LocalStubTwoClaims(t *testing.T) {
	p, b, h := fixtureV4LocalStubCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	var model, jev int64
	for i := 1; i <= 2; i++ {
		model += p.scope.Windows[i-1].ModelCap
		jev += p.scope.Windows[i-1].JEVCap
		raw, err := os.ReadFile(filepath.Join(p.root, fmt.Sprintf("claim-%06d.json", i)))
		if err != nil {
			t.Fatal(err)
		}
		var claim windowCampaignClaim
		if json.Unmarshal(raw, &claim) != nil {
			t.Fatal("claim")
		}
		if claim.Schema != "full-window-campaign-claim.v4" || claim.BaselineEvidenceSHA256 != windowV4BaselineSHA || claim.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || claim.ParentModel != 105000000 || claim.ParentJEV != 1000000 || len(claim.HistoricalClaims) != 25 || claim.ModelPermanent != 38888170+model || claim.JEVPermanent != 308003+jev {
			t.Fatal("v4 claim binding", claim)
		}
		if i == 1 {
			if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if h.calls < 3 {
		t.Fatal("history not fresh before claims", h.calls)
	}
}
func TestWindowCampaignV4LocalStubRejectsHistoryBeforeRoot(t *testing.T) {
	for _, mode := range []string{"old16", "missing25", "wrongsha", "invalid", "missing-interface", "first-overlap", "second-overlap", "third-overlap", "cross-first-second", "cross-first-third", "cross-second-third", "relative", "third-identity"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV4LocalStubCampaign(t)
			switch mode {
			case "old16":
				b = expectedStoppedV2WindowBaseline()
				h.base = b
			case "missing25":
				delete(h.base.ClaimSHA256, "full1220-v3-20261028/window-000009")
			case "wrongsha":
				h.sha = strings.Repeat("9", 64)
			case "invalid":
				h.invalid = true
			case "missing-interface":
				p.stoppedHistory = &h.fixtureStoppedHistory
			case "first-overlap":
				h.roots[0] = filepath.Dir(p.root)
			case "second-overlap":
				h.roots[1] = filepath.Dir(p.root)
			case "third-overlap":
				h.roots[2] = filepath.Dir(p.root)
				h.root = h.roots[2]
			case "cross-first-second":
				h.roots[0] = h.roots[1]
			case "cross-first-third":
				h.roots[0] = h.roots[2]
			case "cross-second-third":
				h.roots[1] = h.roots[2]
			case "relative":
				h.roots[0] = "relative"
			case "third-identity":
				h.root = h.roots[0]
			}
			if _, err := startWindowCampaign(p, b); err == nil {
				t.Fatal("invalid history accepted")
			}
			if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
				t.Fatal("created root before valid proof")
			}
		})
	}
}
func TestWindowCampaignV4LocalStubFreshBeforeNextClaim(t *testing.T) {
	for _, mode := range []string{"missing25", "invalid", "sha", "third-overlap"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV4LocalStubCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			checkpoint := liveFixtureWindowCampaign(t, c)
			beforeModel, beforeJEV := c.model, c.jev
			switch mode {
			case "missing25":
				delete(h.base.ClaimSHA256, "full1220-v3-20261028/window-000009")
			case "invalid":
				h.invalid = true
			case "sha":
				h.sha = strings.Repeat("8", 64)
			case "third-overlap":
				h.roots[2] = filepath.Dir(p.root)
				h.root = h.roots[2]
			}
			if len(c.baseline.ClaimSHA256) != 25 {
				t.Fatal("owned map not detached")
			}
			if _, err = c.advanceWindowCampaign(checkpoint); err == nil {
				t.Fatal("changed proof advanced")
			}
			if c.model != beforeModel || c.jev != beforeJEV || !c.poisoned {
				t.Fatal("rejected next claim changed totals")
			}
			if _, err = os.Lstat(filepath.Join(p.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("wrote next claim")
			}
		})
	}
}

func TestWindowCampaignV4RejectsMixedEpochAndRates(t *testing.T) {
	s, profiles, b := windowScopeInputs(t)
	caps := windowV4TestCaps(t)
	scope, err := prepareFullWindowScopeV4(s, profiles, 28, caps, b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(stoppedEvidenceRoot(t), "never-created")
	for _, mode := range []string{"zero-cache", "wrong-cache", "jev-cache", "input-rate", "output-rate", "bytes", "tokens", "legacy-v1", "legacy-v2", "legacy-v3"} {
		t.Run(mode, func(t *testing.T) {
			bad := scope
			limits := windowV4TestLimits()
			switch mode {
			case "zero-cache":
				limits.Model.CacheHitInputRate = 0
			case "wrong-cache":
				limits.Model.CacheHitInputRate = 200000
			case "jev-cache":
				limits.JEV.CacheHitInputRate = 1
			case "input-rate":
				limits.Model.InputRate++
			case "output-rate":
				limits.Model.OutputRate++
			case "bytes":
				limits.Model.MaxRequestBytes--
			case "tokens":
				limits.JEV.MaxInput--
			case "legacy-v1":
				bad, err = prepareFullWindowScope(s, profiles, 28, caps, b)
			case "legacy-v2":
				bad, err = prepareFullWindowScopeV2(s, profiles, 28, caps, b, windowV4BaselineSHA)
			case "legacy-v3":
				bad, err = prepareFullWindowScopeV3(s, profiles, 28, caps, b, windowV4BaselineSHA, windowBudget105ApprovalSHA)
			}
			if err != nil {
				t.Fatal(err)
			}
			sha, _, e := validateFullWindowScope(bad, s, profiles, b)
			if e != nil {
				t.Fatal(e)
			}
			execution, _ := executionSHAWindowCampaign(sha, root, limits)
			if _, e = prepareWindowCampaign(root, root, bad, s, profiles, b, sha, execution, limits); e == nil {
				t.Fatal("coherent epoch/rate mixing accepted")
			}
			if _, e = os.Lstat(root); !os.IsNotExist(e) {
				t.Fatal("rate rejection wrote root")
			}
		})
	}
}
func TestWindowCampaignV4ExecutionIdentityAndConcreteCtor(t *testing.T) {
	p, _, _ := fixtureV4LocalStubCampaign(t)
	zero := p.limits
	zero.Model.CacheHitInputRate = 0
	oldSHA, err := executionSHAWindowCampaign(p.scopeSHA, p.root, zero)
	if err != nil {
		t.Fatal(err)
	}
	if oldSHA == p.executionSHA {
		t.Fatal("new rate absent from execution identity")
	}
	raw, _ := json.Marshal(zero)
	if strings.Contains(string(raw), "cache_hit_input_micro_per_million") {
		t.Fatal("old JSON changed")
	}
	oldClaim, _ := json.Marshal(windowCampaignClaim{Schema: "full-window-campaign-claim.v3"})
	for _, field := range []string{"budget_approval_sha256", "parent_model_micro_cny", "parent_jev_micro_usd"} {
		if strings.Contains(string(oldClaim), field) {
			t.Fatal("legacy claim JSON changed")
		}
	}
	s, profiles, b := windowScopeInputs(t)
	// This deliberately invalid concrete object proves the production signature
	// and refuses before root creation. Never pass the actual file proof here.
	stopped := windowStoppedV3Baseline{base: expectedStoppedV2WindowBaseline(), evidenceSHA256: windowV4BaselineSHA}
	if _, err = newWindowCampaignV4(p.root, p.root, p.scope, s, profiles, b, p.scopeSHA, p.executionSHA, p.limits, stopped); err == nil {
		t.Fatal("old16 concrete state accepted")
	}
	if _, err = os.Lstat(p.root); !os.IsNotExist(err) {
		t.Fatal("invalid concrete created root")
	}
}
