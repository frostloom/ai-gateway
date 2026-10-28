package taskbench

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

func TestFullWindowActualReadyDefaultRejectsWithoutSideEffects(t *testing.T) {
	root := t.TempDir()
	_, token, err := CheckFullWindowActualReady(FullWindowActualReadyOptions{ManifestPath: filepath.Join(root, "missing")})
	if err == nil || token != nil {
		t.Fatal("unanchored build accepted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("Check wrote artifacts")
	}
	var forged FullWindowActualReady
	if json.Unmarshal([]byte(`{}`), &forged) != nil {
		t.Fatal("unmarshal")
	}
	if forged.revalidate() == nil {
		t.Fatal("JSON minted ready token")
	}
}

func TestFullWindowActualReadyStrictJSON(t *testing.T) {
	for _, raw := range []string{`null`, `{"schema":null}`, `{"schema":"a","schema":"b"}`, `{"unknown":1}`, `{"schema":"a"} {}`, `{"schema":"\xff"}`} {
		var m windowActualManifest
		if decodeWindowActualJSON([]byte(raw), &m) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestFullWindowActualReadyRoots(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(parent, "session"), filepath.Join(parent, "campaign")
	if validateWindowActualRoots(a, b) != nil {
		t.Fatal("new sibling roots rejected")
	}
	for _, pair := range [][2]string{{a, a}, {a, filepath.Join(a, "nested")}, {"relative", b}} {
		if validateWindowActualRoots(pair[0], pair[1]) == nil {
			t.Fatal("bad roots accepted")
		}
	}
	if err := os.Mkdir(a, 0700); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualRoots(a, b) == nil {
		t.Fatal("existing session root accepted")
	}
}

func TestFullWindowActualReadyFileBoundaries(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "evidence")
	raw := []byte("synthetic fixture")
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualFile(p, conversationHash(raw), 64); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualFile(p, strings.Repeat("0", 64), 64); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	if _, err := readWindowActualFile(p, conversationHash(raw), 1); err == nil {
		t.Fatal("oversized accepted")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualFile(link, conversationHash(raw), 64); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestFullWindowActualReadyFullGoEvidence(t *testing.T) {
	good := []byte("{\"Action\":\"start\",\"Package\":\"example/p\"}\n{\"Action\":\"pass\",\"Package\":\"example/p\"}\n")
	if validateWindowActualGoJSON(good, []string{"example/p"}) != nil {
		t.Fatal("synthetic successful package stream rejected")
	}
	for _, raw := range [][]byte{[]byte(`{"exit":0}`), []byte("{\"Action\":\"fail\",\"Package\":\"example/p\"}\n"), good[:strings.Index(string(good), "\n")+1], append(append([]byte{}, good...), []byte("{\"Action\":\"fail\",\"Package\":\"example/p\",\"Test\":\"TestOops\"}\n")...)} {
		if validateWindowActualGoJSON(raw, []string{"example/p"}) == nil {
			t.Fatalf("bad Go evidence accepted %s", raw)
		}
	}
	if validateWindowActualGoJSON(good, []string{"example/p", "example/missing"}) == nil {
		t.Fatal("missing package accepted")
	}
}

// These private fixtures only exercise evidence consistency, never readiness.
func TestFullWindowActualReadyProviderEvidence(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "basis")
	raw := []byte("synthetic test basis; not actual tariff or envelope")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	limits := windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxOutput: 128}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0"}}
	source := strings.Repeat("a", 64)
	e := windowActualProviderEvidence{Schema: "full-window-actual-prices.v1", EvidenceKind: "actual", Limits: limits, ModelEndpoint: "https://api.deepseek.com/anthropic/v1/messages", JEVEndpoint: "https://api.typesafe.ai/v1/systemone", SourceSHA256: source, Basis: []windowActualFile{{path, conversationHash(raw)}}}
	if validateWindowActualProviderEvidence(e, e.Schema, limits, source) != nil {
		t.Fatal("synthetic consistency fixture rejected")
	}
	for _, mutate := range []func(*windowActualProviderEvidence){func(e *windowActualProviderEvidence) { e.EvidenceKind = "local_stub" }, func(e *windowActualProviderEvidence) { e.Limits.Model.InputRate++ }, func(e *windowActualProviderEvidence) { e.ModelEndpoint += "?" }, func(e *windowActualProviderEvidence) { e.SourceSHA256 = strings.Repeat("b", 64) }, func(e *windowActualProviderEvidence) { e.Basis = nil }} {
		changed := e
		mutate(&changed)
		if validateWindowActualProviderEvidence(changed, e.Schema, limits, source) == nil {
			t.Fatal("foreign evidence accepted")
		}
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualProviderEvidence(e, e.Schema, limits, source) == nil {
		t.Fatal("changed basis accepted")
	}
}

func TestFullWindowActualReadyForeignAnchor(t *testing.T) {
	oldSHA, oldCommit, oldTree := fullWindowActualManifestSHA, fullWindowActualArchiveCommit, fullWindowActualArchiveTree
	t.Cleanup(func() {
		fullWindowActualManifestSHA, fullWindowActualArchiveCommit, fullWindowActualArchiveTree = oldSHA, oldCommit, oldTree
	})
	fullWindowActualArchiveCommit = strings.Repeat("a", 40)
	fullWindowActualArchiveTree = strings.Repeat("b", 40)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{"schema":"full-window-actual-ready.v1","evidence_kind":"local_stub"}`, `{"schema":"full-window-actual-ready.v1","unknown":true}`} {
		p := filepath.Join(root, "manifest")
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		fullWindowActualManifestSHA = conversationHash([]byte(raw))
		_, token, err := CheckFullWindowActualReady(FullWindowActualReadyOptions{p})
		if err == nil || token != nil {
			t.Fatal("foreign manifest minted token")
		}
		entries, _ := os.ReadDir(root)
		if len(entries) != 1 {
			t.Fatal("Check wrote artifacts")
		}
	}
}

func TestFullWindowActualReadyRejectsNonPrivateParent(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualRoots(filepath.Join(parent, "session"), filepath.Join(parent, "campaign")) == nil {
		t.Fatal("non-private parent accepted")
	}
}

func TestFullWindowActualReadyNoTestPackages(t *testing.T) {
	raw := []byte("{\"Action\":\"start\",\"Package\":\"example/no\"}\n{\"Action\":\"output\",\"Package\":\"example/no\",\"Output\":\"?   \\texample/no\\t[no test files]\\n\"}\n{\"Action\":\"skip\",\"Package\":\"example/no\"}\n")
	if err := validateWindowActualGoPackages(raw, map[string]bool{"example/no": false}); err != nil {
		t.Fatal("legitimate no-test package rejected", err)
	}
	for _, bad := range []struct {
		raw      []byte
		packages map[string]bool
	}{
		{raw, map[string]bool{"example/no": true}},
		{raw, map[string]bool{"example/no": false, "example/missing": true}},
		{bytes.Replace(raw, []byte("[no test files]"), []byte("other"), 1), map[string]bool{"example/no": false}},
		{append(append([]byte{}, raw...), []byte("{\"Action\":\"skip\",\"Package\":\"example/no\"}\n")...), map[string]bool{"example/no": false}},
		{append([]byte("{\"Action\":\"run\",\"Package\":\"example/no\",\"Test\":\"TestHidden\"}\n"), raw...), map[string]bool{"example/no": false}},
	} {
		if validateWindowActualGoPackages(bad.raw, bad.packages) == nil {
			t.Fatal("invalid skip or incomplete set accepted")
		}
	}
}

func TestFullWindowActualReadyFrozenPaths(t *testing.T) {
	root := "/fixed/source"
	if validateWindowActualInputPaths(windowActualManifest{SourceRoot: root, DefinitionsDir: root + "/eval/task-definitions/2026-10-12-full-dev-v1", ProfilesDir: root + "/eval/runtime-profiles/2026-10-13-six-profiles-v1"}) != nil {
		t.Fatal("exact frozen paths rejected")
	}
	if validateWindowActualInputPaths(windowActualManifest{SourceRoot: root, DefinitionsDir: root + "/eval/task-definitions", ProfilesDir: root + "/eval/runtime-profiles"}) == nil {
		t.Fatal("unversioned roots accepted")
	}
}

func TestFullWindowActualReadyBuildPins(t *testing.T) {
	m := windowActualManifest{ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40)}
	sha := strings.Repeat("c", 64)
	prefix := "github.com/frostloom/ai-gateway/internal/taskbench."
	flags := "-X " + prefix + "fullWindowActualManifestSHA=" + sha + " -X " + prefix + "fullWindowActualArchiveCommit=" + m.ArchiveCommit + " -X " + prefix + "fullWindowActualArchiveTree=" + m.ArchiveTree
	info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/frostloom/ai-gateway"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}}}
	if validateWindowActualBuildPins(info, m, sha) != nil {
		t.Fatal("exact injected pins rejected")
	}
	info.Settings = nil
	if validateWindowActualBuildPins(info, m, sha) == nil {
		t.Fatal("missing compiler pin provenance accepted")
	}
	info.Settings = []debug.BuildSetting{{Key: "-ldflags", Value: strings.Replace(flags, sha, strings.Repeat("d", 64), 1)}}
	if validateWindowActualBuildPins(info, m, sha) == nil {
		t.Fatal("foreign injected anchor accepted")
	}
}

func TestFullWindowActualReadyPackageInventory(t *testing.T) {
	root := t.TempDir()
	sources := map[string]string{}
	for p, raw := range map[string]string{"yes/a.go": "package yes\n", "yes/a_test.go": "package yes\n", "no/a.go": "package no\n", "ignored/a.go": "//go:build ignore\n\npackage ignored\n", "no/ignored_test.go": "//go:build ignore\n\npackage no\n"} {
		path := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		sources[p] = conversationHash([]byte(raw))
	}
	inventory, err := windowActualPackageInventory(root, sources)
	if err != nil || !reflect.DeepEqual(inventory, map[string]bool{"github.com/frostloom/ai-gateway/yes": true, "github.com/frostloom/ai-gateway/no": false}) {
		t.Fatalf("wrong build-constrained inventory: %v %v", inventory, err)
	}
}

func TestFullWindowActualReadyBuildModifiedAndReplace(t *testing.T) {
	m := windowActualManifest{ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40)}
	sha := strings.Repeat("c", 64)
	prefix := "github.com/frostloom/ai-gateway/internal/taskbench."
	flags := "-X " + prefix + "fullWindowActualManifestSHA=" + sha + " -X " + prefix + "fullWindowActualArchiveCommit=" + m.ArchiveCommit + " -X " + prefix + "fullWindowActualArchiveTree=" + m.ArchiveTree
	base := debug.BuildInfo{Main: debug.Module{Path: "github.com/frostloom/ai-gateway"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}}}
	for _, extra := range []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: strings.Repeat("d", 40)}} {
		info := base
		info.Settings = append(append([]debug.BuildSetting{}, base.Settings...), extra)
		if validateWindowActualBuildPins(&info, m, sha) == nil {
			t.Fatal("modified/foreign VCS build accepted")
		}
	}
	info := base
	info.Deps = []*debug.Module{{Path: "example", Replace: &debug.Module{Path: "/local"}}}
	if validateWindowActualBuildPins(&info, m, sha) == nil {
		t.Fatal("module replacement accepted")
	}
}

func TestFullWindowActualReadyWholeSourceTree(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"main.go": "package main\n", "script.lua": "return 1", "web/dist/index.html": "<p>frozen</p>"}
	sources := map[string]string{}
	for p, raw := range files {
		path := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		sources[p] = conversationHash([]byte(raw))
	}
	if validateWindowActualSourceFiles(root, sources) != nil {
		t.Fatal("whole synthetic source rejected")
	}
	if err := os.WriteFile(filepath.Join(root, "script.lua"), []byte("return 2"), 0600); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualSourceFiles(root, sources) == nil {
		t.Fatal("modified embedded Lua accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "script.lua"), []byte(files["script.lua"]), 0600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "web/dist/new.js")
	if err := os.WriteFile(extra, []byte("unbound"), 0600); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualSourceFiles(root, sources) == nil {
		t.Fatal("unbound embedded file accepted")
	}
}

func TestFullWindowActualReadySignedExecutable(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "fake-executable")
	body := []byte("obviously synthetic executable fixture")
	if err := os.WriteFile(exe, body, 0600); err != nil {
		t.Fatal(err)
	}
	m := windowActualManifest{ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40), BuildAttestationPublicKey: hex.EncodeToString(pub)}
	manifestSHA := strings.Repeat("c", 64)
	binding := windowActualBinaryBinding{Schema: "full-window-actual-binary-attestation.v1", ArchiveCommit: m.ArchiveCommit, ArchiveTree: m.ArchiveTree, ReadyManifestSHA256: manifestSHA, BinarySHA256: conversationHash(body)}
	sign := func(b windowActualBinaryBinding) []byte {
		canonical, _ := json.Marshal(b)
		raw, _ := json.Marshal(windowActualBinaryAttestation{windowActualBinaryBinding: b, Signature: hex.EncodeToString(ed25519.Sign(priv, canonical))})
		return raw
	}
	valid := sign(binding)
	if validateWindowActualBinaryAttestation(valid, m, manifestSHA, exe) != nil {
		t.Fatal("valid temporary signature rejected")
	}
	if validateWindowActualBinaryAttestation(nil, m, manifestSHA, exe) == nil {
		t.Fatal("missing signature accepted")
	}
	for _, mutate := range []func(*windowActualBinaryBinding){func(b *windowActualBinaryBinding) { b.ArchiveCommit = strings.Repeat("d", 40) }, func(b *windowActualBinaryBinding) { b.ReadyManifestSHA256 = strings.Repeat("d", 64) }, func(b *windowActualBinaryBinding) { b.BinarySHA256 = strings.Repeat("d", 64) }} {
		changed := binding
		mutate(&changed)
		if validateWindowActualBinaryAttestation(sign(changed), m, manifestSHA, exe) == nil {
			t.Fatal("foreign signed binding accepted")
		}
	}
	forged := bytes.Replace(valid, []byte(`"signature":"`), []byte(`"signature":"00`), 1)
	if validateWindowActualBinaryAttestation(forged, m, manifestSHA, exe) == nil {
		t.Fatal("forged signature accepted")
	}
	if err := os.WriteFile(exe, []byte("changed executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if validateWindowActualBinaryAttestation(valid, m, manifestSHA, exe) == nil {
		t.Fatal("changed executable accepted")
	}
}

func TestFullWindowActualReadyPrivateSettingsDigest(t *testing.T) {
	if validateWindowActualPrivateSettingsBinding(nil) == nil {
		t.Fatal("nil settings binding accepted")
	}
	for _, sha := range []string{"", "not-a-digest", strings.Repeat("a", 63), strings.Repeat("A", 64)} {
		if validateWindowActualPrivateSettingsBinding(&windowActualManifest{PrivateSettingsSHA256: sha}) == nil {
			t.Fatal("invalid settings digest accepted")
		}
	}
	if err := validateWindowActualPrivateSettingsBinding(&windowActualManifest{PrivateSettingsSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
}
