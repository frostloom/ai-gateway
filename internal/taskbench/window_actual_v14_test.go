package taskbench

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowV14ActualUnprivilegedDenied(t *testing.T) {
	if _, p, e := CheckFullWindowV14Preparation(FullWindowActualReadyOptions{}); e == nil || p != nil {
		t.Fatal("ordinary build prepared")
	}
	if _, e := AuthorizeFullWindowV14Preparation(nil, ""); e == nil {
		t.Fatal("nil preparation accepted")
	}
	if e := validateWindowActualStoppedBindingV14(windowActualManifest{}, windowStoppedV13Baseline{}); e == nil {
		t.Fatal("untyped177 accepted")
	}
	if e := validateWindowV14ExecutorEquivalence(windowActualManifest{Schema: "full-window-actual-ready.v14"}); e == nil {
		t.Fatal("unfrozen review accepted")
	}
	if e := (*windowV14Authority)(nil).revalidate(); e == nil {
		t.Fatal("nil authority accepted")
	}
	if e := (*FullWindowActualReady)(nil).revalidateV14(); e == nil {
		t.Fatal("nil ready accepted")
	}
	if e := (*FullWindowActualReady)(nil).revalidateStoppedV14(); e == nil {
		t.Fatal("nil stopped ready accepted")
	}
}

func TestWindowV14PolicyBindingRequiresDistinctFixedApproval(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	m := windowActualManifest{Schema: "full-window-actual-ready.v14", CampaignRoot: filepath.Join(root, "campaign")}
	op := expectedWindowV14OperationPolicy()
	m.Scope.OperationPolicyV14 = &op
	b := windowV14ExecutionBinding{OperationPolicySHA256: windowV8DeclarationSHA(op), OperationPolicyApproval: windowActualFile{Path: filepath.Join(root, "operation-policy-approval.json"), SHA256: strings.Repeat("a", 64)}}
	if e := validateWindowV14OperationPolicyBinding(b, m); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"missing", "old-budget", "old-autonomy", "policy", "foreign", "alias", "epoch", "campaign"} {
		t.Run(name, func(t *testing.T) {
			x, y := b, m
			switch name {
			case "missing":
				x.OperationPolicyApproval = windowActualFile{}
			case "old-budget":
				x.OperationPolicyApproval.SHA256 = windowStoppedV8ApprovalSHA
			case "old-autonomy":
				x.OperationPolicyApproval.SHA256 = windowV9AutonomousAuthorizationSHA
			case "policy":
				x.OperationPolicySHA256 = strings.Repeat("b", 64)
			case "foreign":
				x.OperationPolicyApproval.Path = filepath.Join(root, "foreign.json")
			case "alias":
				x.OperationPolicyApproval.Path = root + "/sub/../operation-policy-approval.json"
			case "epoch":
				y.Schema = "full-window-actual-ready.v13"
			case "campaign":
				y.CampaignRoot = root + "/alias"
			}
			if validateWindowV14OperationPolicyBinding(x, y) == nil {
				t.Fatal("unbound policy approval accepted")
			}
		})
	}
}

// Data-only negative vectors create no key, signature, grant or execution role.
func TestWindowV14OldBudgetAloneCannotAuthorizeOperationPolicy(t *testing.T) {
	for _, schema := range []string{"full-window-delegated-execution-authorization.v13", "full-window-delegated-execution-authorization.v14"} {
		a := windowV14ExecutionAuthorization{}
		a.Schema = schema
		a.HumanAuthorized = true
		a.RetryPolicyApproved = true
		a.SingleOwner = true
		a.NoRelaunch = true
		a.OwnerID = "data-only"
		a.Parent = expectedWindowV14BudgetDeclaration().Parent
		a.HumanReply.SHA256 = windowStoppedV8ApprovalSHA
		a.AutonomousAuthorization.SHA256 = windowV9AutonomousAuthorizationSHA
		raw, _ := json.Marshal(a)
		if _, e := verifyWindowV14ExecutionAuthorization(raw, windowActualManifest{}, strings.Repeat("a", 64), strings.Repeat("b", 64)); e == nil {
			t.Fatal("old budget alone became600s authority")
		}
	}
}

func TestWindowV14EquivalenceUsesDistinctSchemaAndExactChanges(t *testing.T) {
	before := map[string]string{"same.go": strings.Repeat("a", 64), "removed.go": strings.Repeat("b", 64)}
	after := map[string]string{"same.go": before["same.go"], "added.go": strings.Repeat("c", 64)}
	changes := map[string]windowExecutorChangeV10{"removed.go": {Before: before["removed.go"]}, "added.go": {After: after["added.go"]}}
	e := windowExecutorEquivalenceV14{Schema: "full-window-executor-equivalence.v14", OriginalSourcesSHA256: windowV8DeclarationSHA(before), SourcesSHA256: windowV8DeclarationSHA(after), OriginalSources: before, ChangedFiles: changes}
	pins := windowExecutorEquivalencePinsV14{e.OriginalSourcesSHA256, e.SourcesSHA256, windowV8DeclarationSHA(changes)}
	if err := validateWindowV14ExecutorEquivalencePinnedData(after, e, pins); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"epoch", "missing", "unreviewed", "pins", "path"} {
		t.Run(name, func(t *testing.T) {
			x, p := e, pins
			x.ChangedFiles = map[string]windowExecutorChangeV10{}
			for k, v := range changes {
				x.ChangedFiles[k] = v
			}
			switch name {
			case "epoch":
				x.Schema = "full-window-executor-equivalence.v13"
			case "missing":
				delete(x.ChangedFiles, "removed.go")
			case "unreviewed":
				x.ChangedFiles["extra.go"] = windowExecutorChangeV10{After: strings.Repeat("d", 64)}
			case "pins":
				p.ChangesSHA256 = ""
			case "path":
				x.OriginalSources = map[string]string{"../source.go": strings.Repeat("a", 64)}
				x.OriginalSourcesSHA256 = windowV8DeclarationSHA(x.OriginalSources)
				p.OriginalSourcesSHA256 = x.OriginalSourcesSHA256
			}
			if name == "missing" || name == "unreviewed" {
				p.ChangesSHA256 = windowV8DeclarationSHA(x.ChangedFiles)
			}
			if validateWindowV14ExecutorEquivalencePinnedData(after, x, p) == nil {
				t.Fatal("altered equivalence accepted")
			}
		})
	}
}

func TestWindowV14PreparationSelectsNewEpoch(t *testing.T) {
	saved := []string{fullWindowV14ManifestSHA, fullWindowV14ArchiveCommit, fullWindowV14ArchiveTree, fullWindowV13ManifestSHA, fullWindowV13ArchiveCommit, fullWindowV13ArchiveTree}
	t.Cleanup(func() {
		fullWindowV14ManifestSHA, fullWindowV14ArchiveCommit, fullWindowV14ArchiveTree = saved[0], saved[1], saved[2]
		fullWindowV13ManifestSHA, fullWindowV13ArchiveCommit, fullWindowV13ArchiveTree = saved[3], saved[4], saved[5]
	})
	fullWindowV14ManifestSHA, fullWindowV14ArchiveCommit, fullWindowV14ArchiveTree = strings.Repeat("a", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)
	fullWindowV13ManifestSHA, fullWindowV13ArchiveCommit, fullWindowV13ArchiveTree = "", "", ""
	manifest := filepath.Join(stoppedEvidenceRoot(t), "absent-manifest.json")
	stage := func(e error) {
		t.Helper()
		var s interface{ ReadinessStage() string }
		if !errors.As(e, &s) || s.ReadinessStage() != "manifest" {
			t.Fatal("new epoch did not reach manifest", e)
		}
	}
	_, p, e := CheckFullWindowV14Preparation(FullWindowActualReadyOptions{manifest})
	if p != nil {
		t.Fatal("missing manifest prepared")
	}
	stage(e)
	ready, e := AuthorizeFullWindowV14Preparation(&FullWindowV14Preparation{ready: &FullWindowActualReady{manifestPath: manifest}}, filepath.Join(stoppedEvidenceRoot(t), "must-not-read.json"))
	if ready != nil {
		t.Fatal("missing manifest authorized")
	}
	stage(e)
}

func TestWindowV14StoppedContextRejectsBeforeOriginalIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Even an apparently fixed original descriptor must not be opened after cancel.
	m := windowActualManifest{BaselineEvidence: &windowActualFile{windowStoppedV13InventoryPath, windowStoppedV13InventorySHA}}
	if _, e := loadWindowActualStoppedBaselineV14Context(ctx, m); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation lost before original IO", e)
	}
	if e := (&FullWindowActualReady{manifest: m}).revalidateStoppedV14Context(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal("ready cancellation lost before original IO", e)
	}
	if _, e := loadWindowActualStoppedBaselineV14Context(nil, m); e == nil {
		t.Fatal("nil history context accepted")
	}
	if e := (&FullWindowActualReady{manifest: m}).revalidateStoppedV14Context(nil); e == nil {
		t.Fatal("nil ready history context accepted")
	}
	if e := (*FullWindowActualReady)(nil).revalidateStoppedV14Context(context.Background()); e == nil {
		t.Fatal("nil snapshot accepted")
	}
	live, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	if _, e := loadWindowActualStoppedBaselineV14Context(live, m); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("deadline lost before original IO", e)
	}
}
