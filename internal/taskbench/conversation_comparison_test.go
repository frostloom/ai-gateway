package taskbench

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func comparisonInput() (ConversationSuite, ConversationComparisonRun, ConversationComparisonRun) {
	c := rechargeConversation()
	h := strings.Repeat("a", 64)
	suite := ConversationSuite{DefinitionSHA256: h, ManifestSHA256: h, Cases: []ConversationCase{c}}
	cfg := ConversationComparisonConfig{RoutingMode: "model", EvidenceKind: "local_stub", DefinitionSHA256: h, ManifestSHA256: h, ProfileSHA256: map[string]string{c.Scenario.Profile: h}, Model: "test-model", ProviderProtocol: "openai", PromptSHA256: h, ToolSchemaSHA256: h, OutputBudget: 128, ExecutionRevision: "revision-1"}
	r := RunConversation(context.Background(), c, rechargeDriver())
	for i := range r.Observations {
		r.Observations[i].Turn.TelemetryRaw = json.RawMessage(`{"total_latency_ms":12,"llm_calls":1,"jev_calls":0}`)
	}
	a := ConversationComparisonRun{Config: cfg, Results: []ConversationResult{r}}
	var b ConversationComparisonRun
	raw, _ := json.Marshal(a)
	_ = json.Unmarshal(raw, &b)
	b.Config.RoutingMode = "jev_selective"
	for _, run := range []*ConversationComparisonRun{&a, &b} {
		for i := range run.Results[0].Observations {
			run.Results[0].Observations[i].Turn.TelemetryRaw = comparisonRuntimeTelemetry(run.Config, run.Results[0].Observations[i].Turn.TelemetryRaw)
		}
	}
	return suite, a, b
}

func comparisonRuntimeTelemetry(cfg ConversationComparisonConfig, telemetry json.RawMessage) json.RawMessage {
	var raw map[string]any
	_ = json.Unmarshal(telemetry, &raw)
	raw["runtime"] = map[string]any{"model": cfg.Model, "protocol": cfg.ProviderProtocol, "routing_mode": cfg.RoutingMode, "prompt_sha256": cfg.PromptSHA256, "tools_sha256": cfg.ToolSchemaSHA256, "max_output_tokens": cfg.OutputBudget, "endpoint_sha256": strings.Repeat("c", 64), "contract_sha256": strings.Repeat("d", 64), "reply_repair_limit": 1, "thresholds": map[string]float64{"intent_min": 0.9}}
	out, _ := json.Marshal(raw)
	return out
}

func TestConversationComparisonRejectsRuntimeMismatchAndImpossibleModelJEV(t *testing.T) {
	for _, kind := range []string{"routing_mode", "model", "protocol", "jev_calls", "jev_fast_path"} {
		t.Run(kind, func(t *testing.T) {
			s, a, b := comparisonInput()
			var raw map[string]any
			_ = json.Unmarshal(a.Results[0].Observations[0].Turn.TelemetryRaw, &raw)
			if kind == "jev_calls" {
				raw[kind] = 1
			} else if kind == "jev_fast_path" {
				raw[kind] = true
			} else {
				raw["runtime"].(map[string]any)[kind] = "wrong"
			}
			a.Results[0].Observations[0].Turn.TelemetryRaw, _ = json.Marshal(raw)
			if _, err := CompareConversations(s, a, b); err == nil {
				t.Fatal("accepted contradictory runtime telemetry")
			}
		})
	}
}

func TestConversationComparisonMissingRuntimeDisablesEfficiency(t *testing.T) {
	s, a, b := comparisonInput()
	a.Results[0].Observations[0].Turn.TelemetryRaw = json.RawMessage(`{"total_latency_ms":12,"llm_calls":1,"jev_calls":0}`)
	r, err := CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model.ResponseLatencyMS != nil || r.Model.LLMCalls != nil || r.Model.Tokens != nil {
		t.Fatal("missing runtime produced comparable efficiency")
	}
}

func TestConversationComparisonReviewedFalseBalanceFailsTask(t *testing.T) {
	s, a, b := comparisonInput()
	for _, run := range []*ConversationComparisonRun{&a, &b} {
		run.Config.EvidenceKind = "recorded_run"
		run.Results[0].Observations[2].Turn.Response.Reply = "余额：99999元"
		d := &replayConversationDriver{episode: ConversationEpisode{Fixture: run.Results[0].Fixture, Initial: run.Results[0].Initial, Steps: run.Results[0].Observations, Final: run.Results[0].Final}}
		r := RunConversation(context.Background(), s.Cases[0], d)
		for _, req := range r.ReviewRequirements {
			obs := &run.Results[0].Observations[req.TurnIndex]
			obs.Reviews = append(obs.Reviews, ConversationReview{ID: req.ID, RequirementSHA256: req.RequirementSHA256, ReplySHA256: req.ReplySHA256, Verdict: "pass", Reviewer: "auditor", Method: "audited_offline", Reason: "deliberately incorrect semantic review"})
		}
	}
	r, err := CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model.CompletionRate == nil || *r.Model.CompletionRate != 0 || r.Selective.CompletionRate == nil || *r.Selective.CompletionRate != 0 {
		t.Fatalf("false reviewed balance produced success: %+v", r)
	}
}

func TestConversationComparisonRejectsDifferentConditions(t *testing.T) {
	changes := map[string]func(*ConversationComparisonConfig){
		"route":            func(c *ConversationComparisonConfig) { c.RoutingMode = "jev" },
		"evidence":         func(c *ConversationComparisonConfig) { c.EvidenceKind = "recorded_run" },
		"model":            func(c *ConversationComparisonConfig) { c.Model = "other" },
		"protocol":         func(c *ConversationComparisonConfig) { c.ProviderProtocol = "anthropic" },
		"invalid_protocol": func(c *ConversationComparisonConfig) { c.ProviderProtocol = "unknown" },
		"prompt":           func(c *ConversationComparisonConfig) { c.PromptSHA256 = strings.Repeat("b", 64) },
		"schema":           func(c *ConversationComparisonConfig) { c.ToolSchemaSHA256 = "" },
		"budget":           func(c *ConversationComparisonConfig) { c.OutputBudget++ },
		"revision":         func(c *ConversationComparisonConfig) { c.ExecutionRevision = "" },
		"profile":          func(c *ConversationComparisonConfig) { c.ProfileSHA256 = map[string]string{} },
		"definition":       func(c *ConversationComparisonConfig) { c.DefinitionSHA256 = strings.Repeat("b", 64) },
		"manifest":         func(c *ConversationComparisonConfig) { c.ManifestSHA256 = "" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			s, a, b := comparisonInput()
			change(&b.Config)
			if _, err := CompareConversations(s, a, b); err == nil {
				t.Fatal("accepted incompatible conditions")
			}
		})
	}
}

func TestConversationComparisonRejectsUnknownProtocolOnBothSides(t *testing.T) {
	s, a, b := comparisonInput()
	a.Config.ProviderProtocol = "unknown"
	b.Config.ProviderProtocol = "unknown"
	if _, err := CompareConversations(s, a, b); err == nil {
		t.Fatal("accepted unknown provider protocol")
	}
}

func TestConversationComparisonRecomputesQualityAndSeparatesTiming(t *testing.T) {
	s, a, b := comparisonInput()
	a.Results[0].Success = true
	a.Results[0].Status = "passed"
	a.Results[0].ReviewRequirements = nil
	a.Results[0].DurationMS = 99999
	report, err := CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if report.Model.CompletionRate != nil || report.Selective.CompletionRate != nil || report.QualityComparable {
		t.Fatal("unreviewed synthetic quality was published")
	}
	if report.Model.ResponseLatencyMS == nil || *report.Model.ResponseLatencyMS != 36 || report.Model.Tokens != nil {
		t.Fatalf("telemetry or timing confusion: %+v", report.Model)
	}
	if report.EffectClaimsAllowed {
		t.Fatal("local stubs cannot claim model benefits")
	}
}

func TestConversationComparisonRejectsMissingReorderedOrChangedCases(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "scope", "profile", "definition"} {
		t.Run(mode, func(t *testing.T) {
			s, a, b := comparisonInput()
			switch mode {
			case "missing":
				b.Results = nil
			case "duplicate":
				b.Results = append(b.Results, b.Results[0])
			case "scope":
				b.Results[0].Case.Scenario.MeasurementScope = "first_turn_diagnostic"
			case "profile":
				b.Results[0].Case.Scenario.Profile = "low_balance"
			case "definition":
				b.Results[0].Case.Scenario.Turns[0].User = "changed"
			}
			if _, err := CompareConversations(s, a, b); err == nil {
				t.Fatal("accepted changed selection")
			}
		})
	}
	s, a, b := comparisonInput()
	c := s.Cases[0]
	c.SourceID = "second"
	s.Cases = append(s.Cases, c)
	r := a.Results[0]
	r.Case = c
	a.Results = append(a.Results, r)
	b.Results = []ConversationResult{r, b.Results[0]}
	if _, err := CompareConversations(s, a, b); err == nil {
		t.Fatal("accepted reordered cases")
	}
}

func TestConversationComparisonRequiresCompletePairedTelemetry(t *testing.T) {
	s, a, b := comparisonInput()
	b.Results[0].Observations[0].Turn.TelemetryRaw = nil
	report, err := CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if report.Model.ResponseLatencyMS != nil || report.Selective.ResponseLatencyMS != nil || report.Model.LLMCalls != nil {
		t.Fatal("published incomplete paired telemetry")
	}
}

func TestConversationComparisonRejectsSyntheticReviewAsRecordedRun(t *testing.T) {
	s, a, b := comparisonInput()
	a.Config.EvidenceKind = "recorded_run"
	b.Config.EvidenceKind = "recorded_run"
	a.Results[0].Observations[0].Reviews = []ConversationReview{{Method: "local_stub"}}
	if _, err := CompareConversations(s, a, b); err == nil {
		t.Fatal("synthetic review passed as a recorded run")
	}
}

func TestConversationComparisonRecordedQualityNeedsIndependentBoundReviews(t *testing.T) {
	s, a, b := comparisonInput()
	a.Config.EvidenceKind = "recorded_run"
	b.Config.EvidenceKind = "recorded_run"
	for _, run := range []*ConversationComparisonRun{&a, &b} {
		for _, req := range run.Results[0].ReviewRequirements {
			obs := &run.Results[0].Observations[req.TurnIndex]
			obs.Reviews = append(obs.Reviews, ConversationReview{ID: req.ID, RequirementSHA256: req.RequirementSHA256, ReplySHA256: req.ReplySHA256, Verdict: "pass", Reviewer: "independent-auditor", Method: "audited_offline", Reason: "fixture demonstrates digest binding only"})
		}
	}
	report, err := CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !report.QualityComparable || report.Model.CompletionRate == nil || *report.Model.CompletionRate != 1 {
		t.Fatalf("bound reviews unavailable: %+v", report)
	}
	b.Results[0].Observations[0].Reviews = nil
	report, err = CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if report.QualityComparable || report.Model.CompletionRate != nil {
		t.Fatal("one pending review must disable paired quality")
	}
}

func TestConversationComparisonCompleteTokensAndInvalidTelemetry(t *testing.T) {
	s, a, b := comparisonInput()
	complete := json.RawMessage(`{"total_latency_ms":12,"llm_calls":1,"jev_calls":0,"llm_usage_missing":0,"llm_usage_responses":1,"llm_input_tokens":10,"llm_output_tokens":2,"jev_usage_missing":0,"jev_usage_responses":0,"jev_input_tokens":0,"jev_output_tokens":0}`)
	for _, run := range []*ConversationComparisonRun{&a, &b} {
		for i := range run.Results[0].Observations {
			run.Results[0].Observations[i].Turn.TelemetryRaw = comparisonRuntimeTelemetry(run.Config, complete)
		}
	}
	report, err := CompareConversations(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if report.Model.Tokens == nil || report.Model.Tokens.LLMInput != 30 || report.Model.Tokens.LLMOutput != 6 {
		t.Fatalf("complete telemetry lost: %+v", report)
	}
	for _, raw := range []string{`{"total_latency_ms":-1,"llm_calls":-1,"jev_calls":0}`, `{"total_latency_ms":null,"llm_calls":1.5,"jev_calls":0}`, `null`} {
		b.Results[0].Observations[0].Turn.TelemetryRaw = json.RawMessage(raw)
		report, err = CompareConversations(s, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if report.Model.Tokens != nil || report.Model.LLMCalls != nil || report.Model.ResponseLatencyMS != nil {
			t.Fatal("invalid paired telemetry published")
		}
	}
}
