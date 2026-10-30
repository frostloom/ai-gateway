package taskbench

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
)

// This opt-in material-export test follows genuine private V8 campaign and
// acquisition APIs. It is not actual readiness, an actual Session or human
// approval. All newly created claims/authority live under a fresh t.TempDir.
func TestWindowV8CollectedWindowFixtureExport(t *testing.T) {
	if os.Getenv("TASKBENCH_V8_COLLECTED_FIXTURE_EXPORT") != "1" {
		t.Skip("explicit synthetic full-window fixture export only")
	}
	repo := "/Users/a1/项目/ai-gateway"
	exportRoot := filepath.Join(repo, ".tmp/window-v8-runtime-20261030/fixtures/window-001")
	if err := os.Mkdir(exportRoot, 0700); err != nil {
		t.Fatal("new export namespace required", err)
	}
	write := func(name string, v any) {
		t.Helper()
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err = os.WriteFile(filepath.Join(exportRoot, name), append(raw, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}
	sourcePinsPath := filepath.Join(repo, ".tmp/window-v8-runtime-20261030/native-metadata-fix/source-final.json")
	pinRaw, err := os.ReadFile(sourcePinsPath)
	if err != nil {
		t.Fatal(err)
	}
	protected := map[string]string{}
	if err = json.Unmarshal(pinRaw, &protected); err != nil {
		t.Fatal(err)
	}
	verifySource := func() map[string]string {
		out := map[string]string{}
		for p, want := range protected {
			raw, e := os.ReadFile(filepath.Join(repo, p))
			if e != nil {
				t.Error(e)
				continue
			}
			out[p] = conversationHash(raw)
			if out[p] != want {
				t.Errorf("protected privacy source changed: %s", p)
			}
		}
		return out
	}
	beforeSource := verifySource()
	write("protected-source-before.json", beforeSource)
	parent := stoppedEvidenceRoot(t)
	var campaign *windowCampaign
	var stopped windowStoppedV7Baseline
	var report windowAcquisitionReceipt
	var reportRoot string
	var oldBefore *windowV8ReadonlyReceipt
	timings := map[string]float64{}
	phase := "initial"
	stage := func(name string, fn func() error) error {
		phase = name
		t.Log("stage begin", name)
		started := time.Now()
		e := fn()
		timings[name] = time.Since(started).Seconds()
		t.Logf("stage end %s %.6fs err=%v", name, timings[name], e)
		write("stages.json", map[string]any{"evidence_kind": "synthetic_controlled_export_not_actual_authority", "last_stage": name, "duration_seconds": timings, "last_error": fmt.Sprint(e), "actual_acquisition_limit_seconds": 300, "outer_test_limit_seconds": 600})
		return e
	}
	// Registered before resource cleanups, so final copies see flushed/closed new
	// evidence. The real old files are read only and are never copied or altered.
	t.Cleanup(func() {
		if campaign != nil {
			if e := campaign.closeWindowCampaign(); e != nil {
				t.Log("controlled campaign close", e)
			}
		}
		if reportRoot != "" {
			if e := copyWindowV8FixtureTree(reportRoot, filepath.Join(exportRoot, "records")); e != nil && !os.IsNotExist(e) {
				t.Error(e)
			}
		}
		if campaign != nil {
			if e := copyWindowV8FixtureTree(campaign.root, filepath.Join(exportRoot, "campaign")); e != nil {
				t.Error(e)
			}
		}
		for _, name := range []string{"synthetic-authorization.json", "synthetic-reply.txt", "synthetic-manifest.json", "scope.json"} {
			raw, e := os.ReadFile(filepath.Join(parent, name))
			if e == nil {
				if e = os.WriteFile(filepath.Join(exportRoot, name), raw, 0600); e != nil {
					t.Error(e)
				}
			}
		}
		oldUnchanged := false
		if oldBefore != nil {
			after, e := readWindowV8FixtureReadonlyReceipt(stopped)
			if e != nil {
				t.Error(e)
			} else {
				write("old-evidence-after.json", after)
				oldUnchanged = reflect.DeepEqual(oldBefore, after)
				if !oldUnchanged {
					t.Error("old6395/44 evidence changed")
				}
			}
		}
		afterSource := verifySource()
		write("protected-source-after.json", afterSource)
		testRaw, e := os.ReadFile(filepath.Join(repo, "internal/taskbench/window_collected_v8_export_test.go"))
		if e != nil {
			t.Error(e)
		}
		fresh := 1
		if campaign != nil {
			fresh = 3
		}
		if report.Advanced {
			fresh = 4
		}
		write("export-result.json", map[string]any{"evidence_kind": "genuine_local_stub_owned_window_not_actual_ready", "last_stage": phase, "test_failed": t.Failed(), "receipt_status": report.Status, "advanced": report.Advanced, "next_claim_sha256": report.NextClaimSHA256, "actual_acquisition_limit_seconds": 300, "outer_test_limit_seconds": 600, "stage_duration_seconds": timings, "old6395_and44_unchanged": oldUnchanged, "protected26_source_unchanged": reflect.DeepEqual(beforeSource, afterSource), "export_test_sha256": conversationHash(testRaw), "fresh_history_loads_completed_minimum": fresh, "fresh_history_count_basis": "one explicit loader; successful start requires prepared-history and first-claim fresh loads; successful next claim adds one. No production counter/cache was introduced.", "actual_session_summary": "not_created_or_claimed", "statistics_source": "production returned receipt.v8", "source_temp_root": parent, "copied_evidence_is_not_live_owner_or_reopenable_authority": true})
		if e := writeWindowV8FixtureInventory(exportRoot); e != nil {
			t.Error(e)
		}
	})
	descriptor := filepath.Join(repo, ".tmp/paired-live-private/full1220-actual-v7-freeze-20261030/continuation-design/stopped-v7.private.json")
	if err = stage("load_concrete44", func() error {
		var e error
		stopped, e = loadWindowStoppedV7Baseline(descriptor, windowStoppedV7DescriptorSHA)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = stage("old_evidence_before_sha", func() error {
		var e error
		oldBefore, e = readWindowV8FixtureReadonlyReceipt(stopped)
		if e == nil {
			write("old-evidence-before.json", oldBefore)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	source, profiles, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = windowOwnedQualityPolicySHA()
	scope, err := prepareFullWindowScopeV8(source, profiles, 28, windowV7TestCaps(), bindings, windowV8BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	scopeSHA, _, err := validateFullWindowScope(scope, source, profiles, bindings)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "synthetic-campaign")
	reportRoot = filepath.Join(parent, "synthetic-records")
	limits := windowV4ExpectedLimits()
	executionSHA, err := executionSHAWindowCampaign(scopeSHA, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	var prepared windowCampaignPrepared
	if err = stage("prepare", func() error {
		var e error
		prepared, e = prepareWindowCampaignV8Technical(root, root, scope, source, profiles, bindings, scopeSHA, executionSHA, limits)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reply := []byte("Synthetic controlled test fixture only. This is not an actual human approval or budget authorization.\n")
	replyPath := filepath.Join(parent, "synthetic-reply.txt")
	if err = os.WriteFile(replyPath, reply, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := windowActualManifest{Schema: "full-window-actual-ready.v8", EvidenceKind: "local_stub", Scope: scope, ScopeSHA256: scopeSHA, ExecutionSHA256: executionSHA, BuildAttestationPublicKey: hex.EncodeToString(pub), Sources: map[string]string{"synthetic-local-fixture-only.go": conversationHash([]byte("synthetic marker, not frozen actual source"))}, CampaignRoot: root, SessionRoot: reportRoot, BaselineEvidence: &windowActualFile{descriptor, windowStoppedV7DescriptorSHA}, HistoricalClaims: stopped.base.ClaimSHA256, ClaimsDir: stopped.historicalDir, Limits: limits}
	manifestRaw, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(parent, "synthetic-manifest.json"), manifestRaw, 0600); err != nil {
		t.Fatal(err)
	}
	scopeRaw, _ := json.Marshal(scope)
	if err = os.WriteFile(filepath.Join(parent, "scope.json"), scopeRaw, 0600); err != nil {
		t.Fatal(err)
	}
	sourceRaw, _ := json.Marshal(manifest.Sources)
	binaryMarker := conversationHash([]byte("synthetic fixture marker, not actual executing binary attestation"))
	binding := windowV8ExecutionBinding{Schema: "full-window-execution-authorization.v8", HumanAuthorized: true, RetryPolicyApproved: true, HumanReply: windowActualFile{replyPath, conversationHash(reply)}, ManifestSHA256: conversationHash(manifestRaw), SourceSHA256: conversationHash(sourceRaw), BinarySHA256: binaryMarker, ScopeSHA256: scopeSHA, ExecutionSHA256: executionSHA, BudgetDeclarationSHA256: scope.BudgetDeclarationSHA256, RetryPolicySHA256: scope.RetryPolicySHA256, Parent: windowV8Amounts{140000000, 1100000}, OwnerID: "synthetic-controlled-window-fixture", SingleOwner: true, NoRelaunch: true}
	canonical, _ := json.Marshal(binding)
	authorizationRaw, _ := json.Marshal(windowV8ExecutionAuthorization{binding, hex.EncodeToString(ed25519.Sign(key, canonical))})
	authPath := filepath.Join(parent, "synthetic-authorization.json")
	if err = os.WriteFile(authPath, authorizationRaw, 0600); err != nil {
		t.Fatal(err)
	}
	prepared.stoppedHistory = stopped
	prepared.v8Authority = &windowV8Authority{manifest: manifest, manifestSHA: conversationHash(manifestRaw), binarySHA: binaryMarker, authorizationPath: authPath, authorizationSHA: conversationHash(authorizationRaw), raw: authorizationRaw, binding: binding}
	if err = stage("start_campaign", func() error {
		var e error
		campaign, e = startWindowCampaign(prepared, windowV8CandidateBaseline(stopped.base))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	var exportMu sync.Mutex
	var otlpBodies [][]byte
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, e := io.ReadAll(req.Body)
		if e != nil {
			t.Error(e)
		}
		exportMu.Lock()
		otlpBodies = append(otlpBodies, raw)
		exportMu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	t.Cleanup(receiver.Close)
	observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "synthetic-window-pk", SecretKey: "synthetic-window-sk", HashKey: strings.Repeat("h", 32), Release: bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := observer.Shutdown(context.Background()); e != nil {
			t.Error(e)
		}
		exportMu.Lock()
		defer exportMu.Unlock()
		for i, raw := range otlpBodies {
			if e := os.WriteFile(filepath.Join(exportRoot, fmt.Sprintf("observer-%06d.pb", i+1)), raw, 0600); e != nil {
				t.Error(e)
			}
		}
	})
	var providerMu sync.Mutex
	modelCalls, jevCalls := 0, 0
	modelFake, jevFake := testWindowCollectorModel(&modelCalls), testWindowCollectorJEV(&jevCalls)
	newFake := func(provider string, fake http.RoundTripper, firstEOF bool) (*windowActualNoRedirectTransport, *atomic.Int64) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			copy := req.Clone(req.Context())
			endpoint, _, _ := windowProviderEndpoint(provider)
			copy.URL, _ = url.Parse(endpoint)
			providerMu.Lock()
			response, e := fake.RoundTrip(copy)
			providerMu.Unlock()
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			defer response.Body.Close()
			w.Header().Set("Content-Type", "application/json")
			io.Copy(w, response.Body)
		}))
		t.Cleanup(server.Close)
		dials := new(atomic.Int64)
		transport := &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			n := dials.Add(1)
			if firstEOF && n == 2 {
				client, peer := net.Pipe()
				go func() { defer peer.Close(); windowTLSProtocolReadClientHello(bufio.NewReader(peer)) }()
				return client, nil
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}}
		t.Cleanup(transport.CloseIdleConnections)
		return &windowActualNoRedirectTransport{transport}, dials
	}
	modelTransport, modelDials := newFake("model", modelFake, true)
	jevTransport, jevDials := newFake("jev", jevFake, false)
	auth := &windowAcquisitionAuthorization{campaign: campaign, source: source, frozenScopeSHA: scopeSHA, frozenExecutionSHA: executionSHA, actualExecutionSHA: executionSHA, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "synthetic-model-key", jevKey: "synthetic-jev-key", modelUpstream: modelTransport, jevUpstream: jevTransport, fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"}, privateReportRoot: reportRoot, evidenceKind: "local_stub"}
	operation, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	err = stage("acquire_window_001", func() error { var e error; report, e = runWindowAcquisition(operation, auth); return e })
	providerMu.Lock()
	write("native-counts.json", map[string]any{"model_successful_local_requests": modelCalls, "jev_successful_local_requests": jevCalls, "model_dials": modelDials.Load(), "jev_dials": jevDials.Load(), "configured_costs_not_supplier_billing": true})
	providerMu.Unlock()
	write("statistics.json", report.V8)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Err() != nil || report.Status != "collected_window" || !report.Advanced || !report.PayloadVerified || !report.Cleanup || !report.EnvironmentCleanup || !report.Observer || report.FinishedArms != 56 || campaign.index != 2 || report.NextClaimSHA256 != campaign.claimSHA {
		t.Fatal("genuine window did not complete within existing guards", report)
	}
	if report.V8 == nil || len(report.Accounting.UnknownIntents) != 1 || report.Accounting.ModelUnknownHeld != 74752 || report.Accounting.ModelExtraFunded != 74752 || report.Accounting.Pending != nil || report.Accounting.RetryRequired != nil || report.Accounting.Halt != "" || len(campaign.extras) != 1 {
		t.Fatal("unknown/extra/native2 completion linkage absent", report.Accounting)
	}
	audit, e := readWindowAccounting(filepath.Join(campaign.root, "ledger-000001"))
	if e != nil || !audit.State.Closed || audit.State.Halt != "" || audit.State.Pending != nil {
		t.Fatal("closed journal not retained", e)
	}
	write("journal-audit-data.json", audit)
	t.Log("genuine local_stub56arm receipt/checkpoint/nextclaim complete; no actual Session summary or real authority")
}

type windowV8ReadonlyFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}
type windowV8ReadonlyReceipt struct {
	Inventory6395 map[string]windowV8ReadonlyFile `json:"inventory6395"`
	Claims44      map[string]windowV8ReadonlyFile `json:"claims44"`
}

func readWindowV8FixtureReadonlyReceipt(stopped windowStoppedV7Baseline) (*windowV8ReadonlyReceipt, error) {
	raw, err := readWindowStoppedV7File(stopped.evidencePath, windowStoppedV7DescriptorSHA, 65536)
	if err != nil {
		return nil, err
	}
	var descriptor windowStoppedV7Evidence
	if err = decodeWindowActualJSON(raw, &descriptor); err != nil {
		return nil, err
	}
	raw, err = readWindowStoppedV7File(descriptor.Inventory.Path, windowStoppedV7InventorySHA, 8<<20)
	if err != nil {
		return nil, err
	}
	var inventory windowStoppedV7Inventory
	if err = decodeWindowActualJSON(raw, &inventory); err != nil {
		return nil, err
	}
	if len(inventory.Files) != 6395 || inventory.Root != stopped.originalRoot {
		return nil, fmt.Errorf("readonly6395 inventory changed")
	}
	out := &windowV8ReadonlyReceipt{Inventory6395: map[string]windowV8ReadonlyFile{}, Claims44: map[string]windowV8ReadonlyFile{}}
	names := make([]string, 0, len(inventory.Files))
	for name := range inventory.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("invalid readonly inventory name")
		}
		expected := inventory.Files[name]
		path := filepath.Join(stopped.originalRoot, name)
		body, e := readWindowStoppedV7File(path, expected.SHA256, expected.Bytes+1)
		if e != nil {
			return nil, e
		}
		if int64(len(body)) != expected.Bytes {
			return nil, fmt.Errorf("readonly byte mismatch")
		}
		out.Inventory6395[name] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	expectedBySHA := map[string]string{}
	for id, sha := range stopped.base.ClaimSHA256 {
		expectedBySHA[sha] = id
	}
	for id, sha := range fullWindowCanonicalClaims() {
		path := filepath.Join(stopped.historicalDir, id+".json")
		body, e := readWindowStoppedV7File(path, sha, 65536)
		if e != nil {
			return nil, e
		}
		out.Claims44[id] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	for _, root := range stopped.windowStoppedPredecessorRootsV8() {
		paths, e := filepath.Glob(filepath.Join(root, "campaign/claim-*.json"))
		if e != nil {
			return nil, e
		}
		for _, path := range paths {
			body, e := readWindowV8SignedBytes(path, 65536)
			if e != nil {
				return nil, e
			}
			sha := conversationHash(body)
			id, ok := expectedBySHA[sha]
			if !ok {
				return nil, fmt.Errorf("unexpected original claim")
			}
			if _, duplicate := out.Claims44[id]; duplicate {
				return nil, fmt.Errorf("duplicate original claim")
			}
			out.Claims44[id] = windowV8ReadonlyFile{path, sha, len(body)}
		}
	}
	if len(out.Claims44) != 44 {
		return nil, fmt.Errorf("readonly44 incomplete: %d", len(out.Claims44))
	}
	return out, nil
}
func copyWindowV8FixtureTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		dst := filepath.Join(target, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("synthetic export symlink refused")
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, 0700)
		}
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("synthetic export irregular file")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(dst, raw, 0600)
	})
}
func writeWindowV8FixtureInventory(root string) error {
	files := map[string]windowV8ReadonlyFile{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if rel == "export-inventory.json" {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		files[filepath.ToSlash(rel)] = windowV8ReadonlyFile{path, conversationHash(raw), len(raw)}
		return nil
	})
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"evidence_kind": "controlled_local_stub_export_not_authority", "files": files}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "export-inventory.json"), append(raw, '\n'), 0600)
}
