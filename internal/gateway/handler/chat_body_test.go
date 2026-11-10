package handler

import (
	"bytes"
	"testing"
)

func TestParseChatBodyAcceptsPiDefaultRequest(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-flash",
		"messages":[
			{"role":"system","content":"you are pi"},
			{"role":"user","content":[{"type":"text","text":"你好"}]},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"ok"}
		],
		"stream":true,
		"stream_options":{"include_usage":true},
		"max_completion_tokens":16384,
		"tools":[{"type":"function","function":{"name":"bash","parameters":{}}}]
	}`)
	req, err := parseChatBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "deepseek-flash" || !req.Stream || req.MaxTokens != 16384 {
		t.Fatalf("model=%s stream=%v max=%d", req.Model, req.Stream, req.MaxTokens)
	}
	if req.Messages[1].Content != "你好" || req.Messages[2].Content != "" || req.Messages[3].Content != "ok" {
		t.Fatalf("messages %+v", req.Messages)
	}
	if !bytes.Contains(chatUpstreamBody(&req), []byte(`"tools"`)) || !bytes.Contains(chatUpstreamBody(&req), []byte(`"你好"`)) {
		t.Fatal("upstream body dropped pi fields")
	}
}

func TestAdaptChatBodyMapsDeveloperRoleToSystem(t *testing.T) {
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"developer","content":"rules"},{"role":"user","content":"你好"}]}`)
	req, err := parseChatBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.Messages[0].Role != "developer" {
		t.Fatalf("local role = %s", req.Messages[0].Role)
	}
	if !bytes.Contains(chatUpstreamBody(&req), []byte(`"role":"system"`)) || bytes.Contains(chatUpstreamBody(&req), []byte(`"developer"`)) {
		t.Fatalf("upstream body = %s", chatUpstreamBody(&req))
	}
}
