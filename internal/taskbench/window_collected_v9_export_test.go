package taskbench

import (
	"bufio"
	"context"
	"crypto/ed25519"
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

// This opt-in material-export test follows genuine private V9 campaign and
// acquisition APIs. It is not actual readiness, an actual Session or human
// approval. Campaign state lives under a fresh t.TempDir; synthetic provenance
// copies live in the exclusive export root so copied material stays readable.
func TestWindowV9CollectedWindowFixtureExport(t *testing.T) {
	if os.Getenv("TASKBENCH_V9_COLLECTED_FIXTURE_EXPORT") != "1" {
		t.Skip("explicit synthetic full-window fixture export only")
	}
	repo := "/Users/a1/项目/ai-gateway"
	exportRoot := os.Getenv("TASKBENCH_V9_COLLECTED_FIXTURE_EXPORT_ROOT")
	exportNamespace := filepath.Join(repo, ".tmp/v9-collected-local-stub-20261030")
	if !filepath.IsAbs(exportRoot) || filepath.Clean(exportRoot) != exportRoot || !strings.HasPrefix(exportRoot, exportNamespace+string(filepath.Separator)) {
		t.Fatal("explicit new canonical V9 local-stub export root required")
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
	sourcePinsPath := os.Getenv("TASKBENCH_V9_COLLECTED_FIXTURE_SOURCE_PINS")
	if !filepath.IsAbs(sourcePinsPath) || filepath.Clean(sourcePinsPath) != sourcePinsPath {
		t.Fatal("explicit candidate Go source pins required")
	}
	pinRaw, err := os.ReadFile(sourcePinsPath)
	if err != nil {
		t.Fatal(err)
	}
	if conversationHash(pinRaw) != os.Getenv("TASKBENCH_V9_COLLECTED_FIXTURE_SOURCE_PINS_SHA256") {
		t.Fatal("candidate Go pin input SHA mismatch")
	}
	protected := map[string]string{}
	if err = json.Unmarshal(pinRaw, &protected); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"internal/taskbench/window_collected_v9_export_test.go", "internal/taskbench/window_actual_v9.go", "internal/taskbench/window_campaign_v9.go", "internal/taskbench/window_scope_v9.go", "internal/taskbench/window_stopped_v8.go", "internal/taskbench/window_history_read_pass.go", "internal/agent/llm.go", "internal/observability/observer.go"} {
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
			raw, e := os.ReadFile(filepath.Join(repo, p))
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
	var stopped windowStoppedV8Baseline
	var report windowAcquisitionReceipt
	var reportRoot string
	var oldBefore *windowV9ReadonlyReceipt
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
		write("stages.json", map[string]any{"evidence_kind": "synthetic_controlled_export_not_actual_authority", "last_stage": name, "duration_seconds": timings, "last_error": fmt.Sprint(e), "actual_acquisition_limit_seconds": 300, "outer_test_limit_seconds": 600})
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
			after, e := readWindowV9FixtureReadonlyReceipt(stopped)
			if e != nil {
				t.Error(e)
			} else {
				write("old-evidence-after.json", after)
				oldUnchanged = reflect.DeepEqual(oldBefore, after)
				if !oldUnchanged {
					t.Error("old46/451 and archive2253 evidence changed")
				}
			}
		}
		afterSource := verifySource()
		write("protected-source-after.json", afterSource)
		testRaw, e := os.ReadFile(filepath.Join(repo, "internal/taskbench/window_collected_v9_export_test.go"))
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
		write("export-result.json", map[string]any{"evidence_kind": "genuine_local_stub_owned_window_not_actual_ready", "last_stage": phase, "test_failed": t.Failed(), "receipt_status": report.Status, "advanced": report.Advanced, "next_claim_sha256": report.NextClaimSHA256, "actual_acquisition_limit_seconds": 300, "outer_test_limit_seconds": 600, "stage_duration_seconds": timings, "old46_451_AA2253_and_prior6395_unchanged": oldUnchanged, "candidate_go_sources_unchanged": reflect.DeepEqual(beforeSource, afterSource), "export_test_sha256": conversationHash(testRaw), "fresh_history_loads_completed_minimum": fresh, "fresh_history_count_basis": "one explicit loader; successful start requires prepared-history and first-claim fresh loads; successful next claim adds one. No production counter/cache was introduced.", "actual_session_summary": "not_created_or_claimed", "statistics_source": "production returned receipt.v8 accounting under scope.v9", "scope_schema": "full-window-scope.v9", "candidate_go_pin_input_sha256": conversationHash(pinRaw), "candidate_go_protection_is_not_full_archive_sources": true, "controlled_manifest_sources_are_synthetic_marker": true, "controlled_manifest_source_root": "", "new_actual_model_calls": 0, "new_actual_jev_calls": 0, "new_financial_claims": 0, "source_temp_root": parent, "copied_evidence_is_not_live_owner_or_reopenable_authority": true})
		if e := writeWindowV8FixtureInventory(exportRoot); e != nil {
			t.Error(e)
		}
	})
	descriptor := filepath.Join(repo, ".tmp/v9-stopped46-baseline-20261030/stopped-v8.private.json")
	if err = stage("load_concrete46", func() error {
		var e error
		stopped, e = loadWindowStoppedV8Baseline(descriptor, windowV9BaselineSHA)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	historyLoaded = true
	if err = stage("old_evidence_before_sha", func() error {
		var e error
		oldBefore, e = readWindowV9FixtureReadonlyReceipt(stopped)
		if e == nil {
			write("old-evidence-before.json", oldBefore)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	source, profiles, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = windowOwnedQualityPolicySHA()
	scope, err := prepareFullWindowScopeV9(source, profiles, windowV9TestCaps(t), bindings, windowV9BaselineSHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Windows) != 511 || len(scope.Slots) != 3660 || scope.Windows[0].FirstOrdinal != 1 || scope.Windows[0].LastOrdinal != 12 || scope.Windows[0].MaxModelCalls != 120 || scope.Windows[0].MaxJEVCalls != 12 {
		t.Fatal("fixed complete V9 and first12 pair window required")
	}
	firstTurns := 0
	for _, slot := range scope.Slots[:12] {
		firstTurns += 2 * slot.UserTurns
	}
	if firstTurns != 24 {
		t.Fatal("first V9 window must retain24 both-arm defined turns")
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
		prepared, e = prepareWindowCampaignV9Technical(root, root, scope, source, profiles, bindings, scopeSHA, executionSHA, limits)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// Reuse only the retained provenance and fresh synthetic key. Bind and sign
	// this actual controlled scope/execution/manifest instead of the fixture's
	// placeholder digests. Provenance copies live at stable export paths.
	authority, key := fixtureWindowV9Authority(t)
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
	budgetRef := copyProvenance(authority.binding.HumanReply, "synthetic-budget-provenance.json")
	autonomousRef := copyProvenance(authority.binding.AutonomousAuthorization, "synthetic-autonomous-provenance.json")
	manifest := windowActualManifest{Schema: "full-window-actual-ready.v9", EvidenceKind: "local_stub", Scope: scope, ScopeSHA256: scopeSHA, ExecutionSHA256: executionSHA, BuildAttestationPublicKey: authority.manifest.BuildAttestationPublicKey, Sources: map[string]string{"synthetic-local-fixture-only.go": conversationHash([]byte("synthetic marker, not frozen actual source"))}, CampaignRoot: root, SessionRoot: reportRoot, BaselineEvidence: &windowActualFile{descriptor, windowV9BaselineSHA}, HistoricalClaims: stopped.base.ClaimSHA256, ClaimsDir: stopped.historicalDir, Limits: limits}
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
	binding := authority.binding
	binding.HumanReply, binding.AutonomousAuthorization = budgetRef, autonomousRef
	binding.Parent = windowV8Amounts{180000000, 2500000}
	binding.GlobalBudget = windowV8Amounts{200000000, 4000000}
	binding.HumanReviewedExactScope = false
	binding.ManifestSHA256, binding.SourceSHA256, binding.BinarySHA256 = conversationHash(manifestRaw), conversationHash(sourceRaw), binaryMarker
	binding.ScopeSHA256, binding.ExecutionSHA256 = scopeSHA, executionSHA
	binding.BudgetDeclarationSHA256, binding.RetryPolicySHA256 = scope.BudgetDeclarationSHA256, scope.RetryPolicySHA256
	binding.OwnerID = "synthetic-controlled-v9-window-fixture"
	canonical, _ := json.Marshal(binding)
	authorizationRaw, _ := json.Marshal(windowV9ExecutionAuthorization{binding, hex.EncodeToString(ed25519.Sign(key, canonical))})
	authPath := filepath.Join(parent, "synthetic-authorization.json")
	if err = os.WriteFile(authPath, authorizationRaw, 0600); err != nil {
		t.Fatal(err)
	}
	authority = &windowV9Authority{manifest: manifest, manifestSHA: conversationHash(manifestRaw), binarySHA: binaryMarker, authorizationPath: authPath, authorizationSHA: conversationHash(authorizationRaw), raw: authorizationRaw, binding: binding}
	if err = authority.revalidate(); err != nil {
		t.Fatal("re-signed synthetic V9 binding rejected", err)
	}
	prepared.stoppedHistory = stopped
	prepared.v8Authority = authority.nativeView()
	if err = stage("start_campaign", func() error {
		var e error
		campaign, e = startWindowCampaign(prepared, windowV9CandidateBaseline(stopped.base))
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
	if operation.Err() != nil || report.Status != "collected_window" || !report.Advanced || !report.PayloadVerified || !report.Cleanup || !report.EnvironmentCleanup || !report.Observer || report.AttemptedArms != 24 || report.FinishedArms != 24 || report.ObservedTurns != 24 || report.PlannedCurrentPairs != 12 || report.PlannedCurrentArms != 24 || report.PlannedCurrentTurns != 24 || report.PlannedCampaignPairs != 3660 || report.PlannedCampaignArms != 7320 || report.PlannedCampaignTurns != 11772 || campaign.index != 2 || report.NextClaimSHA256 != campaign.claimSHA {
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
	t.Log("genuine local_stub V9 24arm/24turn receipt/checkpoint/next synthetic claim complete; no actual Session summary or real authority")
}

// This receipt separates immutable original data and the AA archive from the
// current candidate Go protection map and the local_stub manifest marker.
type windowV9ReadonlyReceipt struct {
	Prior44     *windowV8ReadonlyReceipt        `json:"prior44_and6395"`
	Original451 map[string]windowV8ReadonlyFile `json:"original451"`
	Archive2253 map[string]windowV8ReadonlyFile `json:"archive2253"`
	Claims46    map[string]windowV8ReadonlyFile `json:"claims46"`
}

func readWindowV9FixtureReadonlyReceipt(stopped windowStoppedV8Baseline) (*windowV9ReadonlyReceipt, error) {
	prior, err := readWindowV8FixtureReadonlyReceipt(stopped.previous)
	if err != nil {
		return nil, err
	}
	out := &windowV9ReadonlyReceipt{Prior44: prior, Original451: map[string]windowV8ReadonlyFile{}, Archive2253: map[string]windowV8ReadonlyFile{}, Claims46: map[string]windowV8ReadonlyFile{}}
	for id, entry := range prior.Claims44 {
		out.Claims46[id] = entry
	}
	descriptorRaw, err := readWindowStoppedV7File(stopped.evidencePath, windowV9BaselineSHA, 65536)
	if err != nil {
		return nil, err
	}
	var descriptor windowStoppedV8Evidence
	if decodeWindowActualJSON(descriptorRaw, &descriptor) != nil || validateWindowStoppedV8Descriptor(descriptor) != nil || descriptor.OriginalRoot != stopped.originalRoot {
		return nil, fmt.Errorf("readonly46 descriptor mismatch")
	}
	invRaw, err := readWindowStoppedV7File(descriptor.Inventory.Path, windowStoppedV8InventorySHA, 8<<20)
	if err != nil {
		return nil, err
	}
	var inventory windowStoppedV8Inventory
	if decodeWindowActualJSON(invRaw, &inventory) != nil || len(inventory.Files) != 451 || inventory.Count != 451 || inventory.Bytes != 2283033 || inventory.Root != stopped.originalRoot || !inventory.Closed || !inventory.NoResume {
		return nil, fmt.Errorf("readonly451 inventory mismatch")
	}
	var total int64
	for name, entry := range inventory.Files {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || entry.Bytes < 0 || entry.Bytes >= 256<<20 {
			return nil, fmt.Errorf("readonly inventory path/size")
		}
		path := filepath.Join(stopped.originalRoot, filepath.FromSlash(name))
		body, e := readWindowStoppedV7File(path, entry.SHA256, entry.Bytes+1)
		if e != nil {
			return nil, e
		}
		if int64(len(body)) != entry.Bytes {
			return nil, fmt.Errorf("readonly451 size")
		}
		total += entry.Bytes
		out.Original451[name] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	if total != 2283033 {
		return nil, fmt.Errorf("readonly451 total")
	}
	manifestRaw, err := readWindowStoppedV7File(descriptor.Manifest.Path, windowStoppedV8ManifestSHA, 8<<20)
	if err != nil {
		return nil, err
	}
	var manifest windowActualManifest
	if decodeWindowActualJSON(manifestRaw, &manifest) != nil || manifest.ArchiveCommit != "aa9147b90406559b9ea9181f9db75f166dd39fce" || manifest.SourceRoot != filepath.Join(stopped.originalRoot, "regression/source") || len(manifest.Sources) != 2253 {
		return nil, fmt.Errorf("readonly AA source map")
	}
	for name, digest := range manifest.Sources {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name {
			return nil, fmt.Errorf("readonly AA source path")
		}
		path := filepath.Join(manifest.SourceRoot, filepath.FromSlash(name))
		body, e := readWindowStoppedV7File(path, digest, 256<<20)
		if e != nil {
			return nil, e
		}
		out.Archive2253[name] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	for i, digest := range windowStoppedV8ClaimPins() {
		id := fmt.Sprintf("full1220-v8-20261030/window-%06d", i+1)
		path := filepath.Join(stopped.originalRoot, "campaign", fmt.Sprintf("claim-%06d.json", i+1))
		body, e := readWindowStoppedV7File(path, digest, 65536)
		if e != nil {
			return nil, e
		}
		out.Claims46[id] = windowV8ReadonlyFile{path, conversationHash(body), len(body)}
	}
	if len(out.Claims46) != 46 {
		return nil, fmt.Errorf("readonly46 count")
	}
	for id, digest := range stopped.base.ClaimSHA256 {
		if entry, ok := out.Claims46[id]; !ok || entry.SHA256 != digest {
			return nil, fmt.Errorf("readonly46 claim mismatch")
		}
	}
	return out, nil
}
