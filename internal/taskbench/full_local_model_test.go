package taskbench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func fullLocalRequest(t *testing.T, user string, result bool) *http.Request {
	t.Helper()
	content := []any{map[string]any{"type": "text", "text": user}}
	messages := []any{map[string]any{"role": "user", "content": content}}
	if result {
		messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "full-local-call", "content": "实际账单结果"}}})
	}
	raw, err := json.Marshal(map[string]any{"model": "local-full-customer", "messages": messages, "max_tokens": 128, "thinking": map[string]string{"type": "disabled"}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest("POST", "http://127.0.0.1/v1/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func localModelReply(t *testing.T, m *fullLocalModel, request *http.Request) map[string]any {
	t.Helper()
	r, err := m.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var value map[string]any
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFullLocalModelUsesFrozenReadAndWriteParameters(t *testing.T) {
	read := ConversationCase{SourceID: "read", Scenario: ConversationScenario{Turns: []ConversationRule{{User: "查十笔", ExpectedActions: []string{"read"}, AllowedTools: []string{"get_recent_bills"}}}}}
	m := &fullLocalModel{c: read, readArgs: map[string]any{"limit": float64(10)}}
	reply := localModelReply(t, m, fullLocalRequest(t, "查十笔", false))
	call := reply["content"].([]any)[0].(map[string]any)
	if call["name"] != "get_recent_bills" || call["input"].(map[string]any)["limit"] != float64(10) {
		t.Fatal("read parameters guessed or lost", call)
	}
	for _, tc := range []struct {
		tool string
		args map[string]any
		want map[string]any
	}{
		{"refund_recharge", map[string]any{"amount_money": float64(123)}, map[string]any{"amount_money": float64(123)}},
		{"subscribe_plan", map[string]any{"plan_reference": "flagship"}, map[string]any{"plan_name": "月度·旗舰"}},
		{"change_plan", map[string]any{"plan_reference": "flagship"}, map[string]any{"plan_name": "月度·旗舰"}},
		{"cancel_subscription", map[string]any{"plan_reference": "standard"}, map[string]any{}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			c := ConversationCase{SourceID: "write", Scenario: ConversationScenario{Operation: &ConversationOperation{Tool: tc.tool, Arguments: tc.args}, Turns: []ConversationRule{{User: "明确申请", ExpectedActions: []string{"read", "preview"}, AllowedTools: []string{tc.tool}}}}}
			m := &fullLocalModel{c: c, planNames: map[string]string{"flagship": "月度·旗舰", "standard": "月度·标准"}}
			r := localModelReply(t, m, fullLocalRequest(t, "明确申请", false))
			call := r["content"].([]any)[0].(map[string]any)
			got, _ := json.Marshal(call["input"])
			want, _ := json.Marshal(tc.want)
			if call["name"] != tc.tool || !bytes.Equal(got, want) {
				t.Fatalf("wrong source arguments: %s want %s", got, want)
			}
		})
	}
	result := localModelReply(t, m, fullLocalRequest(t, "查十笔", true))
	if result["stop_reason"] != "end_turn" || result["content"].([]any)[0].(map[string]any)["text"] != "实际账单结果" {
		t.Fatal("tool result not used")
	}
}

func TestFullLocalModelNoToolBranchesDoNotInventExecution(t *testing.T) {
	for _, action := range []string{"clarify", "respond", "consult", "limited", "reject"} {
		m := &fullLocalModel{c: ConversationCase{SourceID: action, Scenario: ConversationScenario{Turns: []ConversationRule{{User: "原始话语", ExpectedActions: []string{action}}}}}}
		r := localModelReply(t, m, fullLocalRequest(t, "原始话语", false))
		if r["stop_reason"] != "end_turn" {
			t.Fatal("no-tool branch invented a tool")
		}
		if block := r["content"].([]any)[0].(map[string]any); block["type"] != "text" || block["text"] == "" {
			t.Fatal("missing explicit synthetic text")
		}
	}
}

func TestFullLocalModelRejectsExternalMalformedAndUnknownTurn(t *testing.T) {
	m := &fullLocalModel{c: ConversationCase{Scenario: ConversationScenario{Turns: []ConversationRule{{User: "已知", ExpectedActions: []string{"read"}, AllowedTools: []string{"get_balance"}}}}}}
	for _, kind := range []string{"external", "wrong-path", "wrong-method", "malformed", "unknown-turn", "cancelled", "wrong-budget", "wrong-model", "thinking"} {
		t.Run(kind, func(t *testing.T) {
			r := fullLocalRequest(t, "已知", false)
			switch kind {
			case "external":
				r.URL.Host = "api.deepseek.com"
			case "wrong-path":
				r.URL.Path = "/other"
			case "wrong-method":
				r.Method = "GET"
			case "malformed":
				r.Body = io.NopCloser(bytes.NewBufferString("{"))
			case "unknown-turn":
				r = fullLocalRequest(t, "未知", false)
			case "cancelled":
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			default:
				raw, _ := io.ReadAll(r.Body)
				if kind == "wrong-budget" {
					raw = bytes.ReplaceAll(raw, []byte(`"max_tokens":128`), []byte(`"max_tokens":129`))
				}
				if kind == "wrong-model" {
					raw = bytes.ReplaceAll(raw, []byte("local-full-customer"), []byte("other-model"))
				}
				if kind == "thinking" {
					raw = bytes.ReplaceAll(raw, []byte("disabled"), []byte("enabled"))
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
			}
			if response, err := m.RoundTrip(r); err == nil {
				if response != nil {
					response.Body.Close()
				}
				t.Fatal("closed script accepted invalid request")
			}
		})
	}
}
