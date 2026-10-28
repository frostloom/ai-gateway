package taskbench

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/observability"
)

func testWindowCollectorAuthorization(t *testing.T, widths ...int) (*windowAcquisitionAuthorization, *int, *int) {
	return testWindowCollectorAuthorizationPolicy(t, collectionPolicySHA(), widths...)
}
func testWindowCollectorAuthorizationPolicy(t *testing.T, policy string, widths ...int) (*windowAcquisitionAuthorization, *int, *int) {
	return testWindowCollectorAuthorizationVersion(t, policy, false, widths...)
}
func testWindowCollectorAuthorizationVersion(t *testing.T, policy string, v2 bool, widths ...int) (*windowAcquisitionAuthorization, *int, *int) {
	t.Helper()
	source, profiles, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = policy
	width := 1
	if len(widths) > 0 {
		width = widths[0]
	}
	caps := make([]fullWindowCaps, (3660+width-1)/width)
	for i := range caps {
		caps[i] = fullWindowCaps{int64(10 * width), int64(width)}
	}
	var scope fullWindowScope
	var e error
	if v2 {
		scope, e = prepareFullWindowScopeV2(source, profiles, width, caps, bindings, strings.Repeat("8", 64))
	} else {
		scope, e = prepareFullWindowScope(source, profiles, width, caps, bindings)
	}
	if e != nil {
		t.Fatal(e)
	}
	scopeSHA, _, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		t.Fatal(e)
	}
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "campaign")
	limits := windowCampaignLimits{Model: windowCampaignProviderBounds{Model: "deepseek-flash", MaxRequestBytes: 1 << 20, MaxInput: 10000, MaxOutput: 128, InputRate: 1, OutputRate: 1}, JEV: windowCampaignProviderBounds{Model: "jev-1.13.0", MaxRequestBytes: 1 << 20, MaxInput: 10000, MaxOutput: 128, InputRate: 1}}
	execution, e := executionSHAWindowCampaign(scopeSHA, root, limits)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := prepareWindowCampaign(root, root, scope, source, profiles, bindings, scopeSHA, execution, limits)
	if e != nil {
		t.Fatal(e)
	}
	_, baseline := fixtureWindowCampaign(t)
	if v2 {
		baseline = expectedStoppedWindowBaseline()
		prepared.stoppedHistory = &fixtureStoppedHistory{base: baseline, sha: scope.BaselineEvidenceSHA256, root: filepath.Join(parent, "old-stopped")}
	}
	campaign, e := startWindowCampaign(prepared, baseline)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { campaign.closeWindowCampaign() })
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	t.Cleanup(receiver.Close)
	observer, e := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-collector", SecretKey: "sk-collector", HashKey: strings.Repeat("h", 32), Release: bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { observer.Shutdown(context.Background()) })
	model, judge := new(int), new(int)
	auth := &windowAcquisitionAuthorization{campaign: campaign, source: source, frozenScopeSHA: scopeSHA, frozenExecutionSHA: execution, actualExecutionSHA: execution, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "synthetic-model-key", jevKey: "synthetic-jev-key", modelUpstream: testWindowCollectorModel(model), jevUpstream: testWindowCollectorJEV(judge), fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"}, privateReportRoot: filepath.Join(parent, "records"), evidenceKind: "local_stub"}
	return auth, model, judge
}
func testWindowCollectorModel(calls *int) http.RoundTripper {
	return windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		*calls++
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			return nil, e
		}
		var request struct {
			Messages []struct {
				Content []struct{ Type, Content string }
			}
		}
		if json.Unmarshal(raw, &request) != nil {
			return nil, fmt.Errorf("closed request")
		}
		text := ""
		if len(request.Messages) > 0 {
			for _, block := range request.Messages[len(request.Messages)-1].Content {
				if block.Type == "tool_result" {
					text = block.Content
				}
			}
		}
		var content []any
		stop := "tool_use"
		if text == "" {
			content = []any{map[string]any{"type": "tool_use", "id": "collector-read", "name": "get_balance", "input": map[string]any{}}}
		} else {
			stop = "end_turn"
			content = []any{map[string]any{"type": "text", "text": text}}
		}
		body, _ := json.Marshal(map[string]any{"model": "deepseek-flash", "content": content, "stop_reason": stop, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
}
func testWindowCollectorJEV(calls *int) http.RoundTripper {
	return windowTransportRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		*calls++
		raw, _ := io.ReadAll(r.Body)
		var request struct {
			Questions map[string]struct {
				Type     string
				Criteria any
			}
		}
		if json.Unmarshal(raw, &request) != nil {
			return nil, fmt.Errorf("closed JEV request")
		}
		answers := map[string]any{}
		for name, q := range request.Questions {
			switch q.Type {
			case "noul":
				answers[name] = map[string]any{"type": "noul", "noul": .99}
			case "choice":
				choice := "get_balance"
				if name == "route_shape" {
					choice = "complex"
				}
				answers[name] = map[string]any{"type": "choice", "choice": choice, "confidence": .1, "probabilities": map[string]float64{choice: .1}}
			case "score":
				criteria := q.Criteria.([]any)
				answers[name] = map[string]any{"type": "score", "score": 0, "confidence": .1, "legend": map[string]any{"0": criteria[0]}, "probabilities": map[string]float64{"0": .1}}
			}
		}
		body, _ := json.Marshal(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
}
func TestWindowCollectorInvalidPreflightHasNoExecutionWrites(t *testing.T) {
	for _, kind := range []string{"nil", "scope", "execution", "source", "observer", "policy", "root", "key", "fixture"} {
		t.Run(kind, func(t *testing.T) {
			auth, model, judge := testWindowCollectorAuthorization(t)
			campaign := auth.campaign
			reportRoot := auth.privateReportRoot
			switch kind {
			case "nil":
				auth = nil
			case "scope":
				auth.frozenScopeSHA = strings.Repeat("0", 64)
			case "execution":
				auth.actualExecutionSHA = strings.Repeat("0", 64)
			case "source":
				auth.source.Cases[0].Scenario.Turns[0].User += "changed"
			case "observer":
				auth.observerIdentity.Release = "different"
			case "policy":
				auth.campaign.prepared.scope.Bindings.PolicySHA256 = strings.Repeat("0", 64)
			case "root":
				auth.privateReportRoot = filepath.Join(campaign.root, "records")
			case "key":
				auth.modelKey = ""
			case "fixture":
				auth.fixtureOptions.AllowFixtureCreation = false
			}
			if _, e := runWindowAcquisition(context.Background(), auth); e == nil {
				t.Fatal("invalid authorization accepted")
			}
			if *model != 0 || *judge != 0 || campaign.ledger != nil {
				t.Fatal("invalid authorization reached execution")
			}
			if _, e := os.Stat(reportRoot); !os.IsNotExist(e) {
				t.Fatal("invalid authorization created output", e)
			}
		})
	}
}
func TestWindowCollectorActualSingleWindowQualityNegativeAndNextClaim(t *testing.T) {
	auth, model, judge := testWindowCollectorAuthorization(t)
	originalJEV := auth.jevUpstream
	auth.jevUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, "arm-000001-model.json"))
		if e != nil {
			t.Error("next arm began before private record was persisted", e)
		}
		var record collectionRecording
		if json.Unmarshal(raw, &record) != nil || verifyCollectionRecording(record) != nil {
			t.Error("prior record not complete")
		}
		return originalJEV.RoundTrip(req)
	})
	firstClaim := auth.campaign.claimSHA
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	receipt, e := runWindowAcquisition(ctx, auth)
	if !receipt.OperationDeadline.Equal(deadline) || receipt.OperationLimitSeconds != 300 {
		t.Fatal("operation extended caller deadline", receipt.OperationDeadline, deadline)
	}
	if e != nil {
		t.Fatalf("%+v %v", receipt, e)
	}
	if receipt.Status != "collected_window" || !receipt.Advanced || !receipt.Cleanup || !receipt.Observer || receipt.FinishedArms != 2 || receipt.UnrunArms != 0 || receipt.QualityNegativeArms != 2 || *model != 4 || *judge != 1 {
		t.Fatalf("wrong collection receipt %+v model%d judge%d", receipt, *model, *judge)
	}
	if !receipt.PayloadVerified || receipt.PayloadStatus != "verified_ledger_linkage" || receipt.PayloadPackets != 5 || receipt.PayloadPackets != receipt.Accounting.ModelAttempts+receipt.Accounting.JEVAttempts || !comparisonDigest(receipt.PayloadManifestSHA256) || receipt.PayloadAuditOnlyCompletion != "unavailable_from_payload_capture" {
		t.Fatal("native payload linkage missing", receipt)
	}
	payloadRaw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, "payload-manifest.json"))
	if e != nil || conversationHash(payloadRaw) != receipt.PayloadManifestSHA256 {
		t.Fatal("payload manifest missing/unbound", e)
	}
	var manifest windowPayloadManifest
	if json.Unmarshal(payloadRaw, &manifest) != nil || manifest.Contract.ClaimSHA256 != firstClaim || manifest.Contract.ScopeSHA256 != receipt.ScopeSHA256 || manifest.EvidenceKind != "local_stub" {
		t.Fatal("payload manifest identity differs", manifest)
	}
	for _, packet := range manifest.Packets {
		request, e := os.ReadFile(filepath.Join(auth.privateReportRoot, "native-payloads", packet.Request.File))
		if e != nil || conversationHash(request) != packet.Request.FullSHA256 || len(request) != packet.Identity.Bytes {
			t.Fatal("exact native request missing", packet, e)
		}
		response, e := os.ReadFile(filepath.Join(auth.privateReportRoot, "native-payloads", packet.Response.Body.File))
		if e != nil || conversationHash(response) != packet.Response.Body.FullSHA256 || packet.Response.Status != 200 || !packet.Response.Complete {
			t.Fatal("exact native response missing", packet, e)
		}
	}
	if receipt.PlannedCampaignPairs != 3660 || receipt.PlannedCampaignArms != 7320 || receipt.PlannedCampaignTurns != 11772 || auth.campaign.index != 2 || receipt.NextClaimSHA256 != auth.campaign.claimSHA {
		t.Fatal("denominator/next claim differs", receipt)
	}
	for i, row := range receipt.Rows {
		if row.Success || row.Status != "failed" || row.Disposition != "continue_recorded" || row.Arm != auth.campaign.prepared.scope.Slots[0].ArmOrder[i] || !comparisonDigest(row.RecordingSHA256) || len(row.TraceID) != 32 {
			t.Fatal("quality/order/proof promoted", row)
		}
		raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, row.RecordingFile))
		if e != nil || conversationHash(raw) != row.RecordingSHA256 {
			t.Fatal("record not durably bound", e)
		}
	}
	raw, _ := json.Marshal(receipt)
	for _, secret := range []string{auth.modelKey, auth.jevKey, "root:root@", "pk-collector", "sk-collector"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("receipt secret leak")
		}
	}
	if _, e := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); e != nil {
		t.Fatal(e)
	}
}

func TestWindowCollectorJEVFaultStopsRemainingArmsAndNoNextClaim(t *testing.T) {
	auth, model, judge := testWindowCollectorAuthorization(t, 2)
	auth.jevUpstream = windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) {
		*judge++
		return nil, fmt.Errorf("synthetic-secret-provider-error")
	})
	r, e := runWindowAcquisition(context.Background(), auth)
	if e == nil || r.Advanced || r.Cleanup || !r.EnvironmentCleanup || r.FinishedArms != 1 || r.AttemptedArms != 2 || r.UnrunArms != 2 || *model != 2 || *judge != 1 || auth.campaign.index != 1 || !auth.campaign.poisoned {
		t.Fatalf("wrong failure result %+v %v models%d judges%d", r, e, *model, *judge)
	}
	if r.PayloadVerified || r.PayloadPackets != 3 || r.PayloadStatus != "audit_only" || !comparisonDigest(r.PayloadManifestSHA256) {
		t.Fatal("failed operation raw audit missing/promoted", r)
	}
	if r.FirstFailureOrdinal != 1 || r.FirstFailureArm != "jev_selective" || r.FirstFailureStage != "arm_proof" || r.Accounting.JEVHeld == 0 {
		t.Fatal("first failure or unknown usage not retained", r)
	}
	for _, row := range r.Rows[2:] {
		if row.Attempted || row.Status != "not_started" {
			t.Fatal("remaining denominator lost", row)
		}
	}
	if _, e := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(e) {
		t.Fatal("failed window advanced")
	}
	if strings.Contains(e.Error(), "synthetic-secret-provider-error") {
		t.Fatal("failure leaked provider error")
	}
}
func TestWindowCollectorObserverFaultStopsBeforeNextArm(t *testing.T) {
	auth, model, judge := testWindowCollectorAuthorization(t, 2)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.WriteHeader(503)
	}))
	defer receiver.Close()
	observer, e := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-failure", SecretKey: "sk-failure", HashKey: strings.Repeat("h", 32), Release: auth.observerIdentity.Release, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	defer observer.Shutdown(context.Background())
	auth.observer = observer
	auth.observerIdentity = observer.EvaluationIdentity()
	r, e := runWindowAcquisition(context.Background(), auth)
	if e == nil || r.Advanced || r.Cleanup || !r.EnvironmentCleanup || r.FinishedArms != 0 || r.AttemptedArms != 1 || r.UnrunArms != 3 || *model < 1 || *model > 2 || *judge != 0 || r.ObserverCountersCumulative.Failed == 0 || auth.campaign.index != 1 || !auth.campaign.poisoned {
		t.Fatal(r, e, *model, *judge)
	}
	if _, e := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(e) {
		t.Fatal("observer failure advanced")
	}
}
func TestWindowCollectorEnvironmentOpenFailureRetainsReceiptAndNoAdvance(t *testing.T) {
	auth, model, judge := testWindowCollectorAuthorization(t)
	auth.fixtureOptions.AdminDSN = "root:root@tcp(127.0.0.1:1)/"
	r, e := runWindowAcquisition(context.Background(), auth)
	if e == nil || r.FirstFailureStage != "environment_open" || r.Advanced || r.Cleanup || r.UnrunArms != 2 || *model != 0 || *judge != 0 || auth.campaign.index != 1 || !auth.campaign.poisoned {
		t.Fatal(r, e)
	}
	if _, e := os.Stat(filepath.Join(auth.privateReportRoot, "receipt.json")); e != nil {
		t.Fatal("failed open receipt missing", e)
	}
}
func TestWindowCollectorCleanupFailurePoisonsWithoutNextClaim(t *testing.T) {
	auth, _, _ := testWindowCollectorAuthorization(t)
	ledger, e := auth.campaign.openWindowCampaignLedger()
	if e != nil {
		t.Fatal(e)
	}
	// A closed sql.DB fails before any SQL is sent. This constructs no schema,
	// fabricates no arm proof, and tests only the real owned cleanup failure path.
	admin, e := sql.Open("mysql", "")
	if e != nil {
		t.Fatal(e)
	}
	admin.Close()
	r := &windowAcquisitionRun{auth: *auth, ledger: ledger, environment: &PairedFixtureEnvironment{admin: admin, name: "ai_gateway_pair_synthetic_cleanup_no_schema"}}
	if e := r.closeWindowAcquisitionEnvironment(); e == nil || r.receipt.FirstFailureStage != "environment_cleanup" || r.receipt.Cleanup || r.receipt.Advanced || !auth.campaign.poisoned || ledger.Snapshot().Halt == "" {
		t.Fatal(r.receipt, e)
	}
	if _, e := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(e) {
		t.Fatal("cleanup failure advanced")
	}
}

func TestWindowCollectorDurabilityFailureKeepsLiveUnknownHold(t *testing.T) {
	cap, dir := windowTransportFixture(t)
	request := windowTestRequest("model")
	request.Model = "deepseek-flash"
	ticket, e := cap.ledger.Begin(request)
	if e != nil {
		t.Fatal(e)
	}
	cap.ledger.file = &failingWindowFile{windowJournalFile: cap.ledger.file, syncFailure: true}
	if e = cap.ledger.Settle(ticket, strings.Repeat("a", 64), &windowUsage{Input: windowInt(2), Output: windowInt(1)}); e == nil {
		t.Fatal("injected durability failure missing")
	}
	r := &windowAcquisitionRun{ledger: cap.ledger, ledgerDir: dir}
	r.captureWindowAcquisitionAccounting()
	if r.receipt.AccountingSource != "live_owner_snapshot" || r.receipt.Accounting.ModelHeld != 3024 || r.receipt.Accounting.ModelKnown != 0 || r.receipt.Accounting.Pending == nil || r.receipt.Accounting.Halt != "durability_failure" {
		t.Fatal("readable unsynced journal replaced live held accounting", r.receipt)
	}
	if r.receipt.LedgerCompletionProof != "unavailable_from_journal" || !comparisonDigest(r.receipt.LedgerSHA256) {
		t.Fatal("audit digest lost", r.receipt)
	}
}

func TestWindowCollectorOutputFailureStopsAuthorizedOwner(t *testing.T) {
	prepared, baseline := fixtureWindowCampaign(t)
	campaign, e := startWindowCampaign(prepared, baseline)
	if e != nil {
		t.Fatal(e)
	}
	defer campaign.closeWindowCampaign()
	path := filepath.Join(t.TempDir(), "occupied")
	if e = os.WriteFile(path, []byte("unchanged"), 0600); e != nil {
		t.Fatal(e)
	}
	r := &windowAcquisitionRun{auth: windowAcquisitionAuthorization{campaign: campaign, privateReportRoot: path}}
	if e = r.openWindowAcquisitionReportRoot(); e == nil || !campaign.poisoned || r.receipt.FirstFailureStage != "report_root" || campaign.ledger != nil {
		t.Fatal("post-validation output failure left owner usable", r.receipt, e)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "unchanged" {
		t.Fatal("existing output overwritten")
	}
}

func testWindowCollectorPayloadLedger(t *testing.T) (*windowAccounting, string, *windowPayloadStore, windowPayloadManifest) {
	t.Helper()
	cap, dir := windowTransportFixture(t)
	store, e := newWindowPayloadStore(filepath.Join(t.TempDir(), "native"), dir, cap.ledger.contract, "local_stub")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	for i, provider := range []string{"model", "jev"} {
		raw := []byte(windowProtocolModelBody)
		if provider == "jev" {
			raw = []byte(windowProtocolJEVBody)
		}
		request := windowTestRequest(provider)
		request.Model = cap.ledger.contract.Model.Model
		if provider == "jev" {
			request.Model = cap.ledger.contract.JEV.Model
		}
		request.Bytes = len(raw)
		request.BodySHA256 = conversationHash(raw)
		ticket, e := cap.ledger.Begin(request)
		if e != nil {
			t.Fatal(e)
		}
		if e = store.writeWindowRequest(i+1, request, raw); e != nil {
			t.Fatal(e)
		}
		response := windowTransportResponse(provider)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if e = store.writeWindowResponse(i+1, request, 200, body, true, "complete"); e != nil {
			t.Fatal(e)
		}
		if e = cap.ledger.Settle(ticket, conversationHash(body), &windowUsage{Input: windowInt(2), Output: windowInt(1)}); e != nil {
			t.Fatal(e)
		}
	}
	manifest, _, e := verifyWindowPayloadStore(store)
	if e != nil {
		t.Fatal(e)
	}
	return cap.ledger, dir, store, manifest
}
func TestWindowCollectorPayloadLinkageAcceptsHealthyUnsealedOwner(t *testing.T) {
	ledger, dir, _, manifest := testWindowCollectorPayloadLedger(t)
	audit, e := readWindowAccounting(dir)
	if e != nil || audit.State.Halt != "unsealed_window" {
		t.Fatal(audit, e)
	}
	if e = validateWindowAcquisitionPayloadLinkage(ledger, dir, "local_stub", manifest); e != nil {
		t.Fatal("audit-only unsealed status rejected healthy live owner", e)
	}
}
func TestWindowCollectorPayloadLinkageRejectsMismatch(t *testing.T) {
	for _, kind := range []string{"contract", "kind", "count", "sequence", "identity", "request_sha", "request_bytes", "response_sha", "response_status", "response_partial", "missing_response", "live_stop", "live_poison"} {
		t.Run(kind, func(t *testing.T) {
			ledger, dir, _, manifest := testWindowCollectorPayloadLedger(t)
			switch kind {
			case "contract":
				manifest.Contract.WindowID = "window-999"
			case "kind":
				manifest.EvidenceKind = "recorded_run"
			case "count":
				manifest.Packets = manifest.Packets[:1]
			case "sequence":
				manifest.Packets[0].Sequence = 2
			case "identity":
				manifest.Packets[0].Identity.CaseKey = "other"
			case "request_sha":
				manifest.Packets[0].Request.FullSHA256 = strings.Repeat("a", 64)
			case "request_bytes":
				manifest.Packets[0].Request.Bytes++
			case "response_sha":
				manifest.Packets[0].Response.Body.FullSHA256 = strings.Repeat("b", 64)
			case "response_status":
				manifest.Packets[0].Response.Status = 503
			case "response_partial":
				manifest.Packets[0].Response.Body.Complete = false
			case "missing_response":
				manifest.Packets[0].Response = nil
			case "live_stop":
				windowTransportStop(ledger, "protocol_failure")
			case "live_poison":
				ledger.mu.Lock()
				ledger.poisoned = true
				ledger.mu.Unlock()
			}
			if e := validateWindowAcquisitionPayloadLinkage(ledger, dir, "local_stub", manifest); e == nil {
				t.Fatal("unbound payload accepted")
			}
		})
	}
}

func TestWindowCollectorPayloadPoisonNeverReturnsVerifiedHash(t *testing.T) {
	ledger, dir, store, _ := testWindowCollectorPayloadLedger(t)
	report := filepath.Join(t.TempDir(), "private-report")
	if e := os.Mkdir(report, 0700); e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(report)
	if e != nil {
		t.Fatal(e)
	}
	r := &windowAcquisitionRun{auth: windowAcquisitionAuthorization{privateReportRoot: report, evidenceKind: "local_stub"}, ledger: ledger, ledgerDir: dir, payload: store, rootInfo: info, recordFiles: map[string]string{}}
	r.receipt.FirstFailureStage = "earlier_failure"
	if e = r.captureWindowAcquisitionPayload(false); e != nil || !comparisonDigest(r.receipt.PayloadManifestSHA256) || r.receipt.PayloadVerified {
		t.Fatal(r.receipt, e)
	}
	store.mu.Lock()
	store.poisoned = true
	store.mu.Unlock()
	if e = r.captureWindowAcquisitionPayload(false); e == nil || r.receipt.PayloadManifestSHA256 != "" || r.receipt.PayloadVerified || r.receipt.PayloadStatus != "unavailable_store_verification" || r.receipt.FirstFailureStage != "earlier_failure" {
		t.Fatal("poisoned writer produced a verified artifact or replaced first failure", r.receipt, e)
	}
	if _, e := os.Stat(filepath.Join(report, "payload-manifest.json")); e != nil {
		t.Fatal("prior audit evidence deleted", e)
	}
}

func TestWindowCollectorPayloadOuterRootReplacementRejects(t *testing.T) {
	cap, dir := windowTransportFixture(t)
	parent := t.TempDir()
	report := filepath.Join(parent, "report")
	if e := os.Mkdir(report, 0700); e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(report)
	if e != nil {
		t.Fatal(e)
	}
	store, e := newWindowPayloadStore(filepath.Join(report, "native-payloads"), dir, cap.ledger.contract, "local_stub")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	r := &windowAcquisitionRun{auth: windowAcquisitionAuthorization{privateReportRoot: report, evidenceKind: "local_stub"}, ledger: cap.ledger, ledgerDir: dir, payload: store, rootInfo: info, recordFiles: map[string]string{}}
	if e = r.captureWindowAcquisitionPayload(false); e != nil {
		t.Fatal(e)
	}
	moved := report + "-old"
	if e = os.Rename(report, moved); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(report, 0700); e != nil {
		t.Fatal(e)
	}
	// Preserve the genuine anchored payload child while replacing its outer root.
	if e = os.Rename(filepath.Join(moved, "native-payloads"), filepath.Join(report, "native-payloads")); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(filepath.Join(moved, "payload-manifest.json"), filepath.Join(report, "payload-manifest.json")); e != nil {
		t.Fatal(e)
	}
	if e = r.captureWindowAcquisitionPayload(false); e == nil || r.receipt.PayloadManifestSHA256 != "" || r.receipt.PayloadStatus != "unavailable_report_root" {
		t.Fatal("replaced outer report root accepted", r.receipt, e)
	}
}
