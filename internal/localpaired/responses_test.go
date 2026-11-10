package localpaired

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesCompleteReadsStream(t *testing.T) {
	var gotBody string
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ""+
			"event: response.output_text.delta\n"+
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"好\"}\n\n"+
			"event: response.completed\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":5,\"total_tokens\":17}}}\n\n")
	}))
	t.Cleanup(server.Close)

	reply, err := (ResponsesClient{
		Endpoint:  server.URL,
		Token:     "test-token",
		AccountID: "acct-test",
		HTTP:      server.Client(),
	}).Complete(context.Background(), "你好")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "好" || reply.InputTokens != 12 || reply.OutputTokens != 5 {
		t.Fatalf("reply = %+v", reply)
	}
	if !strings.Contains(gotBody, `"model":"gpt-6-luna"`) || !strings.Contains(gotBody, `"stream":true`) || !strings.Contains(gotBody, `"store":false`) || !strings.Contains(gotBody, "你好") {
		t.Fatalf("body = %s", gotBody)
	}
	if gotHeader.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("authorization = %q", gotHeader.Get("Authorization"))
	}
	if gotHeader.Get("ChatGPT-Account-ID") != "acct-test" {
		t.Fatalf("account = %q", gotHeader.Get("ChatGPT-Account-ID"))
	}
	if gotHeader.Get("Accept") != "text/event-stream" || gotHeader.Get("OpenAI-Beta") != "responses=v1" {
		t.Fatalf("headers = %v", gotHeader)
	}
	if gotHeader.Get("originator") != "codex_cli_rs" {
		t.Fatalf("originator = %q", gotHeader.Get("originator"))
	}
}

func TestRoundTripChatReturnsToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":4,\"output_tokens\":6},\"output\":[{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"get_balance\",\"arguments\":\"{}\"}]}}\n\n")
	}))
	t.Cleanup(server.Close)
	raw, err := (ResponsesClient{
		Endpoint:  server.URL,
		Token:     "test-token",
		AccountID: "acct-test",
		HTTP:      server.Client(),
	}).RoundTripChat(context.Background(), []byte(`{"model":"gpt-6-luna","messages":[{"role":"user","content":"查一下余额"}],"tools":[{"function":{"name":"get_balance","description":"余额","parameters":{"type":"object"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Choices) != 1 || len(out.Choices[0].Message.ToolCalls) != 1 || out.Choices[0].Message.ToolCalls[0].Function.Name != "get_balance" {
		t.Fatalf("chat = %s", raw)
	}
}

func TestResponsesCompleteDoesNotCallWithoutCredentials(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	_, err := (ResponsesClient{Endpoint: server.URL, HTTP: server.Client()}).Complete(context.Background(), "你好")
	if err == nil {
		t.Fatal("expected missing credentials to fail")
	}
	if calls != 0 {
		t.Fatalf("requests = %d, want 0", calls)
	}
}
