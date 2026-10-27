package taskbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

// fullLocalModel is deliberately source-aware scaffolding, not an evaluator or
// a model. It has no network transport and cannot fall back to a real provider.
type fullLocalModel struct {
	c         ConversationCase
	readArgs  map[string]any
	planNames map[string]string
}

type fullLocalContent struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Content   string `json:"content"`
	ToolUseID string `json:"tool_use_id"`
}

func (m *fullLocalModel) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.Body == nil {
		return nil, fmt.Errorf("missing local request")
	}
	defer r.Body.Close()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if r.URL == nil || r.Method != "POST" || r.URL.Scheme != "http" || r.URL.Hostname() != "127.0.0.1" || r.URL.Path != "/v1/messages" || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.Fragment != "" {
		return nil, fmt.Errorf("unexpected closed local endpoint")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("invalid local request body")
	}
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string             `json:"role"`
			Content []fullLocalContent `json:"content"`
		} `json:"messages"`
		Max      int `json:"max_tokens"`
		Thinking struct {
			Type string `json:"type"`
		} `json:"thinking"`
	}
	if json.Unmarshal(raw, &request) != nil || request.Model != "local-full-customer" || request.Max != 128 || request.Thinking.Type != "disabled" || len(request.Messages) == 0 {
		return nil, fmt.Errorf("unexpected local model contract")
	}
	user := ""
	for i := len(request.Messages) - 1; i >= 0 && user == ""; i-- {
		if request.Messages[i].Role != "user" {
			continue
		}
		for _, block := range request.Messages[i].Content {
			if block.Type == "text" {
				user = strings.TrimSpace(block.Text)
				break
			}
		}
	}
	var rule *ConversationRule
	for i := range m.c.Scenario.Turns {
		if strings.TrimSpace(m.c.Scenario.Turns[i].User) == user && user != "" {
			if rule != nil {
				return nil, fmt.Errorf("ambiguous local turn")
			}
			rule = &m.c.Scenario.Turns[i]
		}
	}
	if rule == nil {
		return nil, fmt.Errorf("unknown local source turn")
	}
	text := ""
	for _, block := range request.Messages[len(request.Messages)-1].Content {
		if block.Type == "tool_result" {
			if block.ToolUseID != "full-local-call" || block.Content == "" {
				return nil, fmt.Errorf("unexpected local tool result")
			}
			text = block.Content
		}
	}
	var content []any
	stop := "end_turn"
	if text != "" {
		content = []any{map[string]any{"type": "text", "text": text}}
	} else if hasString(rule.ExpectedActions, "preview") {
		if m.c.Scenario.Operation == nil || !hasString(rule.AllowedTools, m.c.Scenario.Operation.Tool) {
			return nil, fmt.Errorf("local preview operation absent")
		}
		tool := m.c.Scenario.Operation.Tool
		if !isConversationWrite(tool) {
			return nil, fmt.Errorf("unsupported local write tool")
		}
		args := map[string]any{}
		switch tool {
		case "recharge", "refund_recharge":
			value := m.c.Scenario.Operation.Arguments["amount_money"]
			amount := conversationInteger(value)
			if amount <= 0 || (reflect.TypeOf(value) != nil && reflect.TypeOf(value).Kind() == reflect.Float64 && value.(float64) != float64(amount)) {
				return nil, fmt.Errorf("unsupported local amount")
			}
			args["amount_money"] = amount
		case "subscribe_plan", "change_plan":
			ref, ok := m.c.Scenario.Operation.Arguments["plan_reference"].(string)
			name := m.planNames[ref]
			if !ok || name == "" {
				return nil, fmt.Errorf("unresolved local plan name")
			}
			args["plan_name"] = name
		case "cancel_subscription":
		default:
			return nil, fmt.Errorf("unsupported local operation")
		}
		content = []any{map[string]any{"type": "tool_use", "id": "full-local-call", "name": tool, "input": args}}
		stop = "tool_use"
	} else if hasString(rule.ExpectedActions, "read") {
		tool := ""
		for _, name := range rule.AllowedTools {
			if hasString([]string{"get_balance", "get_consumption", "get_recent_bills", "list_models", "list_plans", "get_my_subscription"}, name) {
				tool = name
				break
			}
		}
		if tool == "" {
			return nil, fmt.Errorf("local read tool absent")
		}
		args := map[string]any{}
		if tool == "get_recent_bills" {
			if value, ok := m.readArgs["limit"]; ok {
				limit := conversationInteger(value)
				if limit < 1 || limit > 10 || (reflect.TypeOf(value).Kind() == reflect.Float64 && value.(float64) != float64(limit)) {
					return nil, fmt.Errorf("unsupported local bill limit")
				}
				args["limit"] = limit
			}
		}
		content = []any{map[string]any{"type": "tool_use", "id": "full-local-call", "name": tool, "input": args}}
		stop = "tool_use"
	} else {
		allowed := false
		for _, action := range []string{"clarify", "respond", "consult", "limited", "reject"} {
			allowed = allowed || hasString(rule.ExpectedActions, action)
		}
		if !allowed {
			return nil, fmt.Errorf("unsupported local action")
		}
		content = []any{map[string]any{"type": "text", "text": "这是本机脚本化回复占位，仅验证执行链，语义尚未审核。"}}
	}
	body, err := json.Marshal(map[string]any{"model": "local-full-customer", "content": content, "stop_reason": stop, "usage": map[string]int{"input_tokens": 100, "output_tokens": 8}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}
