package localpaired

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type chatMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

func (c ResponsesClient) RoundTripChat(ctx context.Context, raw []byte) ([]byte, error) {
	var req struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Tools    []chatTool    `json:"tools"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = c.Model
	}
	saved := c.Model
	if model != "" {
		c.Model = model
	}
	reply, err := c.completeRequest(ctx, responsesBody(c.Model, req.Messages, req.Tools))
	c.Model = saved
	if err != nil {
		return nil, err
	}
	message := map[string]any{"role": "assistant", "content": reply.Text}
	if len(reply.Calls) > 0 {
		calls := make([]map[string]any, 0, len(reply.Calls))
		for i, call := range reply.Calls {
			id := call.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i+1)
			}
			arguments := call.Arguments
			if arguments == "" {
				arguments = "{}"
			}
			calls = append(calls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]string{
					"name":      call.Name,
					"arguments": arguments,
				},
			})
		}
		message["tool_calls"] = calls
	}
	return json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": message}},
		"usage": map[string]int{
			"prompt_tokens":     reply.InputTokens,
			"completion_tokens": reply.OutputTokens,
		},
	})
}

func responsesBody(model string, messages []chatMessage, tools []chatTool) []byte {
	if model == "" {
		model = CustomerModel
	}
	var instructions []string
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system":
			if message.Content != "" {
				instructions = append(instructions, message.Content)
			}
		case "tool":
			input = append(input, map[string]string{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  message.Content,
			})
		default:
			for _, call := range message.ToolCalls {
				input = append(input, map[string]string{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      call.Function.Name,
					"arguments": call.Function.Arguments,
				})
			}
			if message.Content != "" || len(message.ToolCalls) == 0 {
				input = append(input, map[string]any{
					"role": message.Role,
					"content": []map[string]string{{
						"type": "input_text",
						"text": message.Content,
					}},
				})
			}
		}
	}
	wireTools := make([]any, 0, len(tools))
	for _, tool := range tools {
		wireTools = append(wireTools, map[string]any{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  tool.Function.Parameters,
		})
	}
	body := map[string]any{
		"model":        model,
		"stream":       true,
		"store":        false,
		"instructions": strings.Join(instructions, "\n"),
		"input":        input,
	}
	if len(wireTools) > 0 {
		body["tools"] = wireTools
	}
	raw, _ := json.Marshal(body)
	return raw
}
