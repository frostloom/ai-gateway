package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowCampaignV5ExplicitAccounting2WithOriginalTariff(t *testing.T) {
	scope := fullWindowScope{Schema: "full-window-scope.v5"}
	w := fullWindowDeclaration{ID: "window-001", MaxModelCalls: 100, MaxJEVCalls: 100, fullWindowCaps: fullWindowCaps{500000, 10000}}
	contract := contractWindowCampaign(scope, "", w, windowV4ExpectedLimits())
	if contract.Schema != "full-window-accounting.v2" || contract.Model.CacheHitInputRate != 40000 || contract.Model.cost(36864, 128) != 74752 || contract.JEV.cost(64000, 1000000) != 2688 {
		t.Fatal("v5accountinglost", contract)
	}
}

// This is explicitly local_stub data for the private owned test primitive,
// never the concrete original28 file proof accepted by newWindowCampaignV5.
type fixtureStoppedV5LocalStub struct {
	fixtureStoppedHistory
	roots [4]string
}

func (h *fixtureStoppedV5LocalStub) windowStoppedPredecessorRootsV5() [4]string { return h.roots }
func fixtureV5LocalStubCampaign(t *testing.T) (windowCampaignPrepared, fullWindowBaseline, *fixtureStoppedV5LocalStub) {
	t.Helper()
	s, profiles, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV5(s, profiles, 28, windowV5TestCaps(t), b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(stoppedEvidenceRoot(t), "local_stub-campaign-v5")
	sha, _, err := validateFullWindowScope(scope, s, profiles, b)
	if err != nil {
		t.Fatal(err)
	}
	limits := windowV4ExpectedLimits()
	execution, err := executionSHAWindowCampaign(sha, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	p, err := prepareWindowCampaign(root, root, scope, s, profiles, b, sha, execution, limits)
	if err != nil {
		t.Fatal(err)
	}
	parent := stoppedEvidenceRoot(t)
	roots := [4]string{filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(parent, "third"), filepath.Join(parent, "fourth")}
	h := &fixtureStoppedV5LocalStub{fixtureStoppedHistory: fixtureStoppedHistory{base: expectedStoppedV4WindowBaseline(), sha: windowV5BaselineSHA, root: roots[3]}, roots: roots}
	p.stoppedHistory = h
	return p, expectedStoppedV4WindowBaseline(), h
}
func TestWindowCampaignV5LocalStubTwoOwnedWindowsRetain28History(t *testing.T) {
	p, b, h := fixtureV5LocalStubCampaign(t)
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
		if claim.Schema != "full-window-campaign-claim.v5" || claim.BaselineEvidenceSHA256 != windowV5BaselineSHA || claim.BudgetApprovalSHA256 != windowBudget105ApprovalSHA || claim.ParentModel != 105000000 || claim.ParentJEV != 1000000 || !reflect.DeepEqual(claim.HistoricalClaims, expectedStoppedV4WindowBaseline().ClaimSHA256) || claim.ModelPermanent != 40625891+model || claim.JEVPermanent != 323825+jev {
			t.Fatal("v5claimbinding", claim)
		}
		if i == 1 {
			if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if h.calls < 3 {
		t.Fatal("historynotfresh", h.calls)
	}
}
func TestWindowCampaignV5LocalStubRejectsHistoryBeforeRoot(t *testing.T) {
	for _, mode := range []string{"old25", "missing28", "wrongsha", "invalid", "missing-interface", "relative", "fourth-identity"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV5LocalStubCampaign(t)
			switch mode {
			case "old25":
				b = expectedStoppedV3WindowBaseline()
				h.base = b
			case "missing28":
				delete(h.base.ClaimSHA256, "full1220-v4-20261029/window-000003")
			case "wrongsha":
				h.sha = windowV4BaselineSHA
			case "invalid":
				h.invalid = true
			case "missing-interface":
				p.stoppedHistory = &h.fixtureStoppedHistory
			case "relative":
				h.roots[0] = "relative"
			case "fourth-identity":
				h.root = h.roots[0]
			}
			if _, err := startWindowCampaign(p, b); err == nil {
				t.Fatal("bad28proofaccepted")
			}
			if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
				t.Fatal("badproofcreatedroot")
			}
		})
	}
	for rootIndex := 0; rootIndex < 4; rootIndex++ {
		t.Run(fmt.Sprintf("overlap%d", rootIndex), func(t *testing.T) {
			p, b, h := fixtureV5LocalStubCampaign(t)
			h.roots[rootIndex] = filepath.Dir(p.root)
			if rootIndex == 3 {
				h.root = h.roots[3]
			}
			if _, err := startWindowCampaign(p, b); err == nil {
				t.Fatal("oldrootoverlapaccepted")
			}
			if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
				t.Fatal("overlapcreatedroot")
			}
		})
	}
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			t.Run(fmt.Sprintf("cross%d%d", i, j), func(t *testing.T) {
				p, b, h := fixtureV5LocalStubCampaign(t)
				h.roots[i] = h.roots[j]
				if _, err := startWindowCampaign(p, b); err == nil {
					t.Fatal("crossedoldrootsaccepted")
				}
				if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
					t.Fatal("crossedcreatedroot")
				}
			})
		}
	}
}
func TestWindowCampaignV5LocalStubFreshBeforeNextClaim(t *testing.T) {
	for _, mode := range []string{"missing28", "invalid", "changedsha", "root-overlap"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV5LocalStubCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			checkpoint := liveFixtureWindowCampaign(t, c)
			model, jev := c.model, c.jev
			switch mode {
			case "missing28":
				delete(h.base.ClaimSHA256, "full1220-v4-20261029/window-000003")
			case "invalid":
				h.invalid = true
			case "changedsha":
				h.sha = windowV4BaselineSHA
			case "root-overlap":
				h.roots[3] = filepath.Dir(p.root)
				h.root = h.roots[3]
			}
			if len(c.baseline.ClaimSHA256) != 28 {
				t.Fatal("ownedmapnotdetached")
			}
			if _, err = c.advanceWindowCampaign(checkpoint); err == nil || !c.poisoned || c.model != model || c.jev != jev {
				t.Fatal("changedhistorystillclaimed", err)
			}
			if _, err = os.Lstat(filepath.Join(p.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("badnextclaimed")
			}
		})
	}
}
func TestWindowCampaignV5OriginalTariffRejectsMixedConfiguration(t *testing.T) {
	s, profiles, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV5(s, profiles, 28, windowV5TestCaps(t), b, windowV5BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(stoppedEvidenceRoot(t), "absent")
	for _, mode := range []string{"zero-cache", "wrong-cache", "miss-rate", "output-rate", "jev-cache", "jev-rate", "bytes", "input", "output", "old-schema4", "old-schema3"} {
		t.Run(mode, func(t *testing.T) {
			limits := windowV4ExpectedLimits()
			bad := scope
			switch mode {
			case "zero-cache":
				limits.Model.CacheHitInputRate = 0
			case "wrong-cache":
				limits.Model.CacheHitInputRate = 200000
			case "miss-rate":
				limits.Model.InputRate++
			case "output-rate":
				limits.Model.OutputRate++
			case "jev-cache":
				limits.JEV.CacheHitInputRate = 1
			case "jev-rate":
				limits.JEV.InputRate++
			case "bytes":
				limits.Model.MaxRequestBytes--
			case "input":
				limits.JEV.MaxInput--
			case "output":
				limits.Model.MaxOutput = 512
			case "old-schema4":
				bad.Schema = "full-window-scope.v4"
			case "old-schema3":
				bad.Schema = "full-window-scope.v3"
			}
			raw, _ := json.Marshal(bad)
			sha := conversationHash(raw)
			execution, _ := executionSHAWindowCampaign(sha, root, limits)
			if _, err := prepareWindowCampaign(root, root, bad, s, profiles, b, sha, execution, limits); err == nil {
				t.Fatal("mixedtariff/epochaccepted")
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatal("badconfigurationcreatedroot")
			}
		})
	}
}
func TestWindowCampaignV5NamespaceAndConcreteCtorFailBeforeRoot(t *testing.T) {
	p, _, _ := fixtureV5LocalStubCampaign(t)
	s, profiles, b := windowScopeInputs(t)
	if _, err := prepareWindowCampaign(p.root, p.root+"-different", p.scope, s, profiles, b, p.scopeSHA, p.executionSHA, p.limits); err == nil {
		t.Fatal("wrongfrozenrootaccepted")
	}
	alias := filepath.Join(filepath.Dir(p.root), "alias")
	if err := os.Symlink(filepath.Dir(p.root), alias); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(alias)
	root := filepath.Join(alias, "new")
	if _, err := prepareWindowCampaign(root, root, p.scope, s, profiles, b, p.scopeSHA, p.executionSHA, p.limits); err == nil {
		t.Fatal("aliasaccepted")
	}
	old25 := windowStoppedV4Baseline{base: expectedStoppedV3WindowBaseline(), evidenceSHA256: windowV5BaselineSHA}
	if _, err := newWindowCampaignV5(p.root, p.root, p.scope, s, profiles, b, p.scopeSHA, p.executionSHA, p.limits, old25); err == nil {
		t.Fatal("old25forgedasconcrete28")
	}
	if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
		t.Fatal("invalidconcretecreatedroot")
	}
}

func TestWindowCampaignV5ParentBoundsUse105AndSeparateCurrencies(t *testing.T) {
	for _, mode := range []string{"above-old100", "exact-model105", "over-model105", "exact-jev1", "over-jev1"} {
		t.Run(mode, func(t *testing.T) {
			p, b, _ := fixtureV5LocalStubCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			next := p.scope.Windows[1]
			accept := true
			// Only the private fixture counter is moved to a late-parent boundary. This
			// is not real campaign progress, a fresh financial claim or scope permission.
			switch mode {
			case "above-old100":
				c.model = 100000000
			case "exact-model105":
				c.model = 105000000 - next.ModelCap
			case "over-model105":
				c.model = 105000001 - next.ModelCap
				accept = false
			case "exact-jev1":
				c.jev = 1000000 - next.JEVCap
			case "over-jev1":
				c.jev = 1000001 - next.JEVCap
				accept = false
			}
			model, jev := c.model, c.jev
			err = c.persistClaimWindowCampaign(1, "local_stub-prior", "local_stub-checkpoint")
			if (err == nil) != accept {
				t.Fatal("wrongparentboundary", err)
			}
			_, readErr := os.Lstat(filepath.Join(p.root, "claim-000002.json"))
			if accept {
				if readErr != nil {
					t.Fatal(readErr)
				}
			} else if !os.IsNotExist(readErr) || c.model != model || c.jev != jev {
				t.Fatal("overparentwrote/charged")
			}
		})
	}
}
func TestWindowCampaignV5OwnedRootOEXCLAndUncertainClaimReserve(t *testing.T) {
	p, b, _ := fixtureV5LocalStubCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	first, err := os.ReadFile(filepath.Join(p.root, "claim-000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = startWindowCampaign(p, b); err == nil {
		t.Fatal("existingrootreopened")
	}
	after, err := os.ReadFile(filepath.Join(p.root, "claim-000001.json"))
	if err != nil || !bytes.Equal(first, after) {
		t.Fatal("existingclaimchanged")
	}
	checkpoint := liveFixtureWindowCampaign(t, c)
	model, jev := c.model, c.jev
	c.openFile = func(path string) (windowJournalFile, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		if filepath.Base(path) == "claim-000002.json" {
			return &faultWindowCampaignJournal{File: f, syncFailure: true}, nil
		}
		return f, nil
	}
	if _, err = c.advanceWindowCampaign(checkpoint); err == nil || !c.poisoned {
		t.Fatal("uncertainclaimadvanced")
	}
	if c.model != model+p.scope.Windows[1].ModelCap || c.jev != jev+p.scope.Windows[1].JEVCap {
		t.Fatal("failedclaimreservationreleased")
	}
	if _, err = c.advanceWindowCampaign(checkpoint); err == nil {
		t.Fatal("failedownerreused")
	}
	if _, err = os.Lstat(filepath.Join(p.root, "claim-000003.json")); !os.IsNotExist(err) {
		t.Fatal("uncertainclaimcontinued")
	}
}
