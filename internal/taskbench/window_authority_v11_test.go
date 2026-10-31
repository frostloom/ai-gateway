package taskbench

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

func fixtureWindowV11Authority(t *testing.T) (*windowV11Authority, ed25519.PrivateKey, *windowV11ObservationOwner) {
	t.Helper()
	prior, key := fixtureWindowV9Authority(t)
	o, dc := v11TestObserver(t)
	root := filepath.Dir(dc.Root)
	scope, _, _, _ := windowV11FixtureScope(t)
	m := prior.manifest
	m.Schema = "full-window-actual-ready.v11"
	m.Scope = scope
	m.ScopeSHA256 = windowV8DeclarationSHA(scope)
	m.PrivateSettingsSHA256 = strings.Repeat("e", 64)
	m.SourceRoot = filepath.Join(root, "source")
	m.SessionRoot = filepath.Join(root, "records")
	m.CampaignRoot = filepath.Join(root, "campaign")
	m.ClaimsDir = filepath.Join(root, "history")
	m.SpoolRoot = dc.Root
	m.Observer = o.EvaluationIdentity()
	bindV11TestManifest(&m)
	m.ScopeSHA256 = windowV8DeclarationSHA(m.Scope)
	b := windowV11ExecutionBinding{windowV8ExecutionBinding: prior.binding.windowV8ExecutionBinding, AutonomousAuthorization: prior.binding.AutonomousAuthorization, GlobalBudget: prior.binding.GlobalBudget, PrivateSettingsSHA256: m.PrivateSettingsSHA256, SpoolRoot: m.SpoolRoot, Observer: m.Observer}
	b.Schema = "full-window-delegated-execution-authorization.v11"
	b.ScopeSHA256 = m.ScopeSHA256
	b.Parent = windowV8Amounts{195000000, 3000000}
	b.BudgetDeclarationSHA256 = scope.BudgetDeclarationSHA256
	b.RetryPolicySHA256 = scope.RetryPolicySHA256
	canonical, _ := json.Marshal(b)
	raw, _ := json.Marshal(windowV11ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, canonical))})
	path := filepath.Join(root, "synthetic-v11-authority.json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	a := &windowV11Authority{manifest: m, manifestSHA: prior.manifestSHA, binarySHA: prior.binarySHA, authorizationPath: path, authorizationSHA: conversationHash(raw), raw: raw, binding: b}
	owner, e := selectWindowV11ObservationOwner(context.Background(), m, o)
	if e != nil {
		t.Fatal(e)
	}
	return a, key, owner
}
func TestWindowV11AuthorityExactBindingAndGenuineObserver(t *testing.T) {
	a, _, owner := fixtureWindowV11Authority(t)
	if e := a.revalidate(); e != nil {
		t.Fatal(e)
	}
	p := windowCampaignPrepared{scope: a.manifest.Scope, scopeSHA: a.binding.ScopeSHA256, executionSHA: a.binding.ExecutionSHA256, v8Authority: a.nativeView(), observationOwner: owner}
	if e := validateWindowCampaignNativeAuthority(p); e != nil {
		t.Fatal(e)
	}
	p.observationOwner = nil
	if e := validateWindowCampaignNativeAuthority(p); e == nil {
		t.Fatal("naked signed JSON lacks live writer")
	}
	p.observationOwner = owner
	p.scope.Bindings.PolicySHA256 = strings.Repeat("f", 64)
	a.manifest.Scope = p.scope
	p.v8Authority = a.nativeView()
	if e := validateWindowCampaignNativeAuthority(p); e == nil {
		t.Fatal("coherently rewritten manifest bypassed scope digest")
	}
}
func TestWindowV11AuthorityRejectsSignedPolicyChanges(t *testing.T) {
	a, key, _ := fixtureWindowV11Authority(t)
	for name, change := range map[string]func(*windowV11ExecutionBinding){
		"epoch":       func(b *windowV11ExecutionBinding) { b.Schema = "full-window-delegated-execution-authorization.v10" },
		"parent":      func(b *windowV11ExecutionBinding) { b.Parent.Model++ },
		"global":      func(b *windowV11ExecutionBinding) { b.GlobalBudget.JEV++ },
		"human exact": func(b *windowV11ExecutionBinding) { b.HumanReviewedExactScope = true },
		"settings":    func(b *windowV11ExecutionBinding) { b.PrivateSettingsSHA256 = strings.Repeat("f", 64) },
		"spool":       func(b *windowV11ExecutionBinding) { b.SpoolRoot += "-foreign" },
		"run":         func(b *windowV11ExecutionBinding) { b.Observer.SpoolRunID = strings.Repeat("f", 64) },
		"cap":         func(b *windowV11ExecutionBinding) { b.Observer.SpoolMaxBytes++ },
		"sampling":    func(b *windowV11ExecutionBinding) { b.Observer.SampleRate = .5 },
		"direct":      func(b *windowV11ExecutionBinding) { b.Observer.ObservationReceiptBoundary = "" },
		"source":      func(b *windowV11ExecutionBinding) { b.SourceSHA256 = strings.Repeat("f", 64) },
		"scope":       func(b *windowV11ExecutionBinding) { b.ScopeSHA256 = strings.Repeat("f", 64) },
		"binary":      func(b *windowV11ExecutionBinding) { b.BinarySHA256 = strings.Repeat("f", 64) },
		"autonomy":    func(b *windowV11ExecutionBinding) { b.AutonomousAuthorization.SHA256 = strings.Repeat("f", 64) },
		"retry":       func(b *windowV11ExecutionBinding) { b.RetryPolicyApproved = false },
		"relaunch":    func(b *windowV11ExecutionBinding) { b.NoRelaunch = false },
	} {
		t.Run(name, func(t *testing.T) {
			b := a.binding
			change(&b)
			raw, _ := json.Marshal(b)
			raw, _ = json.Marshal(windowV11ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, raw))})
			if _, e := verifyWindowV11ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA); e == nil {
				t.Fatal("signed altered authority accepted")
			}
		})
	}
	if e := os.WriteFile(a.authorizationPath, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.nativeView().revalidate(); e == nil {
		t.Fatal("changed public authority accepted")
	}
}
func TestWindowV11BuildAndNativeDispatch(t *testing.T) {
	m := windowActualManifest{Schema: "full-window-actual-ready.v11", ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40)}
	sha := strings.Repeat("c", 64)
	prefix := "github.com/frostloom/ai-gateway/internal/taskbench."
	flags := "-X " + prefix + "fullWindowV11ManifestSHA=" + sha + " -X " + prefix + "fullWindowV11ArchiveCommit=" + m.ArchiveCommit + " -X " + prefix + "fullWindowV11ArchiveTree=" + m.ArchiveTree
	info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/frostloom/ai-gateway"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}}}
	if e := validateWindowActualBuildPins(info, m, sha); e != nil {
		t.Fatal(e)
	}
	info.Settings[0].Value = strings.ReplaceAll(flags, "V11", "V10")
	if e := validateWindowActualBuildPins(info, m, sha); e == nil {
		t.Fatal("old build accepted")
	}
	scope, _, _, _ := windowV11FixtureScope(t)
	if !windowScopeUsesNativeV8(scope.Schema) || windowObserverTimeoutForScope(scope.Schema) != 10000 || windowNativeParent(scope) != (windowV8Amounts{195000000, 3000000}) || !reflect.DeepEqual(windowActualGoCommand(m.Schema), []string{"go", "test", "./...", "-count=1", "-timeout=30m", "-json"}) {
		t.Fatal("native dispatch changed")
	}
	c := contractWindowCampaign(scope, windowV8DeclarationSHA(scope), scope.Windows[0], windowV4ExpectedLimits())
	if c.Schema != windowAccountingV3 || c.Model.MaxAttempts != scope.Windows[0].MaxModelCalls+20 || c.JEV.MaxAttempts != scope.Windows[0].MaxJEVCalls+20 {
		t.Fatal("native allowances changed")
	}
	p := windowCampaignPrepared{scope: scope, limits: windowV4ExpectedLimits()}
	if e := validateStoppedV11CampaignHistoryContext(context.Background(), p, windowV11CandidateBaseline(expectedStoppedV10WindowBaseline())); e == nil {
		t.Fatal("forged historical DATA entered live campaign")
	}
}
func TestWindowV11ScopeManifestObservationMismatch(t *testing.T) {
	a, _, _ := fixtureWindowV11Authority(t)
	m := a.manifest
	m.PrivateSettingsSHA256 = strings.Repeat("f", 64)
	if e := validateWindowV11SpoolBinding(m); e == nil {
		t.Fatal("manifest settings detached from frozen scope")
	}
}
