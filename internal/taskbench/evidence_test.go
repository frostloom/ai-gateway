package taskbench

import (
	"encoding/json"
	"testing"
)

func TestTaskReportRetainsCompleteTelemetry(t *testing.T) {
	var response Response
	json.Unmarshal([]byte(`{"trace":[{"tool":"recharge","preview":"5元","phase":"preview","resolved_args":"resolved","outcome":"waiting","duration_ms":2}],"telemetry":{"llm_calls":2,"llm_usage_missing":1,"llm_input_tokens":9,"jev_calls":1,"jev_errors":1,"runtime":{"model":"m"}}}`), &response)
	raw, _ := json.Marshal(response)
	var v map[string]any
	json.Unmarshal(raw, &v)
	tel := v["telemetry"].(map[string]any)
	if tel["llm_input_tokens"] != float64(9) || tel["runtime"] == nil {
		t.Fatalf("usage/config evidence lost: %s", raw)
	}
	if v["trace"].([]any)[0].(map[string]any)["preview"] != "5元" || v["trace"].([]any)[0].(map[string]any)["phase"] != "preview" {
		t.Fatalf("preview lost: %s", raw)
	}
}
func TestTaskUnknownUsageIsNotZero(t *testing.T) {
	m := Summarize([]Result{{Turns: []Turn{{Status: 502}, {Status: 200}}}})
	raw, _ := json.Marshal(m)
	var v map[string]any
	json.Unmarshal(raw, &v)
	if v["usage_status"] != "missing" || v["telemetry_missing_turns"] != float64(2) || v["cost_status"] != "not_evaluable" {
		t.Fatalf("unknown costs presented as zero: %s", raw)
	}
}
func TestTaskValidDenominatorIncludesHTTPFailuresExcludesSetup(t *testing.T) {
	m := Summarize([]Result{{Success: true}, {Turns: []Turn{{Status: 502}}}, {SetupError: true}})
	raw, _ := json.Marshal(m)
	var v map[string]any
	json.Unmarshal(raw, &v)
	if v["valid_tasks"] != float64(2) || v["valid_task_completion_rate"] != 0.5 {
		t.Fatalf("invalid denominator: %s", raw)
	}
}
func TestTaskStageUsageIncludesFailedAttempts(t *testing.T) {
	var response Response
	json.Unmarshal([]byte(`{"telemetry":{"llm_calls":2,"llm_usage_missing":2,"jev_calls":1,"jev_usage_missing":1,"llm_latency_ms":20,"jev_latency_ms":5,"tool_latency_ms":3}}`), &response)
	m := Summarize([]Result{{Turns: []Turn{{Stage: "confirm", Status: 502, LatencyMS: 30, Response: response}}}})
	raw, _ := json.Marshal(m)
	var v map[string]any
	json.Unmarshal(raw, &v)
	stages, ok := v["stage_latency"].(map[string]any)
	if !ok || stages["confirm"].(map[string]any)["average_ms"] != float64(30) {
		t.Fatalf("stage evidence lost: %s", raw)
	}
	if m.Usage.LLMCalls != 2 || m.Usage.LLMUsageMissing != 2 || m.Usage.ToolLatencyMS != 3 {
		t.Fatalf("failure stages dropped: %+v", m)
	}
}
func TestTaskLatencyExcludesInvalidInitialization(t *testing.T) {
	m := Summarize([]Result{{DurationMS: 100}, {SetupError: true}})
	if m.AllTasks.Count != 1 || m.AllTasks.AverageMS != 100 {
		t.Fatalf("setup failure polluted task latency: %+v", m.AllTasks)
	}
}
func TestTaskInvalidFixtureExcludedFromScenarioDenominators(t *testing.T) {
	m := Summarize([]Result{{SetupError: true, Case: Case{MissingSlot: "amount", Cancel: true, DuplicateConfirm: true}}})
	if m.ClarificationRequired != 0 || m.CancelCases != 0 || m.DuplicateConfirmCases != 0 {
		t.Fatalf("invalid fixture counted as business case: %+v", m)
	}
}
