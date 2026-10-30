package taskbench

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHistoryWiringTopClosureRejectsChangedDependencyAndZeroesValue(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	value, err := runHistoryReadPass(context.Background(), func(p *historyReadPass) (string, error) {
		if _, e := historyStoppedFile(p, s.path, s.sha, s.max); e != nil {
			return "", e
		}
		if e := os.WriteFile(s.path, []byte("edit"), 0600); e != nil {
			return "", e
		}
		return "provisional", nil
	})
	if err == nil || value != "" {
		t.Fatal("top returned unclosed provisional baseline", value, err)
	}
}

func TestHistoryWiringPayloadRegistersEveryOriginalAndNamespace(t *testing.T) {
	for _, change := range []string{"", "body", "extra"} {
		t.Run(change, func(t *testing.T) {
			dir, m := stoppedV2PayloadFixture(t)
			p := historyTestPass(t)
			if err := historyStoppedRoot(p, dir); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := readWindowStoppedV2PayloadWithPass(p, dir, m); err != nil {
					t.Fatal(err)
				}
			}
			if got := p.Stats(); got.FirstPhysical != 8 || got.CacheHits != 8 {
				t.Fatal("payload input coverage differs", got)
			}
			if len(p.namespaces) != 1 {
				t.Fatal("payload namespace absent")
			}
			if change != "" {
				name := "request-000001.body"
				if change == "extra" {
					name = "unaccounted"
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := p.Finish()
			if (change == "") != (err == nil) {
				t.Fatal("closure freshness", change, err)
			}
			if change == "" && p.Stats().EndPhysical != 8 {
				t.Fatal("End omitted payload inputs", p.Stats())
			}
		})
	}
}

func TestHistoryWiringNativeAndQualityKeepSemanticResults(t *testing.T) {
	for _, kind := range []string{"native3", "native4", "quality"} {
		t.Run(kind, func(t *testing.T) {
			p := historyTestPass(t)
			if kind == "quality" {
				dir, slot, row, record := stoppedV4QualityFixture(t)
				old, err := readWindowStoppedV4Quality(dir, windowOwnedQualityPolicySHA(), slot, row, record)
				if err != nil {
					t.Fatal(err)
				}
				got, err := readWindowStoppedV4QualityWithPass(p, dir, windowOwnedQualityPolicySHA(), slot, row, record)
				if err != nil || !reflect.DeepEqual(old, got) {
					t.Fatal("quality semantic result changed", err)
				}
				if p.Stats().FirstPhysical != 2 {
					t.Fatal("quality sidecar coverage", p.Stats())
				}
				if err = p.Finish(); err != nil {
					t.Fatal(err)
				}
				return
			}
			fixture := stoppedV3NativeFixture
			reader := readWindowStoppedV3Native
			pooled := readWindowStoppedV3NativeWithPass
			if kind == "native4" {
				fixture = stoppedV4NativeFixture
				reader = readWindowStoppedV4Native
				pooled = readWindowStoppedV4NativeWithPass
			}
			dir, m, ledger := fixture(t)
			old, err := reader(dir, m, ledger)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				got, err := pooled(p, dir, m, ledger)
				if err != nil || !reflect.DeepEqual(old, got) {
					t.Fatal("native semantic result changed", err)
				}
			}
			if p.Stats().CacheHits == 0 || len(p.namespaces) != 1 {
				t.Fatal("native descendants not registered", p.Stats())
			}
			if err = p.Finish(); err != nil {
				t.Fatal(err)
			}
			if p.Stats().EndPhysical != p.Stats().FirstPhysical {
				t.Fatal("native End omitted dependency", p.Stats())
			}
		})
	}
}

func TestHistoryWiringV7StreamsNeverCacheAndEndRegistersEmptyFiles(t *testing.T) {
	for _, mutation := range []string{"", "body", "extra", "empty"} {
		t.Run(mutation, func(t *testing.T) {
			root := stoppedEvidenceRoot(t)
			if err := os.Mkdir(filepath.Join(root, "campaign"), 0700); err != nil {
				t.Fatal(err)
			}
			inv := windowStoppedV7Inventory{Root: root, Files: map[string]windowStoppedV7InventoryEntry{}}
			for name, raw := range map[string][]byte{"campaign/a": []byte("abc"), "campaign/b": nil} {
				if err := os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
				inv.Files[name] = windowStoppedV7InventoryEntry{SHA256: conversationHash(raw), Bytes: int64(len(raw))}
			}
			p := historyTestPass(t)
			if err := historyStoppedRoot(p, root); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, _, err := readWindowStoppedV7InventoryWithPass(p, root, inv); err != nil {
					t.Fatal(err)
				}
			}
			if p.Stats().FirstPhysical != 4 || p.Stats().CacheBytes != 0 || len(p.namespaces) != 1 {
				t.Fatal("direct streams cached or unregistered", p.Stats())
			}
			if mutation != "" {
				name := "campaign/a"
				if mutation == "extra" {
					name = "campaign/new"
				}
				if mutation == "empty" {
					name = "campaign/b"
				}
				if err := os.WriteFile(filepath.Join(root, name), []byte("edit"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := p.Finish()
			if (mutation == "") != (err == nil) {
				t.Fatal(mutation, err)
			}
			if mutation == "" && p.Stats().EndPhysical != 2 {
				t.Fatal("End skipped stream dependency", p.Stats())
			}
		})
	}
}

func TestHistoryWiringFirstPinsAndFixedNamespaces(t *testing.T) {
	root := stoppedEvidenceRoot(t)
	for name := range windowStoppedArtifactPins() {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "campaign/.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	p := historyTestPass(t)
	if err := validateWindowStoppedInventoryWithPass(p, root); err != nil {
		t.Fatal(err)
	}
	if len(p.namespaces) != 5 {
		t.Fatal("first fixed namespaces not registered", len(p.namespaces))
	}
	pins := map[string]string{"records/startup.json": conversationHash([]byte("fixture"))}
	for i := 0; i < 2; i++ {
		if _, err := readWindowStoppedFilesWithPass(p, root, pins, 65536); err != nil {
			t.Fatal(err)
		}
	}
	if p.Stats().FirstPhysical != 1 || p.Stats().CacheHits != 1 {
		t.Fatal(p.Stats())
	}
	if err := os.WriteFile(filepath.Join(root, "campaign/claim-000002.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err == nil {
		t.Fatal("End omitted fixed first namespace")
	}
}

func TestHistoryWiringSignedExecutableIsFreshEndDependency(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := historyTestFile(t, []byte("synthetic executable"))
	m := windowActualManifest{ArchiveCommit: strings.Repeat("a", 40), ArchiveTree: strings.Repeat("b", 40), BuildAttestationPublicKey: hex.EncodeToString(pub)}
	manifest := strings.Repeat("c", 64)
	binding := windowActualBinaryBinding{Schema: "full-window-actual-binary-attestation.v1", ArchiveCommit: m.ArchiveCommit, ArchiveTree: m.ArchiveTree, ReadyManifestSHA256: manifest, BinarySHA256: s.sha}
	canonical, _ := json.Marshal(binding)
	raw, _ := json.Marshal(windowActualBinaryAttestation{windowActualBinaryBinding: binding, Signature: hex.EncodeToString(ed25519.Sign(priv, canonical))})
	p := historyTestPass(t)
	for i := 0; i < 2; i++ {
		if err = historyStoppedBinaryAttestation(p, raw, m, manifest, s.path); err != nil {
			t.Fatal(err)
		}
	}
	if p.Stats().FirstPhysical != 2 || p.Stats().CacheBytes != 0 {
		t.Fatal("executable stream cached or unregistered", p.Stats())
	}
	if err = os.WriteFile(s.path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = p.Finish(); err == nil {
		t.Fatal("End omitted executable")
	}
	q := historyTestPass(t)
	raw[len(raw)-2] = 'x'
	if err = historyStoppedBinaryAttestation(q, raw, m, manifest, s.path); err == nil || q.Stats().FirstPhysical != 0 {
		t.Fatal("signature bypass or executable touched before signature validation", err)
	}
}

func TestHistoryWiringDeadlineAndFailureNeverReturnTypedValue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if value, err := loadWindowStoppedV7BaselineContext(ctx, "/absent", windowStoppedV7DescriptorSHA); err == nil || !reflect.DeepEqual(value, windowStoppedV7Baseline{}) {
		t.Fatal("cancelled top returned value", err)
	}
	if v, e := runHistoryReadPass(context.Background(), func(*historyReadPass) (string, error) { return "provisional", fmt.Errorf("semantic rejection") }); e == nil || v != "" {
		t.Fatal(v, e)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if v, e := runHistoryReadPass(ctx, func(*historyReadPass) (string, error) { cancel(); return "provisional", nil }); e == nil || v != "" {
		t.Fatal(v, e)
	}
}

func TestHistoryWiringNestedConstructorsRequireExplicitPass(t *testing.T) {
	checks := []func() error{
		func() error {
			v, e := loadWindowStoppedBaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedBaseline{}) {
				t.Error("nonzero first")
			}
			return e
		},
		func() error {
			v, e := loadWindowStoppedV2BaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedV2Baseline{}) {
				t.Error("nonzero v2")
			}
			return e
		},
		func() error {
			v, e := loadWindowStoppedV3BaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedV3Baseline{}) {
				t.Error("nonzero v3")
			}
			return e
		},
		func() error {
			v, e := loadWindowStoppedV4BaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedV4Baseline{}) {
				t.Error("nonzero v4")
			}
			return e
		},
		func() error {
			v, e := loadWindowStoppedV5BaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedV5Baseline{}) {
				t.Error("nonzero v5")
			}
			return e
		},
		func() error {
			v, e := loadWindowStoppedV6BaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedV6Baseline{}) {
				t.Error("nonzero v6")
			}
			return e
		},
		func() error {
			v, e := loadWindowStoppedV7BaselineWithPass(nil, "/absent", "")
			if !reflect.DeepEqual(v, windowStoppedV7Baseline{}) {
				t.Error("nonzero v7")
			}
			return e
		},
	}
	for i, check := range checks {
		if err := check(); err == nil {
			t.Fatal("nested constructor silently opened/faked pass", i)
		}
	}
}

func TestHistoryWiringRootReregistrationKeepsFirstMarker(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	root := filepath.Dir(s.path)
	p := historyTestPass(t)
	if err := historyStoppedRoot(p, root); err != nil {
		t.Fatal(err)
	}
	initial := p.dirs[root]
	if err := os.WriteFile(filepath.Join(root, "transient"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "transient")); err != nil {
		t.Fatal(err)
	}
	if err := historyStoppedRoot(p, root); err != nil {
		t.Fatal("classification is not idempotent", err)
	}
	if p.dirs[root] != initial {
		t.Fatal("registration rebased first marker")
	}
	if err := p.Finish(); err == nil {
		t.Fatal("reregistration hid immutable root ABA")
	}
}

func TestHistoryWiringPrivateGraphHasNoUntrackedReadsOrNewPass(t *testing.T) {
	paths, err := filepath.Glob("window_stopped*.go")
	if err != nil {
		t.Fatal(err)
	}
	constructors := map[string]int{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, "window_stopped_v8") || strings.HasPrefix(path, "window_stopped_v9") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasSuffix(fn.Name.Name, "WithPass") {
				continue
			}
			if len(fn.Type.Params.List) == 0 {
				t.Fatal("missing pass", fn.Name.Name)
			}
			param := fn.Type.Params.List[0].Names[0].Name
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				name := id.Name
				if name == "newHistoryReadPass" || name == "runHistoryReadPass" || name == "readWindowActualFile" || name == "readWindowActualBytes" || name == "readWindowStoppedBatch4" || name == "withWindowStoppedLock" || name == "validateWindowStoppedV2Dir" || name == "readWindowStoppedV7File" || name == "validateWindowActualBinaryAttestation" {
					t.Errorf("%s invokes untracked %s", fn.Name.Name, name)
				}
				if strings.HasPrefix(name, "loadWindowStopped") && !strings.HasSuffix(name, "WithPass") {
					t.Errorf("nested top-level pool in %s: %s", fn.Name.Name, name)
				}
				if strings.HasSuffix(name, "WithPass") || strings.HasPrefix(name, "historyStopped") {
					if len(call.Args) == 0 {
						t.Error("missing pass argument", name)
						return true
					}
					arg, ok := call.Args[0].(*ast.Ident)
					if !ok || arg.Name != param {
						t.Errorf("%s not forwarding its pass to %s", fn.Name.Name, name)
					}
				}
				if strings.HasPrefix(name, "loadWindowStopped") && strings.HasSuffix(name, "WithPass") {
					constructors[fn.Name.Name]++
				}
				return true
			})
		}
	}
	for v := 2; v <= 7; v++ {
		name := fmt.Sprintf("loadWindowStoppedV%dBaselineWithPass", v)
		if constructors[name] != 2 {
			t.Errorf("%s omitted original before/after predecessor: %d", name, constructors[name])
		}
	}
}

func TestHistoryWiringBatchSharesPassWithoutOuterSlots(t *testing.T) {
	s := historyTestFile(t, []byte("body"))
	_, err := runHistoryReadPass(context.Background(), func(p *historyReadPass) (struct{}, error) {
		specs := []windowStoppedReadSpec{{s.path, s.sha, s.max, windowStoppedReadFile}, {s.path, s.sha, s.max, windowStoppedReadFile}}
		for i := 0; i < 2; i++ {
			if _, e := historyStoppedBatch(p, specs); e != nil {
				return struct{}{}, e
			}
		}
		if stats := p.Stats(); stats.FirstPhysical != 1 || stats.CacheHits != 3 {
			t.Error("nested batches not sharing pass", stats)
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
