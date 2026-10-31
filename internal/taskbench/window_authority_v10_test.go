package taskbench

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureWindowV10Authority(t *testing.T) (*windowV10Authority, ed25519.PrivateKey) {
	t.Helper()
	prior, key := fixtureWindowV9Authority(t)
	scope, _, _, _ := windowV10FixtureScope(t)
	m := prior.manifest
	m.Schema, m.Scope = "full-window-actual-ready.v10", scope
	scopeRaw, _ := json.Marshal(scope)
	m.ScopeSHA256 = conversationHash(scopeRaw)
	diffPath := filepath.Join(filepath.Dir(prior.authorizationPath), "synthetic-diff.json")
	diffRaw := []byte(`{"evidence_kind":"synthetic_signature_binding_only"}`)
	if err := os.WriteFile(diffPath, diffRaw, 0600); err != nil {
		t.Fatal(err)
	}
	m.ExecutorEquivalence = &windowActualFile{Path: diffPath, SHA256: conversationHash(diffRaw)}
	b := windowV10ExecutionBinding{windowV8ExecutionBinding: prior.binding.windowV8ExecutionBinding, AutonomousAuthorization: prior.binding.AutonomousAuthorization, GlobalBudget: prior.binding.GlobalBudget, CompletionSHA256: windowV8DeclarationSHA(scope.Completion), ExecutorEquivalenceSHA256: m.ExecutorEquivalence.SHA256}
	b.Schema = "full-window-delegated-execution-authorization.v10"
	b.ScopeSHA256 = m.ScopeSHA256
	b.Parent = expectedWindowV10BudgetDeclaration().Parent
	b.BudgetDeclarationSHA256, b.RetryPolicySHA256 = scope.BudgetDeclarationSHA256, scope.RetryPolicySHA256
	canonical, _ := json.Marshal(b)
	raw, _ := json.Marshal(windowV10ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, canonical))})
	if err := os.WriteFile(prior.authorizationPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &windowV10Authority{manifest: m, manifestSHA: prior.manifestSHA, binarySHA: prior.binarySHA, authorizationPath: prior.authorizationPath, authorizationSHA: conversationHash(raw), raw: raw, binding: b}, key
}

func TestWindowV10AuthorityExactCompletionAndNativeView(t *testing.T) {
	a, key := fixtureWindowV10Authority(t)
	if out := os.Getenv("WINDOW_V10_EXPORT_SYNTHETIC"); out != "" {
		b := a.binding
		b.OwnerID = "synthetic é😀&<>\u2028\u2029 owner"
		canonical, _ := json.Marshal(b)
		raw, _ := json.Marshal(windowV10ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, canonical))})
		vector := map[string]any{"evidence_kind": "synthetic_canonical_signature_only_no_ready_or_execution_authority", "blank_build_anchors_deny_actual": true, "binding": b, "canonical_binding_utf8": string(canonical), "canonical_sha256": conversationHash(canonical), "public_key_hex": hex.EncodeToString(key.Public().(ed25519.PublicKey)), "signature_hex": hex.EncodeToString(ed25519.Sign(key, canonical)), "authorization": json.RawMessage(raw)}
		encoded, _ := json.MarshalIndent(vector, "", "  ")
		if err := os.WriteFile(out, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}

	if err := a.revalidate(); err != nil {
		t.Fatal(err)
	}
	view := a.nativeView()
	if err := view.revalidate(); err != nil {
		t.Fatal(err)
	}
	p := windowCampaignPrepared{scope: a.manifest.Scope, scopeSHA: a.binding.ScopeSHA256, executionSHA: a.binding.ExecutionSHA256, v8Authority: view}
	if err := validateWindowCampaignNativeAuthority(p); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*windowV10ExecutionBinding){
		"prefix_refs":    func(b *windowV10ExecutionBinding) { b.CompletionSHA256 = strings.Repeat("f", 64) },
		"equivalence":    func(b *windowV10ExecutionBinding) { b.ExecutorEquivalenceSHA256 = strings.Repeat("f", 64) },
		"foreign_scope":  func(b *windowV10ExecutionBinding) { b.ScopeSHA256 = strings.Repeat("f", 64) },
		"foreign_source": func(b *windowV10ExecutionBinding) { b.SourceSHA256 = strings.Repeat("f", 64) },
		"old_epoch":      func(b *windowV10ExecutionBinding) { b.Schema = "full-window-delegated-execution-authorization.v9" },
		"human_exact":    func(b *windowV10ExecutionBinding) { b.HumanReviewedExactScope = true },
		"global":         func(b *windowV10ExecutionBinding) { b.GlobalBudget.Model++ },
		"parent":         func(b *windowV10ExecutionBinding) { b.Parent.JEV-- },
		"retry":          func(b *windowV10ExecutionBinding) { b.RetryPolicySHA256 = strings.Repeat("f", 64) },
		"autonomy":       func(b *windowV10ExecutionBinding) { b.AutonomousAuthorization.SHA256 = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			b := a.binding
			mutate(&b)
			canonical, _ := json.Marshal(b)
			raw, _ := json.Marshal(windowV10ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, canonical))})
			if _, err := verifyWindowV10ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA); err == nil {
				t.Fatal("changed V10 binding admitted")
			}
		})
	}
	view.binding.OwnerID = "foreign"
	if view.revalidate() == nil {
		t.Fatal("changed native view admitted")
	}
	p.v8Authority = a.nativeView()
	p.scope.Bindings.PolicySHA256 = strings.Repeat("f", 64)
	if validateWindowCampaignNativeAuthority(p) == nil {
		t.Fatal("foreign prepared policy admitted")
	}
}
func TestWindowV10AuthorityNoJSONCapabilityOrConsumedEntry(t *testing.T) {
	if _, err := AuthorizeFullWindowV10Preparation(nil, "/do-not-read"); err == nil {
		t.Fatal("nil preparation admitted")
	}
	for _, epoch := range []string{"v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9", "v10"} {
		r := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready." + epoch, Scope: fullWindowScope{Schema: "full-window-scope." + epoch}}}
		if _, err := OpenFullWindowActual(context.Background(), r, "/do-not-read"); err == nil {
			t.Fatal("unowned epoch admitted")
		}
	}
}
func TestWindowV10TypedHistoryCannotBeSubstituted(t *testing.T) {
	scope, _, _, _ := windowV10FixtureScope(t)
	p := windowCampaignPrepared{scope: scope, limits: windowV4ExpectedLimits()}
	if err := validateStoppedV10CampaignHistoryContext(context.Background(), p, windowV10CandidateBaseline(expectedStoppedV9WindowBaseline())); err == nil {
		t.Fatal("JSON baseline admitted")
	}
	p.stoppedV9 = &windowStoppedV9Baseline{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateStoppedV10CampaignHistoryContext(ctx, p, windowV10CandidateBaseline(expectedStoppedV9WindowBaseline())); err == nil {
		t.Fatal("canceled history admitted")
	}
	if got := expectedStoppedV9WindowBaseline(); got.TotalModel != 200000000 || got.TotalJEV != 4000000 || got.PermanentModel != 66472061 || got.PermanentJEV != 631114 {
		t.Fatal("historical totals rewritten")
	}
}

func TestWindowV10AuthorityChangedEquivalenceRejected(t *testing.T) {
	a, _ := fixtureWindowV10Authority(t)
	if err := a.revalidate(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.manifest.ExecutorEquivalence.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.revalidate() == nil {
		t.Fatal("changed executor equivalence accepted by native authority")
	}
}

func TestWindowV10PreparedScopeCannotBeRewrittenWithManifest(t *testing.T) {
	a, _ := fixtureWindowV10Authority(t)
	p := windowCampaignPrepared{scope: a.manifest.Scope, scopeSHA: a.binding.ScopeSHA256, executionSHA: a.binding.ExecutionSHA256, v8Authority: a.nativeView()}
	if err := validateWindowCampaignNativeAuthority(p); err != nil {
		t.Fatal(err)
	}
	p.scope.Bindings.PolicySHA256 = strings.Repeat("f", 64)
	a.manifest.Scope = p.scope
	p.v8Authority = a.nativeView()
	if validateWindowCampaignNativeAuthority(p) == nil {
		t.Fatal("rewritten prepared scope/manifest bypassed signed scope digest")
	}
}
