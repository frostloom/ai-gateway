package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func retestInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	p, err := os.ReadFile("../../eval/paired-pilots/2026-10-23-cash-read-retest-offline/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan CampaignPreparationPlan
	if json.Unmarshal(p, &plan) != nil {
		t.Fatal("plan")
	}
	a, _ := json.Marshal(map[string]any{"schema": "cash-read-retest-authorization.v1", "campaign_id": "cash-read-retest-20261023", "human_authorized": true, "scope_plan_sha256": conversationHash(p), "target": plan.Target, "authorized_spending": plan.Limits, "case_key": "balance-020/explicit_scope_followup", "execution_revision": strings.Repeat("a", 40), "prior_journal_sha256": "93a6d5df6c9d68fd5202dd023424a8af77de5a1170dd01993e7334b4804b66d2", "prior_review_sha256": "d0ea747fd5f59d10382b8b0cf1b6d94d7df8e68c739a64df8b4c8289b230b673", "automatic_scope_expansion": false, "automatic_paid_retries": false})
	evidence := []byte("synthetic retest operator evidence, not supplier proof")
	b, _ := json.Marshal(map[string]any{"schema": "paired-retest-billing-attestation.v1", "campaign_id": "cash-read-retest-20261023", "execution_revision": strings.Repeat("a", 40), "scope_plan_sha256": conversationHash(p), "model_base": plan.Target.BaseURL, "model": plan.Target.Model, "protocol": plan.Target.Protocol, "jev_model": plan.Target.JEVModel, "account_tariff_verified": true, "token_envelope_verified": true, "jev_failure_charges_verified": false, "jev_failure_policy": "reserve_full_attempt_stop_on_failure", "jev_max_input_tokens": 64000, "jev_input_micro_usd_per_million": 42000, "jev_full_attempt_micro_usd": 2688, "evidence_sha256": conversationHash(evidence)})
	return a, p, b, evidence
}
func alteredJSON(raw []byte, key string, value any) []byte {
	var m map[string]any
	json.Unmarshal(raw, &m)
	m[key] = value
	out, _ := json.Marshal(m)
	return out
}
func TestRetestScopeConsentAndBindings(t *testing.T) {
	a, p, b, e := retestInputs(t)
	pilot, err := CheckAuthorizedRetest(a, p, b, e)
	if err != nil || !pilot.Ready() || !pilot.IsRetest() || pilot.CampaignID() != "cash-read-retest-20261023" {
		t.Fatalf("bound scope rejected: %v", err)
	}
	unapproved, err := CheckAuthorizedRetest(alteredJSON(a, "human_authorized", false), p, b, e)
	if err != nil || unapproved.Ready() || unapproved.HumanAuthorized() {
		t.Fatal("unapproved check enabled execution")
	}
	if _, err = CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("retest authorization accepted by legacy mode")
	}
	oldA, oldP, oldB, oldE := authorizedEntryInputs(t)
	if _, err = CheckAuthorizedRetest(oldA, oldP, oldB, oldE); err == nil {
		t.Fatal("legacy authorization accepted by retest")
	}
	for _, key := range []string{"schema", "campaign_id", "scope_plan_sha256", "case_key", "execution_revision", "prior_journal_sha256", "prior_review_sha256", "target", "authorized_spending"} {
		if _, err := CheckAuthorizedRetest(alteredJSON(a, key, "changed"), p, b, e); err == nil {
			t.Fatalf("accepted altered %s", key)
		}
	}
	for _, key := range []string{"automatic_scope_expansion", "automatic_paid_retries"} {
		if _, err := CheckAuthorizedRetest(alteredJSON(a, key, true), p, b, e); err == nil {
			t.Fatal(key)
		}
	}
	for _, key := range []string{"schema", "campaign_id", "execution_revision", "scope_plan_sha256", "evidence_sha256", "model_base", "jev_failure_policy"} {
		if _, err := CheckAuthorizedRetest(a, p, alteredJSON(b, key, "changed"), e); err == nil {
			t.Fatal(key)
		}
	}
	for _, key := range []string{"account_tariff_verified", "token_envelope_verified"} {
		x, err := CheckAuthorizedRetest(a, p, alteredJSON(b, key, false), e)
		if err != nil || x.Ready() {
			t.Fatal("billing missing enabled execution", key, err)
		}
	}
	x, err := CheckAuthorizedRetest(a, p, nil, nil)
	if err != nil || x.Ready() {
		t.Fatal("missing billing enabled execution")
	}
	if _, err := CheckAuthorizedRetest(a, p, b, []byte("changed")); err == nil {
		t.Fatal("changed billing source accepted")
	}
	if pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("b", 40)) == nil {
		t.Fatal("wrong execution revision accepted")
	}
	dir := filepath.Join(t.TempDir(), "never")
	if _, err := RunAuthorizedFirstBatch(context.Background(), dir, AuthorizedFirstBatchOptions{Pilot: pilot}); err == nil {
		t.Fatal("wrong runner mode accepted retest")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid mode created output")
	}
}

func TestRetestLocalHandlerOneCaseAndFailureStop(t *testing.T) {
	for _, mode := range []string{"complete", "provider-failure", "jev-failure"} {
		t.Run(mode, func(t *testing.T) {
			failure := mode == "provider-failure"
			o := localCampaignOptions(t)
			a, p, b, e := retestInputs(t)
			pilot, err := CheckAuthorizedRetest(a, p, b, e)
			if err != nil {
				t.Fatal(err)
			}
			d, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer d.close()
			source, _ := LoadConversations(o.DefinitionsDir)
			small, _ := PrepareSmallPair(source)
			var c ConversationCase
			for _, v := range small.Suite.Cases {
				if v.Key() == "balance-020/explicit_scope_followup" {
					c = v
				}
			}
			calls := 0
			up := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
					t.Fatal("wrong target")
				}
				calls++
				if failure {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"test"}`))}, nil
				}
				var body struct {
					Messages []struct {
						Role string `json:"role"`
					}
					Max int `json:"max_tokens"`
				}
				if json.NewDecoder(req.Body).Decode(&body) != nil || body.Max != 128 {
					t.Fatal("output bound missing")
				}
				msg := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "read", "type": "function", "function": map[string]string{"name": "get_balance", "arguments": "{}"}}}}
				if len(body.Messages) > 0 && body.Messages[len(body.Messages)-1].Role == "tool" {
					msg = map[string]any{"role": "assistant", "content": "当前现金余额 ¥5000.00，非套餐token额度。"}
				}
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: d.observer, Model: pilot.ModelSettings(), Upstream: up, JEVFactory: func() jev.Evaluator {
				if mode == "jev-failure" {
					return policyFailureJEV{}
				}
				return authorizedTestJEV{d.judge(c), true}
			}}
			opts.Model.APIKey = "synthetic"
			dir := filepath.Join(t.TempDir(), "retest")
			r, err := RunAuthorizedRetest(context.Background(), dir, opts)
			if mode != "complete" {
				if err == nil || r.Status != "stopped" {
					t.Fatal("failure did not stop")
				}
			} else {
				if err != nil || r.Status != "checkpoint_pending" || len(r.Batches) != 1 || r.Schema != "authorized-cash-retest.v1" || r.CampaignID != "cash-read-retest-20261023" || r.CampaignReservedModelCalls != 12 || r.ModelState.Calls > 12 || r.JEVAttempts > 1 || r.Report == nil || r.Report.FinishedArms != 2 {
					t.Fatalf("bad bounded run %+v %v", r, err)
				}
			}

			if mode == "jev-failure" && r.JEVAttempts != 1 {
				t.Fatal("JEV failure retried or uncounted")
			}
			if mode == "complete" {
				journal, err := loadPairJournal(filepath.Join(dir, "campaign", "batch-001", "run.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				if len(journal.finished) != 2 {
					t.Fatal("missing paired arms")
				}
				for _, event := range journal.finished {
					if event.Result == nil || len(event.Result.Observations) != 2 {
						t.Fatal("missing two turns")
					}
					for _, obs := range event.Result.Observations {
						read := false
						for _, trace := range obs.Turn.Response.Trace {
							if trace.Tool == "get_balance" && trace.Outcome == "success" {
								read = true
							}
						}
						if !read {
							t.Fatal("turn lacks fresh successful cash read")
						}
					}
				}
			}
			before := calls
			if _, err := RunAuthorizedRetest(context.Background(), dir, opts); err == nil || calls != before {
				t.Fatal("reused output ran providers")
			}
		})
	}
}

func TestRetestBillingSchemaCannotAuthorizeLegacyWithoutIdentity(t *testing.T) {
	a, p, b, e := conservativePiBilling(t)
	b = alteredJSON(b, "schema", "paired-retest-billing-attestation.v1")
	if _, err := CheckAuthorizedPilot(a, p, b, e); err == nil {
		t.Fatal("retest billing accepted without identity in legacy mode")
	}
}
