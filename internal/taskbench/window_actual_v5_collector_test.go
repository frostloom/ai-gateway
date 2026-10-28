package taskbench

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/observability"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Synthetic DATA history and real closed Handler; no concrete actual proof or provider.
func testActualV5CollectorLocalStub(t *testing.T) (*windowAcquisitionAuthorization, *int, *int) {
	t.Helper()
	source, profiles, bindings := windowScopeInputs(t)
	bindings.PolicySHA256 = windowOwnedQualityPolicySHA()
	scope, err := prepareFullWindowScopeV5(source, profiles, 28, windowV5TestCaps(t), bindings, windowV5BaselineSHA, windowBudget105ApprovalSHA)
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
	roots := [4]string{filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(parent, "third"), filepath.Join(parent, "fourth")}
	base := expectedStoppedV4WindowBaseline()
	p.stoppedHistory = &fixtureStoppedV5LocalStub{fixtureStoppedHistory: fixtureStoppedHistory{base: base, sha: windowV5BaselineSHA, root: roots[3]}, roots: roots}
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
	observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-v5-local-stub", SecretKey: "sk-v5-local-stub", HashKey: strings.Repeat("h", 32), Release: bindings.ExecutorRevision, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observer.Shutdown(context.Background()) })
	model, jev := new(int), new(int)
	auth := &windowAcquisitionAuthorization{campaign: campaign, source: source, frozenScopeSHA: sha, frozenExecutionSHA: execution, actualExecutionSHA: execution, observer: observer, observerIdentity: observer.EvaluationIdentity(), modelKey: "synthetic-model-key", jevKey: "synthetic-jev-key", modelUpstream: testWindowCollectorModel(model), jevUpstream: testWindowCollectorJEV(jev), fixtureOptions: PairedFixtureOptions{AllowFixtureCreation: true, AdminDSN: "root:root@tcp(127.0.0.1:3307)/", ProfilesDir: "../../eval/runtime-profiles/2026-10-13-six-profiles-v1"}, privateReportRoot: filepath.Join(parent, "records"), evidenceKind: "local_stub"}
	return auth, model, jev
}

func TestActualV5CollectorClosedNative(t *testing.T) {
	for _, mode := range []string{"cache", "absent", "repair", "modelEOF", "jevEOF", "negative"} {
		t.Run(mode, func(t *testing.T) {
			auth, model, jev := testActualV5CollectorLocalStub(t)
			original := auth.modelUpstream
			injected := false
			auth.modelUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if mode == "modelEOF" {
					*model++
					return nil, io.EOF
				}
				rsp, e := original.RoundTrip(req)
				if e != nil {
					return rsp, e
				}
				raw, e := io.ReadAll(rsp.Body)
				rsp.Body.Close()
				if e != nil {
					return nil, e
				}
				var body map[string]any
				if json.Unmarshal(raw, &body) != nil {
					t.Fatal("closed native JSON")
				}
				usage := body["usage"].(map[string]any)
				if mode != "absent" {
					usage["input_tokens"], usage["cache_read_input_tokens"] = 20, 80
				}
				if mode == "repair" && !injected {
					body["content"] = []any{map[string]any{"type": "text", "text": "当前现金余额为 ¥5000.00。"}}
					body["stop_reason"] = "end_turn"
					injected = true
				}
				if mode == "negative" {
					body["content"] = []any{map[string]any{"type": "tool_use", "id": "v5-owned-wrong-preview", "name": "recharge", "input": map[string]any{"amount_money": 100}}}
					body["stop_reason"] = "tool_use"
				}
				raw, _ = json.Marshal(body)
				rsp.Body = io.NopCloser(bytes.NewReader(raw))
				return rsp, nil
			})
			if mode == "jevEOF" {
				auth.jevUpstream = windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { *jev++; return nil, io.EOF })
			}
			r, e := runWindowAcquisition(context.Background(), auth)
			audit, ae := readWindowAccounting(filepath.Join(auth.campaign.root, "ledger-000001"))
			if ae != nil {
				t.Fatal(ae)
			}
			if audit.Contract.Schema != "full-window-accounting.v2" || audit.Contract.Model.CacheHitInputRate != 40000 || auth.campaign.prepared.scope.Schema != "full-window-scope.v5" {
				t.Fatal("wrong epoch/accounting")
			}
			if mode == "modelEOF" || mode == "jevEOF" {
				wm, wj := 1, 0
				if mode == "jevEOF" {
					wm, wj = 2, 1
				}
				if e == nil || r.Advanced || !auth.campaign.poisoned || *model != wm || *jev != wj || audit.State.Pending == nil {
					t.Fatal("provider failure retried or released", r, e, *model, *jev)
				}
				if _, e := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(e) {
					t.Fatal("failed window advanced")
				}
				return
			}
			unit := int64(108)
			if mode == "absent" {
				unit = 264
			}
			if e != nil || !r.Advanced || !r.PayloadVerified || !r.Cleanup || !r.Observer || r.FinishedArms != 56 || *jev != 28 || audit.State.ModelKnown != int64(*model)*unit || audit.State.JEVKnown != int64(*jev)*5 || audit.State.Pending != nil || audit.State.ModelHeld != 0 || audit.State.JEVHeld != 0 {
				t.Fatal("closed5 acquisition/accounting", r, e, *model, *jev, audit.State)
			}
			var repairs int
			for _, row := range r.Rows {
				raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, row.RecordingFile))
				if e != nil {
					t.Fatal(e)
				}
				record, e := decodeWindowStoppedV2Recording(raw)
				if e != nil {
					t.Fatal(e)
				}
				if row.Success || record.Result.Success {
					t.Fatal("success upgraded")
				}
				for _, ob := range record.Result.Observations {
					if ob.Turn.Response.Telemetry != nil {
						repairs += ob.Turn.Response.Telemetry.ReplyRepairs
					}
				}
				if mode == "negative" && (row.Status != "failed" || row.StructuralPass || row.QualityEnd == nil || !row.ExcludeCompleteTaskComparison) {
					t.Fatal("owned negative upgraded")
				}
			}
			if mode == "repair" && (!injected || repairs != 1 || *model != 117) {
				t.Fatal("actual Handler repair not exercised", injected, repairs, *model)
			}
			if mode == "negative" && (r.QualityEndArms != 56 || r.QualityNegativeArms != 56 || r.ObservedTurns+r.QualityEndUnrunTurns != r.PlannedCurrentTurns || r.QualityEndUnrunTurns == 0) {
				t.Fatal("negative suffix hidden", r)
			}
			raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, "payload-manifest.json"))
			var payload windowPayloadManifest
			if e != nil || json.Unmarshal(raw, &payload) != nil || len(payload.Packets) != *model+*jev {
				t.Fatal("native capture absent", e)
			}
			for _, packet := range payload.Packets {
				for _, body := range []windowPayloadBody{packet.Request, *packet.Response.Body} {
					raw, e := os.ReadFile(filepath.Join(auth.privateReportRoot, "native-payloads", body.File))
					if e != nil || conversationHash(raw) != body.FullSHA256 || len(raw) != body.Bytes {
						t.Fatal("native bytes mismatch", e)
					}
				}
			}
			raw, e = os.ReadFile(filepath.Join(auth.campaign.root, "claim-000002.json"))
			var claim windowCampaignClaim
			if e != nil || json.Unmarshal(raw, &claim) != nil || claim.Schema != "full-window-campaign-claim.v5" || len(claim.HistoricalClaims) != 28 || claim.BaselineEvidenceSHA256 != windowV5BaselineSHA || claim.ParentModel != 105000000 || claim.ParentJEV != 1000000 {
				t.Fatal("next claim identity", claim, e)
			}
			t.Logf("new5 local_stub mode=%s native=%d model=%d FakeJEV=%d repair=%d known=%d/%d qualitynegative=%d;not actual quality/billing", mode, len(payload.Packets), *model, *jev, repairs, audit.State.ModelKnown, audit.State.JEVKnown, r.QualityNegativeArms)
		})
	}
}
func TestActualV5CollectorInfrastructureStops(t *testing.T) {
	t.Run("handler fsync retains unknown hold", func(t *testing.T) {
		auth, model, jev := testActualV5CollectorLocalStub(t)
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
	t.Run("observer", func(t *testing.T) {
		auth, model, jev := testActualV5CollectorLocalStub(t)
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
		auth, model, jev := testActualV5CollectorLocalStub(t)
		ledger, err := auth.campaign.openWindowCampaignLedger()
		if err != nil {
			t.Fatal(err)
		}
		admin, err := sql.Open("mysql", "")
		if err != nil {
			t.Fatal(err)
		}
		admin.Close()
		r := &windowAcquisitionRun{auth: *auth, ledger: ledger, environment: &PairedFixtureEnvironment{admin: admin, name: "ai_gateway_pair_synthetic_v5_cleanup_no_schema"}}
		if err := r.closeWindowAcquisitionEnvironment(); err == nil || r.receipt.FirstFailureStage != "environment_cleanup" || r.receipt.Cleanup || r.receipt.Advanced || !auth.campaign.poisoned || ledger.Snapshot().Halt == "" || *model != 0 || *jev != 0 {
			t.Fatal("cleanup failure hidden or advanced", r.receipt, err)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("cleanup failure advanced claim")
		}
	})
	for _, mode := range []string{"occupied report", "wrong execution", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			auth, model, jev := testActualV5CollectorLocalStub(t)
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
