package taskbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func conservativePiBilling(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	a, p, b, e := piPilotInputs(t)
	var fields map[string]any
	json.Unmarshal(b, &fields)
	fields["schema"] = "paired-billing-attestation.v2"
	fields["jev_failure_charges_verified"] = false
	fields["jev_failure_policy"] = "reserve_full_attempt_stop_on_failure"
	fields["jev_max_input_tokens"] = 64000
	fields["jev_input_micro_usd_per_million"] = 42000
	fields["jev_full_attempt_micro_usd"] = 2688
	b, _ = json.Marshal(fields)
	return a, p, b, e
}

type policyFailureJEV struct{}

func (policyFailureJEV) Evaluate(context.Context, any, map[string]jev.Question) (*jev.Result, error) {
	return nil, jev.NewFailure("timeout", 0, true)
}

func TestConservativePolicyStopsModelFallbackAfterUnknownJEVCharge(t *testing.T) {
	o := localCampaignOptions(t)
	d, err := newLocalCampaignDependencies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	a, p, b, e := conservativePiBilling(t)
	pilot, err := CheckAuthorizedPilot(a, p, b, e)
	if err != nil {
		t.Fatal(err)
	}
	modelCalls := 0
	upstream := pairedRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		modelCalls++
		var payload struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		message := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "balance", "type": "function", "function": map[string]string{"name": "get_balance", "arguments": "{}"}}}}
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role == "tool" {
			message = map[string]any{"role": "assistant", "content": last.Content}
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": message}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 8}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
	receipt, err := RunAuthorizedFirstBatch(context.Background(), filepath.Join(t.TempDir(), "first"), AuthorizedFirstBatchOptions{Pilot: pilot, DefinitionsDir: o.DefinitionsDir, Environment: o.Environment, Revision: strings.Repeat("a", 40), Observer: d.observer, Model: PairModelSettings{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai", APIKey: "synthetic"}, Upstream: upstream, JEVFactory: func() jev.Evaluator { return policyFailureJEV{} }})
	if err == nil || receipt.Status != "stopped" || receipt.JEVAttempts != 1 {
		t.Fatalf("unknown charge accepted: %+v %v", receipt, err)
	}
	if modelCalls != 2 {
		t.Fatalf("JEV failure triggered paid model fallback: model calls=%d, want 2 from first arm only", modelCalls)
	}
	if receipt.ModelState.Stopped == "" {
		t.Fatal("model budget did not record failure stop")
	}
	if receipt.Batches[1].Status != "not_started" {
		t.Fatal("later batch started")
	}
}

func TestPiConservativeFailurePolicyDoesNotPretendVendorGuarantee(t *testing.T) {
	a, p, b, e := conservativePiBilling(t)
	pilot, err := CheckAuthorizedPilot(a, p, b, e)
	if err != nil || !pilot.Ready() {
		t.Fatalf("reviewed full-attempt reservation rejected: %v %v", err, pilot.Blockers())
	}
	var fields map[string]any
	json.Unmarshal(b, &fields)
	if fields["jev_failure_charges_verified"] != false {
		t.Fatal("test must retain unknown vendor failure terms")
	}
	for _, key := range []string{"jev_failure_policy", "jev_max_input_tokens", "jev_input_micro_usd_per_million", "jev_full_attempt_micro_usd", "evidence_sha256", "account_tariff_verified", "token_envelope_verified"} {
		t.Run(key, func(t *testing.T) {
			var changed map[string]any
			json.Unmarshal(b, &changed)
			delete(changed, key)
			raw, _ := json.Marshal(changed)
			got, err := CheckAuthorizedPilot(a, p, raw, e)
			if err == nil && got.Ready() {
				t.Fatal("incomplete policy/evidence accepted")
			}
		})
	}
	if got, err := CheckAuthorizedPilot(a, p, b, []byte("changed evidence")); err == nil && got.Ready() {
		t.Fatal("changed evidence accepted")
	}
}

func TestConservativeFailurePolicyCannotUpgradeLegacyBilling(t *testing.T) {
	a, p, b, e := authorizedEntryInputs(t)
	var fields map[string]any
	json.Unmarshal(b, &fields)
	fields["schema"] = "paired-billing-attestation.v2"
	fields["jev_failure_charges_verified"] = false
	fields["jev_failure_policy"] = "reserve_full_attempt_stop_on_failure"
	fields["jev_max_input_tokens"] = 64000
	fields["jev_input_micro_usd_per_million"] = 42000
	fields["jev_full_attempt_micro_usd"] = 2688
	b, _ = json.Marshal(fields)
	if got, err := CheckAuthorizedPilot(a, p, b, e); err == nil && got.Ready() {
		t.Fatal("Pi policy changed old Qwen approval")
	}
}

func TestConservativeFailureHaltSurvivesBudgetReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.jsonl")
	calls := 0
	upstream := pairedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":100,"completion_tokens":10}}`))}, nil
	})
	b, err := OpenPairBudget(path, "conservative-policy-test", pairedLimits(), upstream)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	b.setCompanion(func() PairJEVBudgetState { return PairJEVBudgetState{Stopped: stopped} }, true)
	response, err := b.RoundTrip(pairedRequest())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	stopped = true
	if _, err := b.RoundTrip(pairedRequest()); err == nil || calls != 1 {
		t.Fatal("JEV failure did not stop next model attempt")
	}
	before := b.Snapshot()
	if before.CostMicroCNY == nil || before.Calls != 1 || before.Stopped == "" {
		t.Fatalf("lost known model usage: %+v", before)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b, err = OpenPairBudget(path, "conservative-policy-test", pairedLimits(), upstream)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.RoundTrip(pairedRequest()); err == nil || calls != 1 {
		t.Fatal("reopening budget erased failure halt")
	}
	if after := b.Snapshot(); after.ReservedMicroCNY != before.ReservedMicroCNY || after.CostMicroCNY == nil || *after.CostMicroCNY != *before.CostMicroCNY {
		t.Fatal("halt changed existing model reservations/usage")
	}
}
