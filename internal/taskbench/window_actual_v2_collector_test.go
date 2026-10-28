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

// Same real Handler, isolated database and native guards as the original
// collector tests. The private synthetic history cannot create actual ready.
func TestActualV2CollectorHandlerNativeProtocolAndNextClaim(t *testing.T) {
	for _, mode := range []string{"normal", "standard", "bad tier", "missing usage", "JEV EOF"} {
		t.Run(mode, func(t *testing.T) {
			auth, model, judge := testWindowCollectorAuthorizationVersion(t, collectionPolicySHA(), true)
			original := auth.modelUpstream
			if mode != "normal" && mode != "JEV EOF" {
				auth.modelUpstream = windowTransportRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					resp, err := original.RoundTrip(req)
					if err != nil {
						return resp, err
					}
					raw, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil {
						return nil, err
					}
					var body map[string]any
					if err = json.Unmarshal(raw, &body); err != nil {
						return nil, err
					}
					if mode == "missing usage" {
						delete(body, "usage")
					} else {
						tier := "standard"
						if mode == "bad tier" {
							tier = "priority"
						}
						body["usage"].(map[string]any)["service_tier"] = tier
					}
					raw, _ = json.Marshal(body)
					resp.Body = io.NopCloser(bytes.NewReader(raw))
					return resp, nil
				})
			}
			if mode == "JEV EOF" {
				auth.jevUpstream = windowTransportRoundTripFunc(func(*http.Request) (*http.Response, error) { *judge++; return nil, io.EOF })
			}
			r, err := runWindowAcquisition(context.Background(), auth)
			if r.PlannedCampaignPairs != 3660 || r.PlannedCampaignArms != 7320 || r.PlannedCampaignTurns != 11772 {
				t.Fatal("full denominator lost", r)
			}
			if mode == "normal" || mode == "standard" {
				if err != nil || !r.Advanced || !r.PayloadVerified || !r.Cleanup || !r.Observer || *model != 4 || *judge != 1 || r.QualityNegativeArms != 2 {
					t.Fatal("normal native path failed/promoted quality", r, err, *model, *judge)
				}
				data, err := os.ReadFile(filepath.Join(auth.campaign.root, "claim-000002.json"))
				if err != nil {
					t.Fatal(err)
				}
				var claim windowCampaignClaim
				if json.Unmarshal(data, &claim) != nil || claim.Schema != "full-window-campaign-claim.v2" || len(claim.HistoricalClaims) != 10 || claim.ModelPermanent != 29475726 || claim.JEVPermanent != 227185 || claim.BaselineEvidenceSHA256 != auth.campaign.prepared.scope.BaselineEvidenceSHA256 {
					t.Fatal("second claim lost stopped epoch", claim)
				}
			} else {
				wantModel, wantJEV := 1, 0
				if mode == "JEV EOF" {
					wantModel, wantJEV = 2, 1
				}
				if err == nil || r.Advanced || !auth.campaign.poisoned || *model != wantModel || *judge != wantJEV {
					t.Fatal("technical failure retried or advanced", r, err, *model, *judge)
				}
				if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
					t.Fatal("failed window created next claim")
				}
			}
			t.Log(fmt.Sprintf("v2 native mode=%s model=%d FakeJEV=%d, not actual provider or semantic pass", mode, *model, *judge))
		})
	}
}

func TestActualV2CollectorObserverAndCleanupFailuresStop(t *testing.T) {
	t.Run("observer", func(t *testing.T) {
		auth, model, judge := testWindowCollectorAuthorizationVersion(t, collectionPolicySHA(), true)
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			w.WriteHeader(503)
		}))
		defer receiver.Close()
		observer, err := observability.New(context.Background(), observability.Config{Enabled: true, CaptureContent: true, Endpoint: receiver.URL + "/api/public/otel/v1/traces", PublicKey: "pk-v2-failure", SecretKey: "sk-v2-failure", HashKey: strings.Repeat("h", 32), Release: auth.observerIdentity.Release, Environment: "evaluation", SampleRate: 1, QueueSize: 4096, ExportTimeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Shutdown(context.Background())
		auth.observer, auth.observerIdentity = observer, observer.EvaluationIdentity()
		r, err := runWindowAcquisition(context.Background(), auth)
		if err == nil || r.Advanced || r.AttemptedArms != 1 || r.FinishedArms != 0 || *model < 1 || *model > 2 || *judge != 0 || r.ObserverCountersCumulative.Failed == 0 || !auth.campaign.poisoned {
			t.Fatal("observer failure advanced or hidden", r, err, *model, *judge)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("observer failure minted next claim")
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		auth, model, judge := testWindowCollectorAuthorizationVersion(t, collectionPolicySHA(), true)
		ledger, err := auth.campaign.openWindowCampaignLedger()
		if err != nil {
			t.Fatal(err)
		}
		admin, err := sql.Open("mysql", "")
		if err != nil {
			t.Fatal(err)
		}
		admin.Close()
		r := &windowAcquisitionRun{auth: *auth, ledger: ledger, environment: &PairedFixtureEnvironment{admin: admin, name: "ai_gateway_pair_synthetic_v2_cleanup_no_schema"}}
		if err := r.closeWindowAcquisitionEnvironment(); err == nil || r.receipt.FirstFailureStage != "environment_cleanup" || r.receipt.Cleanup || r.receipt.Advanced || !auth.campaign.poisoned || ledger.Snapshot().Halt == "" || *model != 0 || *judge != 0 {
			t.Fatal("cleanup failure advanced or sent request", r.receipt, err)
		}
		if _, err := os.Stat(filepath.Join(auth.campaign.root, "claim-000002.json")); !os.IsNotExist(err) {
			t.Fatal("cleanup failure minted next claim")
		}
	})
}
