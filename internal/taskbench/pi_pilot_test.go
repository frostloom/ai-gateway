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

// All approval and billing values in this helper are synthetic test inputs.
// Changing the real archived authorization is deliberately unnecessary.
func piPilotInputs(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	a, p, b, e := authorizedEntryInputs(t)
	var auth, plan, billing map[string]any
	json.Unmarshal(a, &auth)
	json.Unmarshal(p, &plan)
	json.Unmarshal(b, &billing)
	target := map[string]any{"base_url": "https://api.deepseek.com/v1", "model": "deepseek-flash", "protocol": "openai", "jev_model": "jev-1.13.0"}
	plan["schema"] = "campaign-preflight-plan.v2"
	plan["target"] = target
	p, _ = json.Marshal(plan)
	auth["schema"] = "small-pilot-authorization.v2"
	auth["model_base"] = target["base_url"]
	auth["model"] = target["model"]
	auth["protocol"] = target["protocol"]
	auth["scope_plan_sha256"] = conversationHash(p)
	a, _ = json.Marshal(auth)
	billing["model_base"] = target["base_url"]
	billing["model"] = target["model"]
	billing["protocol"] = target["protocol"]
	billing["scope_plan_sha256"] = conversationHash(p)
	b, _ = json.Marshal(billing)
	return a, p, b, e
}

func TestPiCampaignBindsTargetWithoutEnablingProviders(t *testing.T) {
	_, raw, _, _ := piPilotInputs(t)
	var plan CampaignPreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	source, profiles, _ := campaignPreparationInputs(t)
	model := PairModelSettings{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai", APIKey: "do-not-archive"}
	got, err := PrepareCampaignProposal(source, profiles, model, strings.Repeat("a", 40), "jev-1.13.0", plan)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlannedCases != 6 || got.PlannedArms != 12 || got.AllowPaid || got.ReadyForPaidRun || got.ProviderCalls != 0 || got.AuthorizedSpending != nil {
		t.Fatalf("wrong preparation: %+v", got)
	}
	for _, b := range got.Batches {
		if b.Configuration.Model.Model != "deepseek-flash" || b.Configuration.Model.ProviderProtocol != "openai" || b.Configuration.Model.OutputBudget != 128 {
			t.Fatal("configuration did not use Pi")
		}
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "do-not-archive") {
		t.Fatal("key leaked")
	}
	for _, field := range []string{"endpoint", "model", "protocol", "jev"} {
		changed := model
		j := "jev-1.13.0"
		switch field {
		case "endpoint":
			changed.BaseURL = "https://maas.qianwenaiapi.com/apps/anthropic"
		case "model":
			changed.Model = "deepseek-v4.1-flash"
		case "protocol":
			changed.Protocol = "anthropic"
		case "jev":
			j = "jev-latest"
		}
		if _, err := PrepareCampaignProposal(source, profiles, changed, strings.Repeat("a", 40), j, plan); err == nil {
			t.Fatalf("accepted mismatched %s", field)
		}
	}
}

func TestPiPilotDoesNotReuseLegacyPermissionOrBilling(t *testing.T) {
	a, p, b, e := piPilotInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, nil, nil)
	if err != nil || pilot.Ready() || len(pilot.Blockers()) != 3 {
		t.Fatalf("permission became billing evidence: %v", err)
	}
	if err := pilot.ValidateFrozenInputs("../../eval/task-definitions/2026-10-12-full-dev-v1", "../../eval/runtime-profiles/2026-10-13-six-profiles-v1", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if ready, err := CheckAuthorizedPilot(a, p, b, e); err != nil || !ready.Ready() {
		t.Fatalf("synthetic Pi attestation rejected: %v", err)
	}
	oldA, oldP, oldB, _ := authorizedEntryInputs(t)
	for _, tc := range []struct{ a, p, b []byte }{{oldA, p, b}, {a, oldP, b}, {a, p, oldB}} {
		if _, err := CheckAuthorizedPilot(tc.a, tc.p, tc.b, e); err == nil {
			t.Fatal("legacy binding accepted for Pi")
		}
	}
	var fields map[string]any
	json.Unmarshal(a, &fields)
	fields["human_authorized"] = false
	draft, _ := json.Marshal(fields)
	if _, err := CheckAuthorizedPilot(draft, p, b, e); err == nil {
		t.Fatal("unapproved draft enabled execution")
	}
}

func TestPiCampaignRejectsTargetSmuggling(t *testing.T) {
	_, raw, _, _ := piPilotInputs(t)
	source, profiles, _ := campaignPreparationInputs(t)
	for _, change := range []func(map[string]any){
		func(p map[string]any) { p["schema"] = "campaign-preflight-plan.v1" },
		func(p map[string]any) { delete(p, "target") },
		func(p map[string]any) {
			p["target"].(map[string]any)["base_url"] = "https://secret@api.deepseek.com/v1"
		},
		func(p map[string]any) { p["target"].(map[string]any)["model"] = "deepseek-v4-pro" },
		func(p map[string]any) { p["target"].(map[string]any)["protocol"] = "anthropic" },
		func(p map[string]any) { p["target"].(map[string]any)["jev_model"] = "jev-latest" },
	} {
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		change(fields)
		data, _ := json.Marshal(fields)
		var plan CampaignPreparationPlan
		json.Unmarshal(data, &plan)
		_, err := PrepareCampaignProposal(source, profiles, PairModelSettings{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai"}, strings.Repeat("a", 40), "jev-1.13.0", plan)
		if err == nil {
			t.Fatal("unbound or unsafe target accepted")
		}
	}
}

func TestPiFirstBatchUsesOpenAIAndStopsAtCheckpoint(t *testing.T) {
	o := localCampaignOptions(t)
	a, p, b, e := piPilotInputs(t)
	pilot, err := CheckAuthorizedPilot(a, p, b, e)
	if err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "upstream-failure"}[fail], func(t *testing.T) {
			d, err := newLocalCampaignDependencies(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer d.close()
			source, _ := LoadConversations(o.DefinitionsDir)
			small, _ := PrepareSmallPair(source)
			var first ConversationCase
			for _, c := range small.Suite.Cases {
				if c.Key() == "balance-001/answer_complete" {
					first = c
				}
			}
			n := 0
			upstream := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				n++
				if req.URL.String() != "https://api.deepseek.com/v1/chat/completions" {
					t.Fatalf("wrong endpoint %s", req.URL)
				}
				var body struct {
					Model    string                           `json:"model"`
					Max      int                              `json:"max_tokens"`
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if json.NewDecoder(req.Body).Decode(&body) != nil || body.Model != "deepseek-flash" || body.Max != 128 {
					t.Fatal("request lost model/output bound")
				}
				if fail {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"private-provider-error"}`))}, nil
				}
				message := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "read-balance", "type": "function", "function": map[string]string{"name": "get_balance", "arguments": "{}"}}}}
				last := body.Messages[len(body.Messages)-1]
				if last.Role == "tool" {
					message = map[string]any{"role": "assistant", "content": last.Content}
				}
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": message}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})
			dir := filepath.Join(t.TempDir(), "run")
			opts := AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: d.observer, Model: PairModelSettings{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai", APIKey: "synthetic-test-key"}, Upstream: upstream, JEVFactory: func() jev.Evaluator { return authorizedTestJEV{d.judge(first), true} }}
			receipt, err := RunAuthorizedFirstBatch(context.Background(), dir, opts)
			if fail {
				if err == nil || receipt.Status != "stopped" || n != 1 {
					t.Fatalf("failure retried or accepted: %v %+v calls=%d", err, receipt, n)
				}
			} else {
				if err != nil || receipt.Status != "checkpoint_pending" || n != 2 || receipt.JEVAttempts != 1 || receipt.Report == nil || receipt.Report.FinishedArms != 2 {
					t.Fatalf("incomplete Pi batch: %v %+v calls=%d", err, receipt, n)
				}
				for _, row := range receipt.Report.Rows {
					if row.Status != "needs_review" || row.FactErrors != 0 || row.LangfuseTraceID == "" {
						t.Fatalf("missing tool/trace evidence %+v", row)
					}
				}
			}
			if len(receipt.Batches) != 6 || receipt.Batches[1].Status != "not_started" {
				t.Fatal("continued beyond batch one")
			}
			if _, err := os.Stat(filepath.Join(dir, "campaign", "batch-002")); !os.IsNotExist(err) {
				t.Fatal("reserved next batch")
			}
			before := n
			if _, err := RunAuthorizedFirstBatch(context.Background(), dir, opts); err == nil || n != before {
				t.Fatal("repeated upstream on reused output")
			}
		})
	}
}
