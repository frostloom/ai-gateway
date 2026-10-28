package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This in-memory history exercises the private campaign primitive. The actual
// constructor takes a concrete file proof and cannot accept this test type.
type fixtureStoppedHistory struct {
	base      fullWindowBaseline
	sha, root string
	invalid   bool
	calls     int
}

func (f *fixtureStoppedHistory) revalidateWindowStoppedHistory() (fullWindowBaseline, string, string, error) {
	f.calls++
	if f.invalid {
		return fullWindowBaseline{}, "", "", fmt.Errorf("fixture old proof changed")
	}
	return f.base, f.sha, f.root, nil
}

func fixtureV2WindowCampaign(t *testing.T) (windowCampaignPrepared, fullWindowBaseline, *fixtureStoppedHistory) {
	t.Helper()
	source, profiles, bindings := windowScopeInputs(t)
	old, _ := fixtureWindowCampaign(t)
	caps := make([]fullWindowCaps, len(old.scope.Windows))
	for i := range caps {
		caps[i] = fullWindowCaps{100000, 10000}
	}
	sha := strings.Repeat("8", 64)
	scope, err := prepareFullWindowScopeV2(source, profiles, 100, caps, bindings, sha)
	if err != nil {
		t.Fatal(err)
	}
	scopeSHA, _, err := validateFullWindowScope(scope, source, profiles, bindings)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executionSHAWindowCampaign(scopeSHA, old.root, old.limits)
	if err != nil {
		t.Fatal(err)
	}
	p, err := prepareWindowCampaign(old.root, old.root, scope, source, profiles, bindings, scopeSHA, execution, old.limits)
	if err != nil {
		t.Fatal(err)
	}
	b := expectedStoppedWindowBaseline()
	h := &fixtureStoppedHistory{base: expectedStoppedWindowBaseline(), sha: sha, root: filepath.Join(stoppedEvidenceRoot(t), "old-stopped")}
	p.stoppedHistory = h
	return p, b, h
}

func TestWindowCampaignV2KeepsTenClaimsAcrossTwoWindows(t *testing.T) {
	p, b, h := fixtureV2WindowCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	for index := 1; index <= 2; index++ {
		raw, err := os.ReadFile(filepath.Join(p.root, fmt.Sprintf("claim-%06d.json", index)))
		if err != nil {
			t.Fatal(err)
		}
		var claim windowCampaignClaim
		if err = json.Unmarshal(raw, &claim); err != nil {
			t.Fatal(err)
		}
		if claim.Schema != "full-window-campaign-claim.v2" || claim.BaselineEvidenceSHA256 != h.sha || len(claim.HistoricalClaims) != 10 || claim.HistoricalClaims[windowStoppedClaimID] != b.ClaimSHA256[windowStoppedClaimID] || claim.ModelPermanent != 29475706+int64(index)*100000 || claim.JEVPermanent != 227183+int64(index)*10000 {
			t.Fatal("new baseline lost", claim)
		}
		if index == 1 {
			if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if h.calls < 3 {
		t.Fatal("history not rechecked before claims", h.calls)
	}
}

func TestWindowCampaignV2RefusesChangedHistoryBeforeRoot(t *testing.T) {
	for _, mode := range []string{"missing verifier", "invalid proof", "wrong sha", "wrong baseline", "old root overlap"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV2WindowCampaign(t)
			switch mode {
			case "missing verifier":
				p.stoppedHistory = nil
			case "invalid proof":
				h.invalid = true
			case "wrong sha":
				h.sha = strings.Repeat("9", 64)
			case "wrong baseline":
				h.base.PermanentModel--
			case "old root overlap":
				h.root = filepath.Dir(p.root)
			}
			if _, err := startWindowCampaign(p, b); err == nil {
				t.Fatal("invalid v2 predecessor accepted")
			}
			if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
				t.Fatal("invalid predecessor created root")
			}
		})
	}
}

func TestWindowCampaignV2StopsBeforeNextClaimWhenHistoryChanges(t *testing.T) {
	for _, mode := range []string{"invalid proof", "changed sha", "removed tenth claim", "root overlap"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV2WindowCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			checkpoint := liveFixtureWindowCampaign(t, c)
			beforeModel, beforeJEV := c.model, c.jev
			switch mode {
			case "invalid proof":
				h.invalid = true
			case "changed sha":
				h.sha = strings.Repeat("9", 64)
			case "removed tenth claim":
				delete(h.base.ClaimSHA256, windowStoppedClaimID)
			case "root overlap":
				h.root = filepath.Dir(p.root)
			}
			if len(c.baseline.ClaimSHA256) != 10 {
				t.Fatal("caller mutated owned baseline map")
			}
			if _, err = c.advanceWindowCampaign(checkpoint); err == nil {
				t.Fatal("changed old proof advanced")
			}
			if c.model != beforeModel || c.jev != beforeJEV {
				t.Fatal("unwritten next claim charged")
			}
			if _, err = os.Lstat(filepath.Join(p.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("changed proof created next claim")
			}
			if !c.poisoned {
				t.Fatal("failed owner remained usable")
			}
		})
	}
}
