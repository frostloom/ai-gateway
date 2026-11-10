package handler

import "testing"

func TestCanonicalModelStripsContextSuffix(t *testing.T) {
	if got := canonicalModel("deepseek-flash[1M]"); got != "deepseek-flash" {
		t.Fatalf("got %s", got)
	}
	if got := canonicalModel(" deepseek-flash "); got != "deepseek-flash" {
		t.Fatalf("got %q", got)
	}
}

func TestNativeUpstreamUsesEvalEndpoints(t *testing.T) {
	if got := nativeUpstream("https://api.deepseek.com", "messages"); got != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Fatalf("messages %s", got)
	}
	if got := nativeUpstream("https://api.deepseek.com/v1", "responses"); got != "https://api.deepseek.com/responses" {
		t.Fatalf("responses %s", got)
	}
}

func TestPrepareNativeBodyDefaultsSSE(t *testing.T) {
	prepared, model, route, stream, maxTokens, err := prepareNativeBody([]byte(`{"model":"deepseek-flash[1M]","input":"hi","max_output_tokens":32}`))
	if err != nil || model != "deepseek-flash[1M]" || route != "deepseek-flash" || !stream || maxTokens != 32 {
		t.Fatalf("prepared model=%s route=%s stream=%v max=%d err=%v body=%s", model, route, stream, maxTokens, err, prepared)
	}
}

func TestNativeUsageReadsResponsesAndMessages(t *testing.T) {
	in, out, ok := nativeUsageFromJSON([]byte(`{"usage":{"input_tokens":11,"output_tokens":4}}`))
	if !ok || in != 11 || out != 4 {
		t.Fatalf("json %d %d %v", in, out, ok)
	}
	acc := nativeUsageAcc{}
	acc.observeLine(`data: {"type":"message_start","message":{"usage":{"input_tokens":9,"output_tokens":0}}}`)
	acc.observeLine(`data: {"type":"message_delta","usage":{"output_tokens":6}}`)
	if !acc.ready() || acc.in != 9 || acc.out != 6 {
		t.Fatalf("messages %+v", acc)
	}
	acc = nativeUsageAcc{}
	acc.observeLine(`data: {"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2}}}`)
	if !acc.ready() || acc.in != 3 || acc.out != 2 {
		t.Fatalf("responses %+v", acc)
	}
}
