package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only the private test primitive accepts this fixture. The production v3
// factory requires the concrete, fresh-read sixteen-claim file proof.
type fixtureStoppedV3History struct {
	fixtureStoppedHistory
	firstRoot string
}

func (h *fixtureStoppedV3History) windowStoppedPredecessorRoots() [2]string {
	return [2]string{h.firstRoot, h.root}
}

func fixtureV3WindowCampaign(t *testing.T) (windowCampaignPrepared, fullWindowBaseline, *fixtureStoppedV3History) {
	t.Helper()
	s, p, b := windowScopeInputs(t)
	old, _ := fixtureWindowCampaign(t)
	caps := make([]fullWindowCaps, len(old.scope.Windows))
	for i := range caps {
		caps[i] = fullWindowCaps{100000, 10000}
	}
	sha := strings.Repeat("8", 64)
	scope, err := prepareFullWindowScopeV3(s, p, 100, caps, b, sha, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	scopeSHA, _, err := validateFullWindowScope(scope, s, p, b)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executionSHAWindowCampaign(scopeSHA, old.root, old.limits)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareWindowCampaign(old.root, old.root, scope, s, p, b, scopeSHA, execution, old.limits)
	if err != nil {
		t.Fatal(err)
	}
	base := expectedStoppedV2WindowBaseline()
	parent := stoppedEvidenceRoot(t)
	h := &fixtureStoppedV3History{fixtureStoppedHistory: fixtureStoppedHistory{base: expectedStoppedV2WindowBaseline(), sha: sha, root: filepath.Join(parent, "second")}, firstRoot: filepath.Join(parent, "first")}
	prepared.stoppedHistory = h
	return prepared, base, h
}

func TestWindowCampaignV3KeepsSixteenClaimsAcrossTwoFixtureWindows(t *testing.T) {
	p, b, h := fixtureV3WindowCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatalf("valid v3 fixture rejected: %v", err)
	}
	defer c.closeWindowCampaign()
	for i := 1; i <= 2; i++ {
		raw, err := os.ReadFile(filepath.Join(p.root, fmt.Sprintf("claim-%06d.json", i)))
		if err != nil {
			t.Fatal(err)
		}
		var claim windowCampaignClaim
		if json.Unmarshal(raw, &claim) != nil {
			t.Fatal("claim JSON")
		}
		if claim.Schema != "full-window-campaign-claim.v3" || claim.BaselineEvidenceSHA256 != h.sha || len(claim.HistoricalClaims) != 16 || claim.ModelPermanent != 33395934+int64(i)*100000 || claim.JEVPermanent != 259511+int64(i)*10000 {
			t.Fatal("new baseline lost", claim.Schema, claim.ModelPermanent)
		}
		if i == 1 {
			if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if h.calls < 3 {
		t.Fatal("history not reread before claims")
	}
}

func TestWindowCampaignV3RejectsChangedHistoryAndBothRootOverlapsBeforeClaim(t *testing.T) {
	for _, mode := range []string{"missing roots verifier", "wrong baseline", "wrong sha", "invalid proof", "first overlap", "second overlap", "crossed old roots", "relative first"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV3WindowCampaign(t)
			switch mode {
			case "missing roots verifier":
				p.stoppedHistory = &h.fixtureStoppedHistory
			case "wrong baseline":
				h.base.TotalModel = 100000000
			case "wrong sha":
				h.sha = strings.Repeat("9", 64)
			case "invalid proof":
				h.invalid = true
			case "first overlap":
				h.firstRoot = filepath.Dir(p.root)
			case "second overlap":
				h.root = filepath.Dir(p.root)
			case "crossed old roots":
				h.firstRoot = h.root
			case "relative first":
				h.firstRoot = "relative"
			}
			if _, err := startWindowCampaign(p, b); err == nil {
				t.Fatal("invalid sixteen-claim history accepted")
			}
			if _, err := os.Lstat(p.root); !os.IsNotExist(err) {
				t.Fatal("invalid history wrote campaign root")
			}
		})
	}
}

func TestWindowCampaignV3RechecksHistoryBeforeNextFixtureClaim(t *testing.T) {
	for _, mode := range []string{"removed sixteenth claim", "invalid proof", "changed sha", "first overlap", "second overlap"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV3WindowCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			checkpoint := liveFixtureWindowCampaign(t, c)
			beforeModel, beforeJEV := c.model, c.jev
			switch mode {
			case "removed sixteenth claim":
				delete(h.base.ClaimSHA256, "full1220-v2-20261028/window-000006")
			case "invalid proof":
				h.invalid = true
			case "changed sha":
				h.sha = strings.Repeat("9", 64)
			case "first overlap":
				h.firstRoot = filepath.Dir(p.root)
			case "second overlap":
				h.root = filepath.Dir(p.root)
			}
			if len(c.baseline.ClaimSHA256) != 16 {
				t.Fatal("caller mutated owned baseline map")
			}
			if _, err = c.advanceWindowCampaign(checkpoint); err == nil {
				t.Fatal("changed predecessor advanced")
			}
			if c.model != beforeModel || c.jev != beforeJEV {
				t.Fatal("rejected claim charged")
			}
			if _, err = os.Lstat(filepath.Join(p.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("changed history wrote claim")
			}
			if !c.poisoned {
				t.Fatal("failed owner remains usable")
			}
		})
	}
}

func TestWindowCampaignV3Uses105ParentAndRejectsOverflowBeforeWrite(t *testing.T) {
	// Place only the private fixture primitive's counter at the late-parent
	// boundary, avoiding thousands of unrelated business turns. No actual
	// manifest, build attestation, actual owner or paid provider is present.
	for _, tc := range []struct {
		name   string
		model  int64
		accept bool
	}{{"above_old_100_parent", 100000000, true}, {"exact_105", 104900000, true}, {"over_105", 104900001, false}} {
		t.Run(tc.name, func(t *testing.T) {
			p, b, _ := fixtureV3WindowCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			c.model = tc.model
			err = c.persistClaimWindowCampaign(1, "fixture-prior", "fixture-checkpoint")
			if (err == nil) != tc.accept {
				t.Fatal("wrong parent budget", err)
			}
			_, fileErr := os.Lstat(filepath.Join(p.root, "claim-000002.json"))
			if tc.accept && fileErr != nil {
				t.Fatal(fileErr)
			}
			if !tc.accept && (!os.IsNotExist(fileErr) || c.model != tc.model) {
				t.Fatal("overflow changed counter or wrote claim")
			}
		})
	}
}
