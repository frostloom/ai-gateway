package localpaired

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const CodexResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"

type ResponsesClient struct {
	Endpoint  string
	Token     string
	AccountID string
	Model     string
	HTTP      *http.Client
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type Reply struct {
	Text         string
	Calls        []ToolCall
	InputTokens  int
	OutputTokens int
}

func (c ResponsesClient) Complete(ctx context.Context, userText string) (Reply, error) {
	if strings.TrimSpace(c.Token) == "" || strings.TrimSpace(c.AccountID) == "" {
		return Reply{}, fmt.Errorf("responses credentials missing")
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return Reply{}, fmt.Errorf("responses endpoint missing")
	}
	model := c.Model
	if model == "" {
		model = CustomerModel
	}
	body, err := json.Marshal(map[string]any{
		"model":  model,
		"stream": true,
		"store":  false,
		"input": []map[string]any{{
			"role": "user",
			"content": []map[string]string{{
				"type": "input_text",
				"text": userText,
			}},
		}},
	})
	if err != nil {
		return Reply{}, err
	}
	return c.completeRequest(ctx, body)
}

func (c ResponsesClient) completeRequest(ctx context.Context, body []byte) (Reply, error) {
	if strings.TrimSpace(c.Token) == "" || strings.TrimSpace(c.AccountID) == "" {
		return Reply{}, fmt.Errorf("responses credentials missing")
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return Reply{}, fmt.Errorf("responses endpoint missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("ChatGPT-Account-ID", c.AccountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=v1")
	req.Header.Set("originator", "codex_cli_rs")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Reply{}, fmt.Errorf("responses HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return parseResponsesStream(resp.Body)
}

func parseResponsesStream(r io.Reader) (Reply, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var text strings.Builder
	var reply Reply
	var event string
	completed := false
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			event = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var payload struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  *struct {
				Type      string `json:"type"`
				Name      string `json:"name"`
				CallID    string `json:"call_id"`
				Arguments string `json:"arguments"`
			} `json:"item"`
			Response *struct {
				Status string `json:"status"`
				Usage  struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Output []struct {
					Type      string `json:"type"`
					Name      string `json:"name"`
					CallID    string `json:"call_id"`
					Arguments string `json:"arguments"`
					Content   []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return Reply{}, err
		}
		kind := payload.Type
		if kind == "" {
			kind = event
		}
		switch kind {
		case "response.output_text.delta":
			text.WriteString(payload.Delta)
		case "response.output_item.added":
			if payload.Item != nil && payload.Item.Type == "function_call" {
				reply.Calls = append(reply.Calls, ToolCall{ID: payload.Item.CallID, Name: payload.Item.Name, Arguments: payload.Item.Arguments})
			}
		case "response.function_call_arguments.delta":
			if len(reply.Calls) == 0 {
				reply.Calls = append(reply.Calls, ToolCall{})
			}
			reply.Calls[len(reply.Calls)-1].Arguments += payload.Delta
		case "response.completed":
			completed = true
			if payload.Response != nil {
				reply.InputTokens = payload.Response.Usage.InputTokens
				reply.OutputTokens = payload.Response.Usage.OutputTokens
				if len(payload.Response.Output) > 0 {
					text.Reset()
					reply.Calls = nil
					for _, item := range payload.Response.Output {
						switch item.Type {
						case "function_call":
							reply.Calls = append(reply.Calls, ToolCall{ID: item.CallID, Name: item.Name, Arguments: item.Arguments})
						case "message":
							for _, part := range item.Content {
								text.WriteString(part.Text)
							}
						}
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Reply{}, err
	}
	if !completed {
		return Reply{}, fmt.Errorf("responses stream did not complete")
	}
	reply.Text = text.String()
	return reply, nil
}
