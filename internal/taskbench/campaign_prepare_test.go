package taskbench

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func campaignPreparationInputs(t *testing.T) (ConversationSuite, PairedProfiles, CampaignPreparationPlan) {
	t.Helper()
	s, err := LoadConversations("../../eval/task-definitions/2026-10-12-full-dev-v1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadPairedProfiles("../../eval/runtime-profiles/2026-10-13-six-profiles-v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../eval/paired-pilots/2026-10-21-execution-envelope/execution-proposal.json")
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		Limits  CampaignLimits             `json:"proposed_campaign_limits"`
		Batches []CampaignPreparationBatch `json:"batches"`
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	return s, p, CampaignPreparationPlan{Schema: "campaign-preflight-plan.v1", Limits: old.Limits, Batches: old.Batches}
}

func TestPrepareCampaignProposalSixBatchesOffline(t *testing.T) {
	s, p, plan := campaignPreparationInputs(t)
	model := PairModelSettings{BaseURL: "https://provider.invalid/anthropic", APIKey: "private-marker-not-for-output", Model: "deepseek-v4.1-flash", Protocol: "anthropic"}
	before, _ := json.Marshal(s)
	got, err := PrepareCampaignProposal(s, p, model, "revision-one", "jev-1.13.0", plan)
	if err != nil {
		t.Fatal(err)
	}
	if got.AllowPaid || got.ReadyForPaidRun || got.ProviderCalls != 0 || got.AuthorizedSpending != nil || got.AccountTariffVerified || got.TokenEnvelopeVerified || got.JEVFailureChargesVerified || got.AutomaticScopeExpansion {
		t.Fatal("preparation implies execution, verification or authorization")
	}
	if got.PlannedCases != 6 || got.PlannedArms != 12 || got.PlannedUserTurns != 26 || got.ModelReservedMicroCNY != 811008 || got.JEVReservedMicroUSD != 16128 {
		t.Fatalf("incorrect totals: %+v", got)
	}
	modelCalls, jevCalls := 0, 0
	for i, b := range got.Batches {
		if b.Key != plan.Batches[i].Key || len(b.Configuration.Suite.Cases) != 1 || b.Configuration.Suite.Cases[0].Key() != b.Key || b.Configuration.AllowPaid || b.Contract != PairRunContract(b.Configuration) {
			t.Fatal("scope, order or binding differs")
		}
		modelCalls += b.ModelLimits.MaxCalls
		jevCalls += b.MaxJEVCalls
	}
	if modelCalls != 24 || jevCalls != 6 {
		t.Fatal("call totals differ")
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), model.APIKey) || strings.Contains(string(raw), model.BaseURL) {
		t.Fatal("serialized provider credential or endpoint")
	}
	other, err := PrepareCampaignProposal(s, p, model, "revision-two", "jev-1.13.0", plan)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got.Batches {
		if got.Batches[i].Contract == other.Batches[i].Contract {
			t.Fatal("revision did not change batch contract")
		}
	}
	got.Batches[0].Configuration.Suite.Cases[0].Scenario.Turns[0].User = "changed"
	after, _ := json.Marshal(s)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("proposal aliases source")
	}
}

func TestPrepareCampaignProposalRejectsInvalidScopeAndBudgets(t *testing.T) {
	tests := []struct {
		name   string
		change func(*CampaignPreparationPlan)
	}{
		{"schema", func(p *CampaignPreparationPlan) { p.Schema = "other" }},
		{"empty", func(p *CampaignPreparationPlan) { p.Batches = nil }},
		{"duplicate", func(p *CampaignPreparationPlan) { p.Batches[1].Key = p.Batches[0].Key }},
		{"missing", func(p *CampaignPreparationPlan) { p.Batches[0].Key = "unknown/missing" }},
		{"outside-small-pilot", func(p *CampaignPreparationPlan) { p.Batches[0].Key = "balance-002/answer_complete" }},
		{"batch-cap", func(p *CampaignPreparationPlan) { p.Limits.MaxBatches = 5 }},
		{"model-call-cap", func(p *CampaignPreparationPlan) { p.Limits.MaxModelCalls = 23 }},
		{"jev-call-cap", func(p *CampaignPreparationPlan) { p.Limits.MaxJEVCalls = 5 }},
		{"cny-cap", func(p *CampaignPreparationPlan) { p.Limits.MaxModelMicroCNY = 811007 }},
		{"usd-cap", func(p *CampaignPreparationPlan) { p.Limits.MaxJEVMicroUSD = 16127 }},
		{"child-cny-cap", func(p *CampaignPreparationPlan) { p.Batches[0].ModelLimits.MaxMicroCNY = 1 }},
		{"output-mismatch", func(p *CampaignPreparationPlan) { p.Batches[0].ModelLimits.MaxOutputTokens = 127 }},
		{"invalid-model-calls", func(p *CampaignPreparationPlan) { p.Batches[0].ModelLimits.MaxCalls = 257 }},
		{"invalid-jev-calls", func(p *CampaignPreparationPlan) { p.Batches[0].MaxJEVCalls = 257 }},
		{"missing-rate", func(p *CampaignPreparationPlan) { p.Batches[0].ModelLimits.InputMicroCNYPerMillion = 0 }},
		{"missing-usd-rate", func(p *CampaignPreparationPlan) { p.Limits.JEVMicroUSDPerAttempt = 0 }},
		{"huge-usd-rate", func(p *CampaignPreparationPlan) { p.Limits.JEVMicroUSDPerAttempt = 1 << 62 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, p, plan := campaignPreparationInputs(t)
			tt.change(&plan)
			_, err := PrepareCampaignProposal(s, p, PairModelSettings{BaseURL: "https://provider.invalid", Model: "model", Protocol: "anthropic"}, "revision", "jev-1.13.0", plan)
			if err == nil {
				t.Fatal("accepted invalid campaign")
			}
		})
	}
}
