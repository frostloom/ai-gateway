package taskbench

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowV14ScopeDeadlineIsExplicit(t *testing.T) {
	for _, n := range []int{1, 8, 9, 10, 11, 12, 13} {
		s := fullWindowScope{Schema: "full-window-scope.v" + strconv.Itoa(n)}
		if d, e := windowOperationLimitForScope(s); e != nil || d != 300*time.Second {
			t.Fatalf("legacy %d: %v %v", n, d, e)
		}
	}
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	scope, e := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], b, windowV14BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	if d, e := windowOperationLimitForScope(scope); e != nil || d != 600*time.Second {
		t.Fatal(d, e)
	}
	for _, bad := range []fullWindowScope{{Schema: "full-window-scope.v14"}, {Schema: "full-window-scope.v15"}, {}} {
		if _, e := windowOperationLimitForScope(bad); e == nil {
			t.Fatal("unbound or unknown deadline accepted")
		}
	}
	changed := scope
	policy := *scope.OperationPolicyV14
	policy.WindowSeconds = 601
	changed.OperationPolicyV14 = &policy
	if _, e := windowOperationLimitForScope(changed); e == nil {
		t.Fatal("changed window deadline accepted")
	}
}

func TestWindowV14RuntimeMappedSuffix(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, e := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], b, windowV14BaselineSHA)
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
	if !windowScopeUsesNativeV8(v.Schema) || windowNativeParent(v) != (windowV8Amounts{200000000, 3000000}) {
		t.Fatal("native budget dispatch")
	}
}

func TestWindowV14RuntimeActualRequiresObservation(t *testing.T) {
	s, p, b := windowScopeInputs(t)
	b.PolicySHA256 = windowOwnedPreviewFailurePolicySHA()
	v, e := prepareFullWindowScopeV14(s, p, windowV9TestCaps(t)[55:], b, windowV14BaselineSHA)
	if e != nil {
		t.Fatal(e)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v14", Scope: v}
	if validateWindowV11SpoolBinding(m) == nil {
		t.Fatal("nil observation accepted for actual")
	}
	if _, tot, e := validateFullWindowScope(v, s, p, b); e != nil || tot.Pairs != 3275 || tot.Boundaries != 0 {
		t.Fatalf("full reconstruction %+v %v", tot, e)
	}
	d := fullWindowClaimDeclaration{ScopeSHA256: v.ScheduleSHA256, PolicySHA256: v.Bindings.PolicySHA256, ScheduleSHA256: v.ScheduleSHA256, Windows: v.Windows, ModelTotal: 110191257, JEVTotal: 1818872}
	if e := validateV14ClaimDeclaration(d, v.CompletionV14); e != nil {
		t.Fatal(e)
	}
	d.Windows = append([]fullWindowDeclaration(nil), d.Windows...)
	d.Windows[0].FirstOrdinal = 1
	if validateV14ClaimDeclaration(d, v.CompletionV14) == nil {
		t.Fatal("prefix claim accepted")
	}
}

func TestWindowV14RejectsConsumedV13BeforeSettings(t *testing.T) {
	old := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready.v13", Scope: fullWindowScope{Schema: "full-window-scope.v13"}}}
	if session, err := OpenFullWindowActual(context.Background(), old, "/must-not-read-settings"); session != nil || err == nil || !strings.Contains(err.Error(), "v14 separate policy") {
		t.Fatal(session, err)
	}
}
func TestWindowV14RejectsConsumedV13Observation(t *testing.T) {
	o, dc := v11TestObserver(t)
	defer o.Shutdown(context.Background())
	root := filepath.Dir(dc.Root)
	m := windowActualManifest{Schema: "full-window-actual-ready.v14", SourceRoot: filepath.Join(root, "source"), SessionRoot: filepath.Join(root, "records"), CampaignRoot: filepath.Join(root, "campaign"), ClaimsDir: filepath.Join(root, "history"), SpoolRoot: dc.Root, Observer: o.EvaluationIdentity()}
	bindV11TestManifest(&m)
	m.Scope.Schema = "full-window-scope.v14"
	if err := validateWindowV11SpoolBinding(m); err != nil {
		t.Fatal("fresh fixture rejected", err)
	}
	for _, which := range []string{"nonce", "settings"} {
		bad := m
		if which == "nonce" {
			bad.Observer.SpoolRunID = "dd3af173632b3d28051540c33e2437affb0c3c65ffab342bde35ca143c21c29b"
		} else {
			bad.PrivateSettingsSHA256 = "33e8b67d9d7b1aa1550972f1a2df33b16e105cd426c7b86151edea61ee6385ed"
		}
		d := *m.Scope.Observation
		d.SpoolRunID = bad.Observer.SpoolRunID
		d.PrivateSettingsSHA256 = bad.PrivateSettingsSHA256
		d.ObserverIdentitySHA256 = windowV8DeclarationSHA(bad.Observer)
		bad.Scope.Observation = &d
		if err := validateWindowV11SpoolBinding(bad); err == nil || !strings.Contains(err.Error(), "consumed") {
			t.Fatal("consumed V13 identity accepted", which, err)
		}
	}
}

func TestWindowV14VerificationRequiresLiveOperationContext(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	s := &FullWindowActualSession{}
	for _, ctx := range []context.Context{nil, canceled} {
		if err := s.verifyInputsContext(ctx); err == nil || ctx != nil && !errors.Is(err, context.Canceled) {
			t.Fatal("input check escaped operation context", err)
		}
		if err := s.verifyContext(ctx); err == nil || ctx != nil && !errors.Is(err, context.Canceled) {
			t.Fatal("session check escaped operation context", err)
		}
	}
}
