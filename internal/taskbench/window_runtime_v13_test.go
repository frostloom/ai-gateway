package taskbench

import (
	"context"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// Missing V13 dispatch must reject this legitimate suffix, while prefix/local457 stay denied.
func TestWindowV13RuntimeMappedSuffix(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, e := prepareFullWindowScopeV13(s, p, windowV9TestCaps(t)[55:], b, windowV13BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		i     int
		id    string
		first int
	}{{1, "window-056", 386}, {456, "window-511", 3650}} {
		w, e := executionWindowAt(v, tc.i)
		if e != nil || w.ID != tc.id || w.FirstOrdinal != tc.first {
			t.Fatalf("mapping %d: %+v %v", tc.i, w, e)
		}
	}
	for _, i := range []int{0, 457} {
		if _, e := executionWindowAt(v, i); e == nil {
			t.Fatal("invalid local permitted", i)
		}
	}
	totals, e := executionTotals(v)
	if e != nil || totals != (fullWindowTotals{3275, 6550, 10504, 0, 110191257, 1818872}) {
		t.Fatalf("suffix totals %+v %v", totals, e)
	}
	if !windowScopeUsesNativeV8(v.Schema) || windowNativeParent(v) != (windowV8Amounts{195000000, 3000000}) {
		t.Fatal("native budget dispatch")
	}
}
func TestWindowV13RuntimeCanonicalParentDenied(t *testing.T) {
	for _, p := range []string{"..", ".", "../x", "/x", "x\\y"} {
		if windowV13CanonicalSourcePath(p) {
			t.Fatal("noncanonical accepted", p)
		}
	}
}
func TestWindowV13RuntimeOldEpochInjection(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, e := prepareFullWindowScopeV10(s, p, windowV9TestCaps(t)[55:], b, windowV10BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	c := expectedWindowCompletionDeclarationV13()
	v.CompletionV13 = &c
	if validateWindowV10Declarations(v) == nil {
		t.Fatal("old epoch accepted V13")
	}
}

// Actual preparation must reject data-only candidates without opening settings.
func TestWindowV13RuntimeUnprivilegedDenied(t *testing.T) {
	if _, p, e := CheckFullWindowV13Preparation(FullWindowActualReadyOptions{}); e == nil || p != nil {
		t.Fatal("ordinary build prepared")
	}
	if _, e := AuthorizeFullWindowV13Preparation(nil, ""); e == nil {
		t.Fatal("nil token accepted")
	}
	if e := validateWindowActualStoppedBindingV13(windowActualManifest{}, windowStoppedV12Baseline{}); e == nil {
		t.Fatal("fake history accepted")
	}
	if e := validateWindowV13ExecutorEquivalence(windowActualManifest{Schema: "full-window-actual-ready.v13"}); e == nil {
		t.Fatal("self-declared review accepted")
	}
	if _, e := OpenFullWindowActual(nil, nil, ""); e == nil {
		t.Fatal("nil actual accepted")
	}
}

// A source map alone must not substitute for a separately compiled review anchor.
func TestWindowV13RuntimeActualRequiresObservation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, e := prepareFullWindowScopeV13(s, p, windowV9TestCaps(t)[55:], b, windowV13BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v13", Scope: v}
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("nil observation accepted for actual")
	}
	if _, tot, e := validateFullWindowScope(v, s, p, b); e != nil || tot.Pairs != 3275 || tot.Boundaries != 0 {
		t.Fatalf("full reconstruction %+v %v", tot, e)
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: v.ScheduleSHA256, PolicySHA256: v.Bindings.PolicySHA256, ScheduleSHA256: v.ScheduleSHA256, Windows: v.Windows, ModelTotal: 110191257, JEVTotal: 1818872}
	if e := validateV13ClaimDeclaration(d, v.CompletionV13); e != nil {
		t.Fatal(e)
	}
	d.Windows = append([]fullWindowDeclaration(nil), d.Windows...)
	d.Windows[0].FirstOrdinal = 1
	if validateV13ClaimDeclaration(d, v.CompletionV13) == nil {
		t.Fatal("prefix claim accepted")
	}
}

// Reusing a consumed nonce/settings identity must fail even at a fresh root.
func TestWindowV13RuntimeConsumedObservationDenied(t *testing.T) {
	o, dc := v11TestObserver(t)
	root := filepath.Dir(dc.Root)
	m := windowActualManifest{Schema: "full-window-actual-ready.v13", SourceRoot: filepath.Join(root, "source"), SessionRoot: filepath.Join(root, "records"), CampaignRoot: filepath.Join(root, "campaign"), ClaimsDir: filepath.Join(root, "history"), SpoolRoot: dc.Root, Observer: o.EvaluationIdentity()}
	bindV11TestManifest(&m)
	m.Scope.Schema = "full-window-scope.v13"
	if e := validateWindowV11SpoolBinding(m); e != nil {
		t.Fatal(e)
	}
	m.Observer.SpoolRunID = "f419a799e9915b8f0249a8f09e8834aeb209d3336cd811ddfb2791445550b308"
	m.Scope.Observation.SpoolRunID = m.Observer.SpoolRunID
	m.Scope.Observation.ObserverIdentitySHA256 = windowV8DeclarationSHA(m.Observer)
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("old run nonce accepted")
	}
	m.Observer = o.EvaluationIdentity()
	bindV11TestManifest(&m)
	m.Scope.Schema = "full-window-scope.v13"
	m.PrivateSettingsSHA256 = "25cdb8728d5a32b3402685f01d79687eac4a29a7859749aab3c22eea329a7811"
	m.Scope.Observation.PrivateSettingsSHA256 = m.PrivateSettingsSHA256
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("old settings accepted")
	}
}

func TestWindowV13RuntimeBuildAnchorSet(t *testing.T) {
	old := fullWindowV13EquivalencePinsSHA
	fullWindowV13EquivalencePinsSHA = strings.Repeat("d", 64)
	t.Cleanup(func() { fullWindowV13EquivalencePinsSHA = old })
	m := windowActualManifest{Schema: "full-window-actual-ready.v13", ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40)}
	sha := strings.Repeat("c", 64)
	prefix := "github.com/frostloom/ai-gateway/internal/taskbench."
	flags := "-X " + prefix + "fullWindowV13ManifestSHA=" + sha + " -X " + prefix + "fullWindowV13ArchiveCommit=" + m.ArchiveCommit + " -X " + prefix + "fullWindowV13ArchiveTree=" + m.ArchiveTree + " -X " + prefix + "fullWindowV13EquivalencePinsSHA=" + fullWindowV13EquivalencePinsSHA
	info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/frostloom/ai-gateway"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}}}
	if e := validateWindowActualBuildPins(info, m, sha); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.ReplaceAll(flags, "V13", "V11"), flags + " -X " + prefix + "fullWindowV11ManifestSHA=" + sha, flags + " -X " + prefix + "fullWindowV13EquivalencePinsSHA=" + fullWindowV13EquivalencePinsSHA, strings.Replace(flags, fullWindowV13EquivalencePinsSHA, sha, 1)} {
		info.Settings[0].Value = bad
		if validateWindowActualBuildPins(info, m, sha) == nil {
			t.Fatal("foreign/missing/duplicate anchor accepted")
		}
	}
}

func TestWindowV13RejectsConsumedV12BeforeSettings(t *testing.T) {
	old := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v12", Scope: fullWindowScope{Schema: "full-window-scope.v12"}}, v8Authority: &windowV8Authority{v12: &windowV12Authority{}}}
	if session, err := OpenFullWindowActual(context.Background(), old, "/must-not-read-settings"); session != nil || err == nil || !strings.Contains(err.Error(), "v13 delegated authority required before settings") {
		t.Fatalf("old12 accepted: %v", err)
	}
}
