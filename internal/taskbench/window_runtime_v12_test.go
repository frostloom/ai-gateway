package taskbench

import (
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// Missing V12 dispatch must reject this legitimate suffix, while prefix/local457 stay denied.
func TestWindowV12RuntimeMappedSuffix(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, e := prepareFullWindowScopeV12(s, p, windowV9TestCaps(t)[55:], b, windowV12BaselineSHA)
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
func TestWindowV12RuntimeCanonicalParentDenied(t *testing.T) {
	for _, p := range []string{"..", ".", "../x", "/x", "x\\y"} {
		if windowV12CanonicalSourcePath(p) {
			t.Fatal("noncanonical accepted", p)
		}
	}
}
func TestWindowV12RuntimeOldEpochInjection(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, e := prepareFullWindowScopeV10(s, p, windowV9TestCaps(t)[55:], b, windowV10BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	c := expectedWindowCompletionDeclarationV12()
	v.CompletionV12 = &c
	if validateWindowV10Declarations(v) == nil {
		t.Fatal("old epoch accepted V12")
	}
}

// Actual preparation must reject data-only candidates without opening settings.
func TestWindowV12RuntimeUnprivilegedDenied(t *testing.T) {
	if _, p, e := CheckFullWindowV12Preparation(FullWindowActualReadyOptions{}); e == nil || p != nil {
		t.Fatal("ordinary build prepared")
	}
	if _, e := AuthorizeFullWindowV12Preparation(nil, ""); e == nil {
		t.Fatal("nil token accepted")
	}
	if e := validateWindowActualStoppedBindingV12(windowActualManifest{}, windowStoppedV11Baseline{}); e == nil {
		t.Fatal("fake history accepted")
	}
	if e := validateWindowV12ExecutorEquivalence(windowActualManifest{Schema: "full-window-actual-ready.v12"}); e == nil {
		t.Fatal("self-declared review accepted")
	}
	if _, e := OpenFullWindowActual(nil, nil, ""); e == nil {
		t.Fatal("nil actual accepted")
	}
}

// A source map alone must not substitute for a separately compiled review anchor.
func TestWindowV12RuntimeActualRequiresObservation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	v, e := prepareFullWindowScopeV12(s, p, windowV9TestCaps(t)[55:], b, windowV12BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v12", Scope: v}
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("nil observation accepted for actual")
	}
	if _, tot, e := validateFullWindowScope(v, s, p, b); e != nil || tot.Pairs != 3275 || tot.Boundaries != 0 {
		t.Fatalf("full reconstruction %+v %v", tot, e)
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: v.ScheduleSHA256, PolicySHA256: v.Bindings.PolicySHA256, ScheduleSHA256: v.ScheduleSHA256, Windows: v.Windows, ModelTotal: 110191257, JEVTotal: 1818872}
	if e := validateV12ClaimDeclaration(d, v.CompletionV12); e != nil {
		t.Fatal(e)
	}
	d.Windows = append([]fullWindowDeclaration(nil), d.Windows...)
	d.Windows[0].FirstOrdinal = 1
	if validateV12ClaimDeclaration(d, v.CompletionV12) == nil {
		t.Fatal("prefix claim accepted")
	}
}

// Reusing a consumed nonce/settings identity must fail even at a fresh root.
func TestWindowV12RuntimeConsumedObservationDenied(t *testing.T) {
	o, dc := v11TestObserver(t)
	root := filepath.Dir(dc.Root)
	m := windowActualManifest{Schema: "full-window-actual-ready.v12", SourceRoot: filepath.Join(root, "source"), SessionRoot: filepath.Join(root, "records"), CampaignRoot: filepath.Join(root, "campaign"), ClaimsDir: filepath.Join(root, "history"), SpoolRoot: dc.Root, Observer: o.EvaluationIdentity()}
	bindV11TestManifest(&m)
	m.Scope.Schema = "full-window-scope.v12"
	if e := validateWindowV11SpoolBinding(m); e != nil {
		t.Fatal(e)
	}
	m.Observer.SpoolRunID = "c7d34cb23ca0507a18e9892b5f90ae3f0580a6772289b7276c4bf46db83a9ac4"
	m.Scope.Observation.SpoolRunID = m.Observer.SpoolRunID
	m.Scope.Observation.ObserverIdentitySHA256 = windowV8DeclarationSHA(m.Observer)
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("old run nonce accepted")
	}
	m.Observer = o.EvaluationIdentity()
	bindV11TestManifest(&m)
	m.Scope.Schema = "full-window-scope.v12"
	m.PrivateSettingsSHA256 = "8c4f09c6ba7a04fa1812fe1b45e623ff9be73a1905380dab6032ffa4684e5394"
	m.Scope.Observation.PrivateSettingsSHA256 = m.PrivateSettingsSHA256
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("old settings accepted")
	}
}

func TestWindowV12RuntimeBuildAnchorSet(t *testing.T) {
	old := fullWindowV12EquivalencePinsSHA
	fullWindowV12EquivalencePinsSHA = strings.Repeat("d", 64)
	t.Cleanup(func() { fullWindowV12EquivalencePinsSHA = old })
	m := windowActualManifest{Schema: "full-window-actual-ready.v12", ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40)}
	sha := strings.Repeat("c", 64)
	prefix := "github.com/frostloom/ai-gateway/internal/taskbench."
	flags := "-X " + prefix + "fullWindowV12ManifestSHA=" + sha + " -X " + prefix + "fullWindowV12ArchiveCommit=" + m.ArchiveCommit + " -X " + prefix + "fullWindowV12ArchiveTree=" + m.ArchiveTree + " -X " + prefix + "fullWindowV12EquivalencePinsSHA=" + fullWindowV12EquivalencePinsSHA
	info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/frostloom/ai-gateway"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}}}
	if e := validateWindowActualBuildPins(info, m, sha); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.ReplaceAll(flags, "V12", "V11"), flags + " -X " + prefix + "fullWindowV11ManifestSHA=" + sha, flags + " -X " + prefix + "fullWindowV12EquivalencePinsSHA=" + fullWindowV12EquivalencePinsSHA, strings.Replace(flags, fullWindowV12EquivalencePinsSHA, sha, 1)} {
		info.Settings[0].Value = bad
		if validateWindowActualBuildPins(info, m, sha) == nil {
			t.Fatal("foreign/missing/duplicate anchor accepted")
		}
	}
}
