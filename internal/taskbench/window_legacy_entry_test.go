package taskbench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyCampaignCannotInterpretV2WithNineClaimBaseline(t *testing.T) {
	source, profiles, bindings := windowScopeInputs(t)
	p, _ := fixtureWindowCampaign(t)
	caps := make([]fullWindowCaps, len(p.scope.Windows))
	for i := range caps {
		caps[i] = p.scope.Windows[i].fullWindowCaps
	}
	scope, err := prepareFullWindowScopeV2(source, profiles, p.scope.Width, caps, bindings, strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	sha, _, err := validateFullWindowScope(scope, source, profiles, bindings)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executionSHAWindowCampaign(sha, p.root, p.limits)
	if err != nil {
		t.Fatal(err)
	}
	_, err = newWindowCampaign(p.root, p.root, "missing-history", scope, source, profiles, bindings, sha, execution, p.limits)
	if err == nil || !strings.Contains(err.Error(), "scope version") {
		t.Fatal("legacy campaign did not reject v2 before history", err)
	}
	if _, err = os.Lstat(p.root); !os.IsNotExist(err) {
		t.Fatal("legacy campaign created v2 root")
	}
}

func TestActualOpenRejectsLegacyBudgetBeforeSettingsOrRoots(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	ready := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v1", Scope: fullWindowScope{Schema: "full-window-scope.v1"}, SessionRoot: filepath.Join(root, "never-created"), CampaignRoot: filepath.Join(root, "never-claimed")}}
	_, err := OpenFullWindowActual(context.Background(), ready, filepath.Join(root, "unread-settings"))
	if err == nil || !strings.Contains(err.Error(), "v2 scope") {
		t.Fatal("legacy actual path did not fail for obsolete budget epoch", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("legacy actual path performed filesystem side effects", err)
	}
}
