package taskbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type fixtureStoppedV7LocalStub struct {
	fixtureStoppedHistory
	roots [6]string
}

func (h *fixtureStoppedV7LocalStub) windowStoppedPredecessorRootsV7() [6]string { return h.roots }
func fixtureV7LocalStubCampaign(t *testing.T) (windowCampaignPrepared, fullWindowBaseline, *fixtureStoppedV7LocalStub) {
	t.Helper()
	s, profiles, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV7(s, profiles, 28, windowV7TestCaps(), b, windowV7BaselineSHA, windowBudget130ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	parent := stoppedEvidenceRoot(t)
	root := filepath.Join(parent, "local_stub-v7")
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
	roots := [6]string{filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(parent, "third"), filepath.Join(parent, "fourth"), filepath.Join(parent, "fifth"), filepath.Join(parent, "sixth")}
	base := expectedStoppedV6WindowBaseline()
	h := &fixtureStoppedV7LocalStub{fixtureStoppedHistory: fixtureStoppedHistory{base: base, sha: windowV7BaselineSHA, root: roots[5]}, roots: roots}
	p.stoppedHistory = h
	return p, base, h
}
func TestWindowCampaignV7LocalStubClaims(t *testing.T) {
	p, b, h := fixtureV7LocalStubCampaign(t)
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
		if json.Unmarshal(raw, &claim) != nil || claim.Schema != "full-window-campaign-claim.v7" || claim.ParentModel != 130000000 || claim.ParentJEV != 1000000 || claim.ModelPermanent != 43465965+model || claim.JEVPermanent != 349080+jev || claim.BaselineEvidenceSHA256 != windowV7BaselineSHA || claim.BudgetApprovalSHA256 != windowBudget130ApprovalSHA || !reflect.DeepEqual(claim.HistoricalClaims, b.ClaimSHA256) {
			t.Fatal("claim", claim)
		}
		if i == 1 {
			if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if h.calls < 3 {
		t.Fatal("history not fresh")
	}
}

func TestWindowCampaignV7RejectsHistoryAndRootMutations(t *testing.T) {
	for _, mode := range []string{"historical_parent", "current_parent", "missing_claim", "evidence", "overlap", "crossed", "limits"} {
		t.Run(mode, func(t *testing.T) {
			p, b, h := fixtureV7LocalStubCampaign(t)
			switch mode {
			case "historical_parent":
				b = expectedStoppedV5WindowBaseline()
				h.base = b
			case "current_parent":
				b.TotalModel++
				h.base = b
			case "missing_claim":
				delete(b.ClaimSHA256, "full1220-v5-20261029/window-000004")
				h.base = b
			case "evidence":
				h.sha = windowV5BaselineSHA
			case "overlap":
				h.roots[0] = p.root
			case "crossed":
				h.roots[1] = h.roots[0]
			case "limits":
				p.limits.Model.CacheHitInputRate++
			}
			if _, err := startWindowCampaign(p, b); err == nil {
				t.Fatal("accepted mutation")
			}
			if _, err := os.Stat(p.root); !os.IsNotExist(err) {
				t.Fatal("root created before validation", err)
			}
		})
	}
}
func TestWindowCampaignV7RevalidatesBeforeEveryClaim(t *testing.T) {
	p, b, h := fixtureV7LocalStubCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	h.sha = windowV5BaselineSHA
	if _, err = c.advanceWindowCampaign(liveFixtureWindowCampaign(t, c)); err == nil {
		t.Fatal("changed history accepted")
	}
	if _, err = os.Stat(filepath.Join(p.root, "claim-000002.json")); !os.IsNotExist(err) {
		t.Fatal("claim created", err)
	}
}
func TestWindowV7ClaimBoundsPreserveLegacy(t *testing.T) {
	p, _, _ := fixtureV7LocalStubCampaign(t)
	d := fullWindowClaimDeclaration{ScopeSHA256: p.scopeSHA, ScheduleSHA256: p.scope.ScheduleSHA256, PolicySHA256: p.scope.Bindings.PolicySHA256, Windows: p.scope.Windows, ModelTotal: 84500000, JEVTotal: 600000}
	if d.validate() == nil {
		t.Fatal("legacy accepted v7 budget")
	}
	if err := d.validateWithBounds(84510611, 650855); err != nil {
		t.Fatal(err)
	}
	d.ModelTotal = 1<<63 - 1
	if d.validateWithBounds(84510611, 650855) == nil {
		t.Fatal("overflow accepted")
	}
}
