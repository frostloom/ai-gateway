package taskbench

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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

// Closed transports, isolated business fixture and data-only historical mirror.
// This never calls the concrete actual constructor or mints actual readiness.
func testActualV4CollectorLocalStub(t *testing.T) (*windowAcquisitionAuthorization, *int, *int) {
	t.Helper()
	source, profiles, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = windowOwnedQualityPolicySHA()
	scope, err := prepareFullWindowScopeV4(source, profiles, 28, windowV4TestCaps(t), bindings, windowV4BaselineSHA, windowBudget105ApprovalSHA)
	if err != nil {
		t.Fatal(err)
	}
	sha, _, err := validateFullWindowScope(scope, source, profiles, bindings)
	if err != nil {
		t.Fatal(err)
	}
	parent := stoppedEvidenceRoot(t)
	root := filepath.Join(parent, "local_stub-campaign")
	limits := windowV4ExpectedLimits()
	execution, err := executionSHAWindowCampaign(sha, root, limits)
	if err != nil {
		t.Fatal(err)
	}
	p, err := prepareWindowCampaign(root, root, scope, source, profiles, bindings, sha, execution, limits)
	if err != nil {
		t.Fatal(err)
	}
	roots := [3]string{filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(parent, "third")}
	base := expectedStoppedV3WindowBaseline()
	p.stoppedHistory = &fixtureStoppedV4LocalStub{fixtureStoppedHistory: fixtureStoppedHistory{base: base, sha: windowV4BaselineSHA, root: roots[2]}, roots: roots}
	campaign, err := startWindowCampaign(p, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { campaign.closeWindowCampaign() })
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.Copy(io.Discard, req.Body)
		req.Body.Close()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	t.Cleanup(receiver.Close)
	observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-v4-local-stub", SecretKey: "sk-v4-local-stub", HashKey: strings.Repeat("h", 32), Release: bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observer.Shutdown(context.Background()) })
	model, jev := new(int), new(int)
	auth := &windowAcquisitionAuthorization{campaign: campaign, source: source, frozenScopeSHA: sha, frozenExecutionSHA: execution, actualExecutionSHA: execution, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "synthetic-model-key", jevKey: "synthetic-jev-key", modelUpstream: testWindowCollectorModel(model), jevUpstream: testWindowCollectorJEV(jev), fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"}, privateReportRoot: filepath.Join(parent, "records"), evidenceKind: "local_stub"}
	return auth, model, jev
}

func TestActualV4CollectorInfrastructureStops(t *testing.T) {
	t.Run("handler fsync retains unknown hold", func(t *testing.T) {
		auth, model, jev := testActualV4CollectorLocalStub(t)
		original := auth.modelUpstream
		auth.modelUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			response, err := original.RoundTrip(req)
			if err != nil {
				return response, err
			}
			ledger := auth.campaign.ledger
			ledger.mu.Lock()
			ledger.file = &failingWindowFile{windowJournalFile: ledger.file, syncFailure: true}
			ledger.mu.Unlock()
			return response, nil
		})
		r, err := runWindowAcquisition(context.Background(), auth)
		if err == nil || r.Advanced || r.AttemptedArms != 1 || r.FinishedArms != 0 || *model != 1 || *jev != 0 || !auth.campaign.poisoned || r.AccountingSource != "live_owner_snapshot" || r.Accounting.ModelKnown != 0 || r.Accounting.ModelHeld != 74752 || r.Accounting.Pending == nil || r.Accounting.Halt != "durability_failure" {
			t.Fatal("Handler fsync failure hidden/retried or unknown hold released", r, err, *model, *jev)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("Handler fsync failure advanced claim")
		}
	})
	t.Run("fsync retains worst hold", func(t *testing.T) {
		auth, model, jev := testActualV4CollectorLocalStub(t)
		ledger, err := auth.campaign.openWindowCampaignLedger()
		if err != nil {
			t.Fatal(err)
		}
		request := windowTestRequest("model")
		request.Model = "deepseek-flash"
		ticket, err := ledger.Begin(request)
		if err != nil {
			t.Fatal(err)
		}
		ledger.file = &failingWindowFile{windowJournalFile: ledger.file, syncFailure: true}
		usage := &windowUsage{Input: windowInt(100), Output: windowInt(8), CacheHit: windowInt(80), CacheMiss: windowInt(20)}
		if err := ledger.Settle(ticket, strings.Repeat("a", 64), usage); err == nil {
			t.Fatal("injected fsync accepted")
		}
		r := &windowAcquisitionRun{ledger: ledger, ledgerDir: auth.campaign.ledgerDir}
		r.captureWindowAcquisitionAccounting()
		if r.receipt.AccountingSource != "live_owner_snapshot" || r.receipt.Accounting.ModelHeld != 74752 || r.receipt.Accounting.ModelKnown != 0 || r.receipt.Accounting.Pending == nil || r.receipt.Accounting.Halt != "durability_failure" || r.receipt.LedgerCompletionProof != "unavailable_from_journal" || *model != 0 || *jev != 0 {
			t.Fatal("cache pricing released unknown hold", r.receipt)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("fsync failure advanced claim")
		}
	})
	t.Run("observer", func(t *testing.T) {
		auth, model, jev := testActualV4CollectorLocalStub(t)
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			w.WriteHeader(503)
		}))
		defer receiver.Close()
		observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-v4-failure", SecretKey: "sk-v4-failure", HashKey: strings.Repeat("h", 32), Release: auth.observerIdentity.Release, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Shutdown(context.Background())
		auth.observer, auth.observerIdentity = observer, observer.EvaluationIdentity()
		r, err := runWindowAcquisition(context.Background(), auth)
		if err == nil || r.Advanced || r.AttemptedArms != 1 || r.FinishedArms != 0 || *model < 1 || *model > 2 || *jev != 0 || r.ObserverCountersCumulative.Failed == 0 || !auth.campaign.poisoned {
			t.Fatal("observer failure hidden or advanced", r, err, *model, *jev)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("observer failure advanced claim")
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		auth, model, jev := testActualV4CollectorLocalStub(t)
		ledger, err := auth.campaign.openWindowCampaignLedger()
		if err != nil {
			t.Fatal(err)
		}
		admin, err := sql.Open("mysql", "")
		if err != nil {
			t.Fatal(err)
		}
		admin.Close()
		r := &windowAcquisitionRun{auth: *auth, ledger: ledger, environment: &PairedFixtureEnvironment{admin: admin, name: "ai_gateway_pair_synthetic_v4_cleanup_no_schema"}}
		if err := r.closeWindowAcquisitionEnvironment(); err == nil || r.receipt.FirstFailureStage != "environment_cleanup" || r.receipt.Cleanup || r.receipt.Advanced || !auth.campaign.poisoned || ledger.Snapshot().Halt == "" || *model != 0 || *jev != 0 {
			t.Fatal("cleanup failure hidden or advanced", r.receipt, err)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("cleanup failure advanced claim")
		}
	})
	for _, mode := range []string{"occupied report", "wrong execution", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			auth, model, jev := testActualV4CollectorLocalStub(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "occupied report":
				if err := os.WriteFile(auth.privateReportRoot, []byte("local_stub occupied"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong execution":
				auth.actualExecutionSHA = strings.Repeat("9", 64)
			case "cancelled":
				cancel()
			}
			if run, err := openWindowAcquisition(ctx, auth); run != nil || err == nil || *model != 0 || *jev != 0 {
				t.Fatal("preflight reached provider", err, *model, *jev)
			}
			if _, err := os.Stat(filepath.Join(auth.campaign.root, "ledger-000001")); !os.IsNotExist(err) {
				t.Fatal("preflight created ledger")
			}
			if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("preflight advanced claim")
			}
		})
	}
}

func TestActualV4CollectorOwnedNegativeRetainsDefinedSuffix(t *testing.T) {
	auth, model, jev := testActualV4CollectorLocalStub(t)
	auth.modelUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		*model++
		body := `{"model":"deepseek-flash","content":[{"type":"tool_use","id":"v4-owned-wrong-preview","name":"recharge","input":{"amount_money":100}}],"stop_reason":"tool_use","usage":{"input_tokens":20,"cache_read_input_tokens":80,"output_tokens":8}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	r, err := runWindowAcquisition(context.Background(), auth)
	if err != nil || !r.Advanced || r.FinishedArms != 56 || r.QualityNegativeArms != 56 || r.QualityEndArms != 56 || *model != 56 || *jev != 28 || r.ObservedTurns+r.QualityEndUnrunTurns != r.PlannedCurrentTurns || r.QualityEndUnrunTurns == 0 {
		t.Fatal("owned negative/suffix hidden or promoted", r, err, *model, *jev)
	}
	for _, row := range r.Rows {
		if row.Status != "failed" || row.Success || row.StructuralPass || row.QualityEnd == nil || !row.ExcludeCompleteTaskComparison || row.QualityEnd.UnrunTurns != row.DefinedTurns-row.ObservedTurns {
			t.Fatal("negative disposition lost", row)
		}
	}
}

func TestActualV4CollectorCacheSettlementAndFailures(t *testing.T) {
	for _, mode := range []string{"reported cache", "cache absent", "missing usage", "invalid cache", "model EOF", "JEV EOF"} {
		t.Run(mode, func(t *testing.T) {
			auth, model, jev := testActualV4CollectorLocalStub(t)
			original := auth.modelUpstream
			auth.modelUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if mode == "model EOF" {
					*model++
					return nil, io.EOF
				}
				response, err := original.RoundTrip(req)
				if err != nil {
					return response, err
				}
				raw, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					return nil, err
				}
				var body map[string]any
				if err = json.Unmarshal(raw, &body); err != nil {
					return nil, err
				}
				usage := body["usage"].(map[string]any)
				switch mode {
				case "reported cache":
					usage["input_tokens"], usage["cache_read_input_tokens"] = 20, 80
				case "missing usage":
					delete(body, "usage")
				case "invalid cache":
					usage["cache_read_input_tokens"] = -1
				}
				raw, _ = json.Marshal(body)
				response.Body = io.NopCloser(bytes.NewReader(raw))
				return response, nil
			})
			if mode == "JEV EOF" {
				auth.jevUpstream = windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { *jev++; return nil, io.EOF })
			}
			r, err := runWindowAcquisition(context.Background(), auth)
			if r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 || r.PlannedCurrentPairs != 28 {
				t.Fatal("full denominator or fixed window lost", r)
			}
			audit, readErr := readWindowAccounting(filepath.Join(auth.campaign.root, "ledger-000001"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if audit.Contract.Schema != "full-window-accounting.v2" || audit.Contract.Model.CacheHitInputRate != 40000 {
				t.Fatal("new accounting not selected", audit.Contract)
			}
			if mode == "reported cache" || mode == "cache absent" {
				unit := int64(108)
				if mode == "cache absent" {
					unit = 264
				}
				if err != nil || !r.Advanced || !r.PayloadVerified || !r.Cleanup || !r.Observer || r.AttemptedArms != 56 || r.FinishedArms != 56 || *jev != 28 || *model < 1 || audit.State.ModelKnown != int64(*model)*unit || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 {
					t.Fatal("closed handler settlement/denominator failed", r, err, *model, *jev, audit.State)
				}
				raw, err := os.ReadFile(filepath.Join(auth.campaign.root, "claim-000002.json"))
				var claim windowCampaignClaim
				if err != nil || json.Unmarshal(raw, &claim) != nil || claim.Schema != "full-window-campaign-claim.v4" || len(claim.HistoricalClaims) != 25 || claim.ParentModel != 105000000 || claim.ParentJEV != 1000000 {
					t.Fatal("closed next claim lost epoch", err, claim)
				}
			} else {
				wantModel, wantJEV := 1, 0
				if mode == "JEV EOF" {
					wantModel, wantJEV = 2, 1
				}
				if err == nil || r.Advanced || !auth.campaign.poisoned || *model != wantModel || *jev != wantJEV || r.FinishedArms >= r.AttemptedArms {
					t.Fatal("failure hidden or retried", r, err, *model, *jev)
				}
				if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
					t.Fatal("failed window advanced claim")
				}
			}
			t.Logf("local_stub mode=%s model=%d FakeJEV=%d; not actual quality or billing evidence", mode, *model, *jev)
		})
	}
}
