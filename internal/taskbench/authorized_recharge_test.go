package taskbench

import (
	"context"
	"encoding/json"
	"github.com/frostloom/ai-gateway/internal/jev"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rechargeEntryInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	base := "../../eval/paired-pilots/2026-10-23-recharge-confirm-entry"
	read := func(n string) []byte {
		r, e := os.ReadFile(filepath.Join(base, n))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	a, p, b := read("authorization.example.json"), read("plan.json"), read("billing.example.json")
	e := []byte("synthetic evidence only; not vendor proof")
	a = alteredJSON(a, "human_authorized", true)
	a = alteredJSON(a, "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(b, "execution_revision", strings.Repeat("a", 40))
	b = alteredJSON(b, "account_tariff_verified", true)
	b = alteredJSON(b, "token_envelope_verified", true)
	b = alteredJSON(b, "evidence_sha256", conversationHash(e))
	return a, p, b, e
}

// Catches rejecting the new scope, permitting larger/rebound scopes, or dropping
// the independent consent, cumulative ceiling and offline acceptance bindings.
func TestRechargeEntryFixedScope(t *testing.T) {
	a, p, b, e := rechargeEntryInputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || !pilot.Ready() || pilot.CampaignID() != "recharge-confirm-20261023" {
		t.Fatalf("new scope not ready: %v %v", err, pilot.Blockers())
	}
	if err = pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	no, err := CheckAuthorizedRetest(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || no.Ready() || no.HumanAuthorized() {
		t.Fatal("budget ceiling enabled exact execution consent")
	}
	for _, key := range []string{"schema", "campaign_id", "case_key", "customer_source_revision", "runtime_contract_sha256", "budget_ceiling_sha256", "prior_offline_manifest_sha256", "prior_offline_acceptance_sha256", "scope_plan_sha256", "execution_revision"} {
		if _, err = CheckAuthorizedRetest(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatal("changed binding accepted", key)
		}
	}
	for _, key := range []string{"automatic_scope_expansion", "automatic_paid_retries"} {
		if _, err = CheckAuthorizedRetest(alteredJSON(a, key, true), p, b, e); err == nil {
			t.Fatal(key)
		}
	}
	var plan map[string]any
	json.Unmarshal(p, &plan)
	plan["batches"].([]any)[0].(map[string]any)["key"] = "recharge-002/duplicate_confirm"
	changed, _ := json.Marshal(plan)
	if _, err = CheckAuthorizedRetest(alteredJSON(a, "scope_plan_sha256", conversationHash(changed)), changed, alteredJSON(b, "scope_plan_sha256", conversationHash(changed)), e); err == nil {
		t.Fatal("rebound different case accepted")
	}
	var auth map[string]any
	json.Unmarshal(a, &auth)
	auth["authorized_spending"].(map[string]any)["max_model_calls"] = 11
	changed, _ = json.Marshal(auth)
	if _, err = CheckAuthorizedRetest(changed, p, b, e); err == nil {
		t.Fatal("expanded call cap accepted")
	}
	if _, err = CheckAuthorizedRetest(a, p, b, []byte("tampered")); err == nil {
		t.Fatal("billing evidence ignored")
	}
	_, _, oldB, oldE := recoveryRetestInputs(t)
	if _, err = CheckAuthorizedRetest(a, p, oldB, oldE); err == nil {
		t.Fatal("inherited consumed billing")
	}
	if _, err = CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("legacy entry accepted recharge")
	}
}

// Exercises the real controlled runner, Handler, database fixtures and budgets;
// only external model/JEV transports are replaced. Missing preview/confirmation,
// lost receipt provenance, fallback after vendor failure or output reuse fails.
func TestRechargeEntryLocalRunner(t *testing.T) {
	for _, mode := range []string{"complete", "provider-failure", "jev-failure"} {
		t.Run(mode, func(t *testing.T) {
			o := localCampaignOptions(t)
			a, p, b, e := rechargeEntryInputs(t)
			pilot, err := CheckAuthorizedRetest(a, p, b, e)
			if err != nil {
				t.Fatal(err)
			}
			deps, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer deps.close()
			calls := 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
					t.Fatal("wrong provider target")
				}
				if mode == "provider-failure" {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"synthetic"}`))}, nil
				}
				var body struct {
					Max int `json:"max_tokens"`
				}
				if json.NewDecoder(req.Body).Decode(&body) != nil || body.Max != 128 {
					t.Fatal("unbounded output")
				}
				msg := `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"recharge","type":"function","function":{"name":"recharge","arguments":"{\"amount_money\":100}"}}]}}],"usage":{"prompt_tokens":100,"completion_tokens":8}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(msg))}, nil
			})
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: deps.observer, Model: pilot.ModelSettings(), Upstream: up, JEVFactory: func() jev.Evaluator {
				if mode == "jev-failure" {
					return policyFailureJEV{}
				}
				return authorizedTestJEV{jev.NewFake(localJevScript("recharge", .99)), true}
			}}
			opts.Model.APIKey = "synthetic-only"
			dir := filepath.Join(t.TempDir(), "run")
			r, err := RunAuthorizedRetest(context.Background(), dir, opts)
			if mode != "complete" {
				if err == nil || r.Status != "stopped" {
					t.Fatalf("failure did not stop: %v %s", err, r.Status)
				}
			} else {
				if err != nil || r.Status != "checkpoint_pending" || r.Report == nil || r.Report.FinishedArms != 2 || r.Schema != "authorized-recharge-confirm.v1" {
					t.Fatalf("incomplete run %v %+v", err, r)
				}
				if r.ModelState.Calls != 2 || r.JEVAttempts != 1 || r.CampaignReservedModelCalls != 10 || r.CampaignReservedMicroCNY != 337920 || r.CampaignReservedMicroUSD != 2688 {
					t.Fatalf("wrong reservation/usage %+v", r)
				}
				if r.BudgetCeilingSHA != "4669a31fe6f74f7f6901ed9a4dcb114016d7ed4aac85be949d9b0b6e74c0c00b" || r.OfflineManifestSHA == "" || r.OfflineAcceptanceSHA == "" {
					t.Fatal("receipt lost provenance")
				}
				journal, err := loadPairJournal(filepath.Join(dir, "campaign", "batch-001", "run.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range journal.finished {
					if event.Result == nil || len(event.Result.Observations) != 2 {
						t.Fatal("missing two turns")
					}
					executed := 0
					for _, obs := range event.Result.Observations {
						for _, trace := range obs.Turn.Response.Trace {
							if trace.Tool == "recharge" && trace.Outcome == "success" {
								executed++
							}
						}
					}
					if executed != 1 {
						t.Fatalf("expected one real fixture recharge per arm, got %d", executed)
					}
				}
			}
			if mode == "jev-failure" && r.JEVAttempts != 1 {
				t.Fatal("JEV retried")
			}
			before := calls
			if _, err := RunAuthorizedRetest(context.Background(), dir, opts); err == nil || calls != before {
				t.Fatal("reused output touched upstream")
			}
		})
	}
}

func TestRechargeBillingCannotAuthorizeLegacyWithoutIdentity(t *testing.T) {
	a, p, b, e := conservativePiBilling(t)
	b = alteredJSON(b, "schema", "recharge-confirm-billing-attestation.v1")
	if _, err := CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("recharge billing schema accepted without fixed identity in legacy mode")
	}
}
