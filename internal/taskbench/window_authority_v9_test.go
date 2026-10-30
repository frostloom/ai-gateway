package taskbench

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The retained instruction text is provenance data. This fixture signs it with
// a fresh synthetic key and fake source identities; it issues no ready token.
func fixtureWindowV9Authority(t *testing.T) (*windowV9Authority, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := stoppedEvidenceRoot(t)
	refs := make([]windowActualFile, 0, 2)
	for _, name := range []string{"window_v9_budget_approval.json", "window_v9_autonomous_authorization.json"} {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, windowActualFile{path, conversationHash(raw)})
	}
	s, p, b := windowScopeInputs(t)
	scope, err := prepareFullWindowScopeV9(s, p, windowV9TestCaps(t), b, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	m := windowActualManifest{Schema: "full-window-actual-ready.v9", Scope: scope, ScopeSHA256: digest, ExecutionSHA256: digest, BuildAttestationPublicKey: hex.EncodeToString(pub), Sources: map[string]string{"synthetic.go": digest}}
	sources, _ := json.Marshal(m.Sources)
	binding := windowV9ExecutionBinding{windowV8ExecutionBinding: windowV8ExecutionBinding{Schema: "full-window-delegated-execution-authorization.v9", HumanAuthorized: true, RetryPolicyApproved: true, HumanReply: refs[0], ManifestSHA256: digest, SourceSHA256: conversationHash(sources), BinarySHA256: digest, ScopeSHA256: digest, ExecutionSHA256: digest, BudgetDeclarationSHA256: scope.BudgetDeclarationSHA256, RetryPolicySHA256: scope.RetryPolicySHA256, Parent: windowV8Amounts{180000000, 2500000}, OwnerID: "synthetic-test-owner", SingleOwner: true, NoRelaunch: true}, AutonomousAuthorization: refs[1], GlobalBudget: windowV8Amounts{200000000, 4000000}, HumanReviewedExactScope: false}
	canonical, _ := json.Marshal(binding)
	raw, _ := json.Marshal(windowV9ExecutionAuthorization{binding, hex.EncodeToString(ed25519.Sign(key, canonical))})
	path := filepath.Join(root, "synthetic-v9-authority.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &windowV9Authority{manifest: m, manifestSHA: digest, binarySHA: digest, authorizationPath: path, authorizationSHA: conversationHash(raw), raw: raw, binding: binding}, key
}

func TestWindowV9DelegatedAuthorityAndSyntheticCanonicalVector(t *testing.T) {
	a, key := fixtureWindowV9Authority(t)
	if err := a.revalidate(); err != nil {
		t.Fatalf("synthetic signature rejected: %v", err)
	}
	view := a.nativeView()
	if err := view.revalidate(); err != nil {
		t.Fatal(err)
	}
	view.binding.OwnerID = "changed"
	if view.revalidate() == nil {
		t.Fatal("changed native view accepted")
	}
	if out := os.Getenv("WINDOW_V9_EXPORT_SYNTHETIC"); out != "" {
		canonical, _ := json.Marshal(a.binding)
		vector := map[string]any{"evidence_kind": "synthetic_canonical_signature_only_no_ready_or_execution_authority", "binding": a.binding, "canonical_binding_utf8": string(canonical), "public_key_hex": hex.EncodeToString(key.Public().(ed25519.PublicKey)), "signature_hex": hex.EncodeToString(ed25519.Sign(key, canonical)), "authorization": json.RawMessage(a.raw)}
		raw, _ := json.MarshalIndent(vector, "", "  ")
		if err := os.WriteFile(out, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mutations := map[string]func(*windowV9ExecutionBinding){
		"human_exact": func(b *windowV9ExecutionBinding) { b.HumanReviewedExactScope = true },
		"global":      func(b *windowV9ExecutionBinding) { b.GlobalBudget.Model++ },
		"autonomous":  func(b *windowV9ExecutionBinding) { b.AutonomousAuthorization.SHA256 = strings.Repeat("b", 64) },
		"reply":       func(b *windowV9ExecutionBinding) { b.HumanReply.SHA256 = strings.Repeat("b", 64) },
		"parent":      func(b *windowV9ExecutionBinding) { b.Parent = windowV8Amounts{200000000, 4000000} },
		"retry":       func(b *windowV9ExecutionBinding) { b.RetryPolicySHA256 = strings.Repeat("b", 64) },
		"source":      func(b *windowV9ExecutionBinding) { b.SourceSHA256 = strings.Repeat("b", 64) },
		"scope":       func(b *windowV9ExecutionBinding) { b.ScopeSHA256 = strings.Repeat("b", 64) },
		"relaunch":    func(b *windowV9ExecutionBinding) { b.NoRelaunch = false },
		"old_schema":  func(b *windowV9ExecutionBinding) { b.Schema = "full-window-execution-authorization.v8" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			b := a.binding
			mutate(&b)
			canonical, _ := json.Marshal(b)
			raw, _ := json.Marshal(windowV9ExecutionAuthorization{b, hex.EncodeToString(ed25519.Sign(key, canonical))})
			if _, err := verifyWindowV9ExecutionAuthorization(raw, a.manifest, a.manifestSHA, a.binarySHA); err == nil {
				t.Fatal("forged delegated binding accepted")
			}
		})
	}
	before := append([]byte(nil), a.raw...)
	if err := os.WriteFile(a.binding.AutonomousAuthorization.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.revalidate() == nil {
		t.Fatal("changed provenance accepted")
	}
	if !bytes.Equal(before, a.raw) {
		t.Fatal("verification rewrote evidence")
	}
}

func TestWindowV9RejectsOldActualBeforeSettings(t *testing.T) {
	for _, epoch := range []string{"v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8"} {
		t.Run(epoch, func(t *testing.T) {
			r := &FullWindowActualReady{manifest: windowActualManifest{Schema: "full-window-actual-ready." + epoch, Scope: fullWindowScope{Schema: "full-window-scope." + epoch}}}
			if _, err := OpenFullWindowActual(context.Background(), r, "/must-not-read-private-settings"); err == nil || !strings.Contains(err.Error(), "v9 delegated authority required before settings") {
				t.Fatalf("old epoch reached settings: %v", err)
			}
		})
	}
}

func TestWindowV9HelperCatalogReference(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	manifest := filepath.Join(root, "actual-manifest.json")
	build := filepath.Join(root, "helper-build")
	if err := os.Mkdir(build, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(build, "catalog.json")
	body := []byte(`{"schema":"synthetic-catalog-data-only"}`)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{Schema: "full-window-actual-ready.v9", HelperCatalog: &windowActualFile{path, conversationHash(body)}}
	if err := validateWindowActualHelperCatalog(m, manifest); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "foreign", "sha", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			v := m
			ref := *m.HelperCatalog
			v.HelperCatalog = &ref
			switch mode {
			case "missing":
				v.HelperCatalog = nil
			case "foreign":
				v.HelperCatalog.Path = filepath.Join(root, "other-catalog.json")
			case "sha":
				v.HelperCatalog.SHA256 = strings.Repeat("b", 64)
			case "legacy":
				v.Schema = "full-window-actual-ready.v8"
			}
			if validateWindowActualHelperCatalog(v, manifest) == nil {
				t.Fatal("unbound catalog accepted")
			}
		})
	}
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".original", path); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualHelperCatalog(m, manifest) == nil {
		t.Fatal("catalog symlink accepted")
	}
}
