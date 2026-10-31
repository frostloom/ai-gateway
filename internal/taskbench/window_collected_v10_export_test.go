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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
)

// This opt-in material-export test follows genuine private V10 campaign and
// acquisition APIs. It is not actual readiness, an actual Session or human
// approval. Campaign state lives under a fresh t.TempDir; synthetic provenance
// copies live in the exclusive export root so copied material stays readable.
func TestWindowV10CollectedWindowFixtureExport(t *testing.T) {
	if os.Getenv("TASKBENCH_V10_COLLECTED_FIXTURE_EXPORT") != "1" {
		t.Skip("explicit synthetic full-window fixture export only")
	}
	repo := "/Users/a1/项目/ai-gateway"
	sourceRoot := os.Getenv("TASKBENCH_V10_COLLECTED_FIXTURE_SOURCE_ROOT")
	if !filepath.IsAbs(sourceRoot) || filepath.Clean(sourceRoot) != sourceRoot {
		t.Fatal("explicit canonical selected source archive required")
	}
	sourceCanonical, sourceErr := filepath.EvalSymlinks(sourceRoot)
	if sourceErr != nil || sourceCanonical != sourceRoot {
		t.Fatal("selected source archive alias", sourceErr)
	}
	exportRoot := os.Getenv("TASKBENCH_V10_COLLECTED_FIXTURE_EXPORT_ROOT")
	exportNamespace := filepath.Join(repo, ".tmp/v10-collected-local-stub-20261031")
	if !filepath.IsAbs(exportRoot) || filepath.Clean(exportRoot) != exportRoot || !strings.HasPrefix(exportRoot, exportNamespace+string(filepath.Separator)) {
		t.Fatal("explicit new canonical V10 local-stub export root required")
	}
	canonicalParent, parentErr := filepath.EvalSymlinks(filepath.Dir(exportRoot))
	if parentErr != nil || canonicalParent != filepath.Dir(exportRoot) {
		t.Fatal("canonical existing export parent required", parentErr)
	}
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
	sourcePinsPath := os.Getenv("TASKBENCH_V10_COLLECTED_FIXTURE_SOURCE_PINS")
	if !filepath.IsAbs(sourcePinsPath) || filepath.Clean(sourcePinsPath) != sourcePinsPath {
		t.Fatal("explicit candidate Go source pins required")
	}
	pinRaw, err := os.ReadFile(sourcePinsPath)
	if err != nil {
		t.Fatal(err)
	}
	if conversationHash(pinRaw) != os.Getenv("TASKBENCH_V10_COLLECTED_FIXTURE_SOURCE_PINS_SHA256") {
		t.Fatal("candidate Go pin input SHA mismatch")
	}
	protected := map[string]string{}
	if err = json.Unmarshal(pinRaw, &protected); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"internal/taskbench/window_collected_v10_export_test.go", "internal/taskbench/window_actual_v10.go", "internal/taskbench/window_campaign_v10.go", "internal/taskbench/window_scope_v10.go", "internal/taskbench/window_stopped_v9.go", "internal/taskbench/window_stopped_v9_bundle.go", "internal/taskbench/window_history_read_pass.go", "internal/agent/llm.go", "internal/observability/observer.go"} {
		if !comparisonDigest(protected[required]) {
			t.Fatal("current candidate protection incomplete", required)
		}
	}
	for name, digest := range protected {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || !strings.HasSuffix(name, ".go") || !comparisonDigest(digest) {
			t.Fatal("invalid candidate Go pin")
		}
	}
	verifySource := func() map[string]string {
		out := map[string]string{}
		for p, want := range protected {
			raw, e := readWindowStoppedV7File(filepath.Join(sourceRoot, p), want, 256<<20)
			if e != nil {
				t.Error(e)
				continue
			}
			out[p] = conversationHash(raw)
			if out[p] != want {
				t.Errorf("protected candidate Go source changed: %s", p)
			}
		}
		return out
	}
	if !windowPayloadDisjoint(sourceRoot, exportRoot) {
		t.Fatal("export root overlaps selected source")
	}
	verifyGoNamespace := func() error {
		found := map[string]bool{}
		err := filepath.WalkDir(sourceRoot, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("selected source alias %s", p)
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			rel, e := filepath.Rel(sourceRoot, p)
			if e != nil {
				return e
			}
			rel = filepath.ToSlash(rel)
			if _, ok := protected[rel]; !ok {
				return fmt.Errorf("unprotected selected Go source %s", rel)
			}
			found[rel] = true
			return nil
		})
		if err != nil {
			return err
		}
		if len(found) != len(protected) {
			return fmt.Errorf("selected Go source namespace differs from pins")
		}
		return nil
	}
	if err := verifyGoNamespace(); err != nil {
		t.Fatal(err)
	}
	beforeSource := verifySource()
	if t.Failed() {
		t.Fatal("candidate Go protection rejected before any fixture or provider")
	}
	if err = os.WriteFile(filepath.Join(exportRoot, "candidate-go-source-pins.json"), pinRaw, 0600); err != nil {
		t.Fatal(err)
	}
	write("protected-source-before.json", beforeSource)
	parent := stoppedEvidenceRoot(t)
	var campaign *windowCampaign
	var stopped windowStoppedV9Baseline
	var report windowAcquisitionReceipt
	var reportRoot string
	var oldBefore *windowV10ReadonlyReceipt
	historyLoaded, campaignStarted := false, false
	timings := map[string]float64{}
	phase := "initial"
	stage := func(name string, fn func() error) error {
		phase = name
		t.Log("stage begin", name)
		started := time.Now()
		e := fn()
		timings[name] = time.Since(started).Seconds()
		t.Logf("stage end %s %.6fs err=%v", name, timings[name], e)
		write("stages.json", map[string]any{"evidence_kind": "synthetic_controlled_export_not_actual_authority", "last_stage": name, "duration_seconds": timings, "last_error": fmt.Sprint(e), "actual_acquisition_limit_seconds": 300, "recommended_outer_test_limit_seconds": 900, "outer_test_timeout_source": "go_test_driver"})
		return e
	}
	// Registered before resource cleanups, so final copies see flushed/closed new
	// evidence. The real old files are read only and are never copied or altered.
	t.Cleanup(func() {
		if campaign != nil {
			if e := campaign.closeWindowCampaign(); e != nil {
				t.Error("controlled campaign close", e)
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
		for _, name := range []string{"synthetic-authorization.json", "synthetic-manifest.json", "scope.json"} {
			raw, e := os.ReadFile(filepath.Join(parent, name))
			if e == nil {
				if e = os.WriteFile(filepath.Join(exportRoot, name), raw, 0600); e != nil {
					t.Error(e)
				}
			}
		}
		oldUnchanged := false
		if oldBefore != nil {
			after, e := readWindowV10FixtureReadonlyReceipt(stopped)
			if e != nil {
				t.Error(e)
			} else {
				write("old-evidence-after.json", after)
				oldUnchanged = reflect.DeepEqual(oldBefore, after)
				if !oldUnchanged {
					t.Error("old102/9055 and selected historical archives changed")
				}
			}
		}
		namespaceAfterErr := verifyGoNamespace()
		if namespaceAfterErr != nil {
			t.Error(namespaceAfterErr)
		}
		afterSource := verifySource()
		write("protected-source-after.json", afterSource)
		testRaw, e := os.ReadFile(filepath.Join(sourceRoot, "internal/taskbench/window_collected_v10_export_test.go"))
		if e != nil {
			t.Error(e)
		}
		fresh := 0
		if historyLoaded {
			fresh++
		}
		if campaignStarted {
			fresh += 2
		}
		if report.Advanced {
			fresh++
		}
		write("export-result.json", map[string]any{"evidence_kind": "genuine_local_stub_owned_window_not_actual_ready", "last_stage": phase, "test_failed": t.Failed(), "receipt_status": report.Status, "advanced": report.Advanced, "next_claim_sha256": report.NextClaimSHA256, "actual_acquisition_limit_seconds": 300, "recommended_outer_test_limit_seconds": 900, "outer_test_timeout_source": "go_test_driver", "stage_duration_seconds": timings, "schema": "full1220-v10-controlled-export-result.v1", "old_stopped102_unchanged": oldUnchanged, "selected_source_unchanged": namespaceAfterErr == nil && len(afterSource) == len(protected) && reflect.DeepEqual(beforeSource, afterSource), "export_test_sha256": conversationHash(testRaw), "fresh_history_loads_completed_minimum": fresh, "fresh_history_count_basis": "one explicit loader; successful start requires prepared-history and first-claim fresh loads; successful next claim adds one. No production counter/cache was introduced.", "actual_session_summary": "not_created_or_claimed", "statistics_source": "production returned receipt.v10 with native runtime.v8 under scope.v10", "scope_schema": "full-window-scope.v10", "candidate_go_pin_input_sha256": conversationHash(pinRaw), "selected_go_protection_covers_whole_go_namespace": true, "selected_source_root": sourceRoot, "go_protection_is_not_actual_frozen_sourceproof": true, "controlled_manifest_sources_are_synthetic_marker": true, "controlled_manifest_source_root": "", "new_actual_model_calls": 0, "new_actual_jev_calls": 0, "new_financial_claims": 0, "source_temp_root": parent, "copied_evidence_is_not_live_owner_or_reopenable_authority": true})
		if e := writeWindowV8FixtureInventory(exportRoot); e != nil {
			t.Error(e)
		}
	})
	descriptor := filepath.Join(repo, ".tmp/v9-live-score-horizon-20261031/r5-stopped102-preparation-v2/stopped-r5.private.json")
	if err = stage("load_concrete102", func() error {
		var e error
		stopped, e = loadWindowStoppedV9BaselineContext(context.Background(), descriptor, windowV10BaselineSHA)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	historyLoaded = true
	if err = stage("old_evidence_before_sha", func() error {
		var e error
		oldBefore, e = readWindowV10FixtureReadonlyReceipt(stopped)
		if e == nil {
			write("old-evidence-before.json", oldBefore)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	source, err := LoadConversations(filepath.Join(sourceRoot, "eval/task-definitions/2026-10-12-full-dev-v1"))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadPairedProfiles(filepath.Join(sourceRoot, "eval/runtime-profiles/2026-10-13-six-profiles-v1"))
	if err != nil {
		t.Fatal(err)
	}
	bindings := windowV10OriginalBindings()
	bindings.PolicySHA256 = windowOwnedQualityPolicySHA()
	scope, err := prepareFullWindowScopeV10(source, profiles, windowV9TestCaps(t)[55:], bindings, windowV10BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Windows) != 456 || len(scope.Slots) != 3660 || scope.Windows[0].ID != "window-056" || scope.Windows[0].FirstOrdinal != 386 || scope.Windows[0].LastOrdinal != 390 || scope.Windows[455].ID != "window-511" || scope.Windows[455].LastOrdinal != 3660 {
		t.Fatal("fixed suffix456 with original window056/386..390 required")
	}
	if err := validateWindowV10ControlledFirstWindow(scope); err != nil {
		t.Fatal(err)
	}
	firstTurns := 0
	for _, slot := range scope.Slots[385:390] {
		firstTurns += 2 * slot.UserTurns
	}
	if firstTurns != 22 {
		t.Fatal("first V10 local window must retain22 both-arm defined turns")
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
		prepared, e = prepareWindowCampaignV10Technical(root, root, scope, source, profiles, bindings, scopeSHA, executionSHA, limits)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// Reuse only the retained provenance and fresh synthetic key. Bind and sign
	// this actual controlled scope/execution/manifest instead of the fixture's
	// placeholder digests. Provenance copies live at stable export paths.
	pub, key, keyErr := ed25519.GenerateKey(rand.Reader)
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	copyProvenance := func(ref windowActualFile, name string) windowActualFile {
		t.Helper()
		raw, e := readWindowStoppedV7File(ref.Path, ref.SHA256, 65536)
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(exportRoot, name)
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, we := f.Write(raw)
		ce := f.Close()
		if we != nil || ce != nil {
			t.Fatal(we, ce)
		}
		return windowActualFile{path, conversationHash(raw)}
	}
	budgetRef := copyProvenance(windowActualFile{filepath.Join(sourceRoot, "internal/taskbench/testdata/window_v9_budget_approval.json"), windowStoppedV8ApprovalSHA}, "synthetic-budget-provenance.json")
	autonomousRef := copyProvenance(windowActualFile{filepath.Join(sourceRoot, "internal/taskbench/testdata/window_v9_autonomous_authorization.json"), windowV9AutonomousAuthorizationSHA}, "synthetic-autonomous-provenance.json")
	diffRaw := []byte(`{"evidence_kind":"synthetic_marker_only_not_actual_source_equivalence"}`)
	diffPath := filepath.Join(exportRoot, "synthetic-executor-equivalence.json")
	if err = os.WriteFile(diffPath, diffRaw, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := windowActualManifest{Schema: "full-window-actual-ready.v10", EvidenceKind: "local_stub", Scope: scope, ScopeSHA256: scopeSHA, ExecutionSHA256: executionSHA, BuildAttestationPublicKey: hex.EncodeToString(pub), ExecutorEquivalence: &windowActualFile{diffPath, conversationHash(diffRaw)}, Sources: map[string]string{"synthetic-local-fixture-only.go": conversationHash([]byte("synthetic marker, not frozen actual source"))}, CampaignRoot: root, SessionRoot: reportRoot, BaselineEvidence: &windowActualFile{descriptor, windowV10BaselineSHA}, HistoricalClaims: stopped.base.ClaimSHA256, ClaimsDir: stopped.historicalDir, Limits: limits}
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
	binding := windowV10ExecutionBinding{windowV8ExecutionBinding: windowV8ExecutionBinding{Schema: "full-window-delegated-execution-authorization.v10", HumanAuthorized: true, RetryPolicyApproved: true, SingleOwner: true, NoRelaunch: true}, CompletionSHA256: windowV8DeclarationSHA(scope.Completion), ExecutorEquivalenceSHA256: manifest.ExecutorEquivalence.SHA256}
	binding.HumanReply, binding.AutonomousAuthorization = budgetRef, autonomousRef
	binding.Parent = windowV8Amounts{185000000, 3000000}
	binding.GlobalBudget = windowV8Amounts{200000000, 4000000}
	binding.HumanReviewedExactScope = false
	binding.ManifestSHA256, binding.SourceSHA256, binding.BinarySHA256 = conversationHash(manifestRaw), conversationHash(sourceRaw), binaryMarker
	binding.ScopeSHA256, binding.ExecutionSHA256 = scopeSHA, executionSHA
	binding.BudgetDeclarationSHA256, binding.RetryPolicySHA256 = scope.BudgetDeclarationSHA256, scope.RetryPolicySHA256
	binding.OwnerID = "synthetic-controlled-v10-window-fixture"
	canonical, _ := json.Marshal(binding)
	authorizationRaw, _ := json.Marshal(windowV10ExecutionAuthorization{binding, hex.EncodeToString(ed25519.Sign(key, canonical))})
	authPath := filepath.Join(parent, "synthetic-authorization.json")
	if err = os.WriteFile(authPath, authorizationRaw, 0600); err != nil {
		t.Fatal(err)
	}
	authority := &windowV10Authority{manifest: manifest, manifestSHA: conversationHash(manifestRaw), binarySHA: binaryMarker, authorizationPath: authPath, authorizationSHA: conversationHash(authorizationRaw), raw: authorizationRaw, binding: binding}
	if err = authority.revalidate(); err != nil {
		t.Fatal("re-signed synthetic V10 binding rejected", err)
	}
	prepared.stoppedV9 = &stopped
	prepared.v8Authority = authority.nativeView()
	if err = stage("start_campaign", func() error {
		var e error
		campaign, e = startWindowCampaign(prepared, windowV10CandidateBaseline(stopped.base))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	campaignStarted = true
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
				done := make(chan struct{})
				go func() { defer close(done); defer peer.Close(); windowTLSProtocolReadClientHello(bufio.NewReader(peer)) }()
				t.Cleanup(func() {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("synthetic EOF peer did not join")
					}
				})
				return client, nil
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}}
		t.Cleanup(transport.CloseIdleConnections)
		return &windowActualNoRedirectTransport{transport}, dials
	}
	modelTransport, modelDials := newFake("model", modelFake, true)
	jevTransport, jevDials := newFake("jev", jevFake, false)
	auth := &windowAcquisitionAuthorization{campaign: campaign, source: source, frozenScopeSHA: scopeSHA, frozenExecutionSHA: executionSHA, actualExecutionSHA: executionSHA, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "synthetic-model-key", jevKey: "synthetic-jev-key", modelUpstream: modelTransport, jevUpstream: jevTransport, fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: filepath.Join(sourceRoot, "eval/runtime-profiles/2026-10-13-six-profiles-v1")}, privateReportRoot: reportRoot, evidenceKind: "local_stub"}
	operation, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	err = stage("acquire_local001_declared056", func() error { var e error; report, e = runWindowAcquisition(operation, auth); return e })
	providerMu.Lock()
	write("native-counts.json", map[string]any{"model_successful_local_requests": modelCalls, "jev_successful_local_requests": jevCalls, "model_dials": modelDials.Load(), "jev_dials": jevDials.Load(), "configured_costs_not_supplier_billing": true})
	providerMu.Unlock()
	write("statistics.json", report.V8)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Err() != nil || report.Status != "collected_window" || !report.Advanced || !report.PayloadVerified || !report.Cleanup || !report.EnvironmentCleanup || !report.Observer || report.Schema != "full-window-acquisition-receipt.v10" || report.WindowIndex != 1 || report.WindowID != "window-056" || report.AttemptedArms != 10 || report.FinishedArms != 10 || report.ObservedTurns != 22 || report.PlannedCurrentPairs != 5 || report.PlannedCurrentArms != 10 || report.PlannedCurrentTurns != 22 || report.PlannedCampaignPairs != 3275 || report.PlannedCampaignArms != 6550 || report.PlannedCampaignTurns != 10504 || !reflect.DeepEqual(report.Completion, scope.Completion) || campaign.index != 2 || report.NextClaimSHA256 != campaign.claimSHA {
		t.Fatal("genuine window did not complete within existing guards", report)
	}
	if report.V8 == nil || report.V8.ModelRetryIntents != 1 || report.V8.JEVRetryIntents != 0 || report.V8.NativeUnknown != 1 || report.V8.NativePending != 0 || len(report.Accounting.UnknownIntents) != 1 || report.Accounting.ModelUnknownHeld != 74752 || report.Accounting.ModelExtraFunded != 74752 || report.Accounting.Pending != nil || report.Accounting.RetryRequired != nil || report.Accounting.Halt != "" || len(campaign.extras) != 1 {
		t.Fatal("unknown/extra/native2 completion linkage absent", report.Accounting)
	}
	audit, e := readWindowAccounting(filepath.Join(campaign.root, "ledger-000001"))
	if e != nil || !audit.State.Closed || audit.State.Halt != "" || audit.State.Pending != nil {
		t.Fatal("closed journal not retained", e)
	}
	write("journal-audit-data.json", audit)
	t.Log("genuine local_stub V10 local1/declared056 10arm/22turn receipt/checkpoint/next synthetic claim complete; no actual Session summary or real authority")
}

// Physical before/after receipts cover all historical102 claims, the original
// 9055 campaign/records files and original2315 source archive. This is read-only
// protection DATA, not a source-proof or normal-window capability.
type windowV10ReadonlyReceipt struct {
	Previous46   *windowV9ReadonlyReceipt        `json:"previous46"`
	Original9055 map[string]windowV8ReadonlyFile `json:"original9055"`
	Archive2315  map[string]windowV8ReadonlyFile `json:"archive2315"`
	Claims102    map[string]windowV8ReadonlyFile `json:"claims102"`
	References   map[string]windowV8ReadonlyFile `json:"references"`
}

func readWindowV10FixtureReadonlyReceipt(stopped windowStoppedV9Baseline) (*windowV10ReadonlyReceipt, error) {
	prior, err := readWindowV9FixtureReadonlyReceipt(stopped.previous)
	if err != nil {
		return nil, err
	}
	out := &windowV10ReadonlyReceipt{Previous46: prior, Original9055: map[string]windowV8ReadonlyFile{}, Archive2315: map[string]windowV8ReadonlyFile{}, Claims102: map[string]windowV8ReadonlyFile{}, References: map[string]windowV8ReadonlyFile{}}
	for id, entry := range prior.Claims46 {
		out.Claims102[id] = entry
	}
	raw, err := readWindowStoppedV7File(stopped.evidencePath, windowStoppedV9DescriptorSHA, 65536)
	var e windowStoppedV9Evidence
	if err != nil || decodeWindowActualJSON(raw, &e) != nil || validateWindowStoppedV9Descriptor(e) != nil || e.OriginalRoot != stopped.originalRoot {
		return nil, fmt.Errorf("readonly102 descriptor mismatch: %v", err)
	}
	out.References["descriptor"] = windowV8ReadonlyFile{stopped.evidencePath, conversationHash(raw), len(raw)}
	for name, ref := range map[string]windowActualFile{"manifest": e.Manifest, "inventory": e.Inventory, "budget_approval": e.BudgetApproval, "stopped_audit": e.StoppedAudit, "source_confirmation": e.SourceConfirmation, "owner_exit": e.OwnerExit} {
		body, err := readWindowStoppedV7File(ref.Path, ref.SHA256, 8<<20)
		if err != nil {
			return nil, err
		}
		out.References[name] = windowV8ReadonlyFile{ref.Path, conversationHash(body), len(body)}
	}
	invRaw, err := readWindowStoppedV7File(e.Inventory.Path, e.Inventory.SHA256, 8<<20)
	var inv windowStoppedV8Inventory
	if err != nil || decodeWindowActualJSON(invRaw, &inv) != nil || inv.Schema != "full1220-v9-stopped-original-inventory.v1" || inv.Root != stopped.originalRoot || inv.Count != 9055 || len(inv.Files) != 9055 || inv.Bytes != 47257122 || !inv.Closed || !inv.NoResume {
		return nil, fmt.Errorf("readonly9055 inventory mismatch")
	}
	var total int64
	for name, entry := range inv.Files {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || !(strings.HasPrefix(name, "campaign/") || strings.HasPrefix(name, "records/")) || entry.Bytes < 0 || entry.Bytes > 8<<20 {
			return nil, fmt.Errorf("readonly9055 entry bounds")
		}
		path := filepath.Join(stopped.originalRoot, filepath.FromSlash(name))
		body, err := readWindowStoppedV7File(path, entry.SHA256, entry.Bytes+1)
		if err != nil {
			return nil, err
		}
		if int64(len(body)) != entry.Bytes {
			return nil, fmt.Errorf("readonly9055 length")
		}
		total += entry.Bytes
		out.Original9055[name] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	if total != inv.Bytes {
		return nil, fmt.Errorf("readonly9055 total")
	}
	seenNames := 0
	for _, dir := range []string{"campaign", "records"} {
		if err := filepath.WalkDir(filepath.Join(stopped.originalRoot, dir), func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("readonly9055 namespace alias")
			}
			if d.IsDir() {
				return nil
			}
			name, e := filepath.Rel(stopped.originalRoot, p)
			if e != nil {
				return e
			}
			name = filepath.ToSlash(name)
			if _, ok := inv.Files[name]; !ok {
				return fmt.Errorf("readonly9055 unexpected file %s", name)
			}
			seenNames++
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if seenNames != 9055 {
		return nil, fmt.Errorf("readonly9055 namespace count")
	}

	manifestRaw, err := readWindowStoppedV7File(e.Manifest.Path, windowStoppedV9ManifestSHA, 8<<20)
	var m windowActualManifest
	if err != nil || decodeWindowActualJSON(manifestRaw, &m) != nil || validateWindowStoppedV9Manifest(m, e, stopped.previous) != nil {
		return nil, fmt.Errorf("readonly original R5 manifest")
	}
	sources, _ := json.Marshal(m.Sources)
	if len(m.Sources) != 2315 || conversationHash(sources) != "47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00" {
		return nil, fmt.Errorf("readonly2315 source map")
	}
	for name, digest := range m.Sources {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name {
			return nil, fmt.Errorf("readonly2315 path")
		}
		path := filepath.Join(m.SourceRoot, filepath.FromSlash(name))
		body, err := readWindowStoppedV7File(path, digest, 256<<20)
		if err != nil {
			return nil, err
		}
		out.Archive2315[name] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	for i, digest := range windowStoppedV9ClaimPins() {
		id := fmt.Sprintf("full1220-v9-r5-20261031/window-%06d", i+1)
		path := filepath.Join(stopped.originalRoot, "campaign", fmt.Sprintf("claim-%06d.json", i+1))
		body, err := readWindowStoppedV7File(path, digest, 65536)
		if err != nil {
			return nil, err
		}
		out.Claims102[id] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	if len(out.Claims102) != 102 {
		return nil, fmt.Errorf("readonly102 count")
	}
	for id, digest := range stopped.base.ClaimSHA256 {
		if entry, ok := out.Claims102[id]; !ok || entry.SHA256 != digest {
			return nil, fmt.Errorf("readonly102 claim mismatch")
		}
	}
	if _, err := os.Lstat(filepath.Join(stopped.originalRoot, "campaign/claim-000057.json")); !os.IsNotExist(err) {
		return nil, fmt.Errorf("old R5 claim57 must remain absent")
	}
	return out, nil
}

func validateWindowV10ControlledFirstWindow(scope fullWindowScope) error {
	totals, err := executionTotals(scope)
	expected := expectedWindowCompletionDeclarationV10()
	if err != nil || totals.Pairs != 3275 || totals.Arms != 6550 || totals.DefinedUserTurns != 10504 || totals.Boundaries != 0 || !reflect.DeepEqual(scope.Completion, &expected) {
		return fmt.Errorf("controlled V10 suffix totals/completion mismatch")
	}
	first, err := executionWindowAt(scope, 1)
	if err != nil || first.ID != "window-056" || first.FirstOrdinal != 386 || first.LastOrdinal != 390 {
		return fmt.Errorf("controlled V10 first local declaration mismatch")
	}
	turns := 0
	for _, slot := range scope.Slots[first.FirstOrdinal-1 : first.LastOrdinal] {
		turns += 2 * slot.UserTurns
	}
	if turns != 22 {
		return fmt.Errorf("controlled V10 first definition turns")
	}
	return nil
}
func TestWindowV10ControlledExportDeclaration(t *testing.T) {
	scope, _, _, _ := windowV10FixtureScope(t)
	if err := validateWindowV10ControlledFirstWindow(scope); err != nil {
		t.Fatal(err)
	}
	scope.Windows = append([]fullWindowDeclaration(nil), scope.Windows...)
	scope.Windows[0].FirstOrdinal = 1
	if validateWindowV10ControlledFirstWindow(scope) == nil {
		t.Fatal("prefix mapping admitted by controlled exporter")
	}
}
