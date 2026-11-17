// autoroute state 提取测试：chat/responses 形状映射、多模态拼接、无文本与畸形输入拒绝、
// maxBytes 裁剪保留最近轮次与单条 text 截断。
package autoroute

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// extractOK 调用 ExtractState 并断言 ok，返回类型收窄后的状态切片。
func extractOK(t *testing.T, body string, maxBytes int) []map[string]string {
	t.Helper()
	state, ok := ExtractState([]byte(body), maxBytes)
	if !ok {
		t.Fatalf("应能提取到状态: %s", body)
	}
	msgs, isSlice := state.([]map[string]string)
	if !isSlice {
		t.Fatalf("state 实际类型应为 []map[string]string, 得到 %T", state)
	}
	return msgs
}

// assertState 断言状态与期望逐条一致（顺序敏感）。
func assertState(t *testing.T, got, want []map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("条目数 = %d, 期望 %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("第 %d 条 = %v, 期望 %v", i, got[i], want[i])
		}
	}
}

// chat 形状：逐条映射 role/text，保持对话顺序。
func TestExtractStateChatMessages(t *testing.T) {
	body := `{"model":"deepseek-v4-flash","messages":[` +
		`{"role":"system","content":"你是客服助手"},` +
		`{"role":"user","content":"查一下余额"},` +
		`{"role":"assistant","content":"当前余额 100 元"}]}`
	got := extractOK(t, body, 8192)
	assertState(t, got, []map[string]string{
		{"role": "system", "text": "你是客服助手"},
		{"role": "user", "text": "查一下余额"},
		{"role": "assistant", "text": "当前余额 100 元"},
	})
}

// content 为多模态数组：拼接各 text 字段，忽略无 text 的项。
func TestExtractStateMultimodalContent(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[` +
		`{"type":"text","text":"描述这张图"},` +
		`{"type":"image_url","image_url":{"url":"https://example.com/a.png"}},` +
		`{"type":"text","text":"并判断金额"}]}]}`
	got := extractOK(t, body, 8192)
	assertState(t, got, []map[string]string{
		{"role": "user", "text": "描述这张图并判断金额"},
	})
}

// responses 形状：字符串 input 整体作为一条 user 输入。
func TestExtractStateResponsesStringInput(t *testing.T) {
	got := extractOK(t, `{"model":"m","input":"帮我充值 100 元"}`, 8192)
	assertState(t, got, []map[string]string{
		{"role": "user", "text": "帮我充值 100 元"},
	})
}

// responses 形状：数组 input 中 {role,content} 与字符串元素各成一条。
func TestExtractStateResponsesInputArray(t *testing.T) {
	body := `{"input":[` +
		`{"role":"user","content":"第一条问题"},` +
		`"直接文本输入",` +
		`{"role":"assistant","content":[{"type":"text","text":"多模态回答"}]}]}`
	got := extractOK(t, body, 8192)
	assertState(t, got, []map[string]string{
		{"role": "user", "text": "第一条问题"},
		{"role": "user", "text": "直接文本输入"},
		{"role": "assistant", "text": "多模态回答"},
	})
}

// 提取不到任何文本或输入形状非法时 ok=false 且 state 为 nil。
func TestExtractStateNotOK(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		maxBytes int
	}{
		{"空messages", `{"messages":[]}`, 8192},
		{"messages为null", `{"messages":null}`, 8192},
		{"空input数组", `{"input":[]}`, 8192},
		{"空input字符串", `{"input":""}`, 8192},
		{"两者皆缺", `{"model":"m","stream":true}`, 8192},
		{"content全无text项", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`, 8192},
		{"content为null", `{"messages":[{"role":"user","content":null}]}`, 8192},
		{"input为数字", `{"input":42}`, 8192},
		{"body非JSON", `not json`, 8192},
		{"body非对象", `[1,2,3]`, 8192},
		{"maxBytes为0", `{"messages":[{"role":"user","content":"hi"}]}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, ok := ExtractState([]byte(tc.body), tc.maxBytes)
			if ok {
				t.Fatalf("%s 不应提取到状态, 得到 %v", tc.name, state)
			}
			if state != nil {
				t.Fatalf("%s ok=false 时 state 应为 nil, 得到 %v", tc.name, state)
			}
		})
	}
}

// 裁剪：超限时从最旧条目开始丢弃，保留最近轮次且序列化长度 ≤ maxBytes。
func TestExtractStateTruncationKeepsRecentTurns(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"messages":[`)
	for i := 0; i < 10; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"role":"user","content":"第%d轮:%s"}`, i, strings.Repeat("x", 100))
	}
	b.WriteString(`]}`)
	body := b.String()

	full := extractOK(t, body, 1<<20)
	fullJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化完整状态失败: %v", err)
	}
	maxBytes := len(fullJSON) / 3

	got := extractOK(t, body, maxBytes)
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("序列化裁剪状态失败: %v", err)
	}
	if len(gotJSON) > maxBytes {
		t.Fatalf("裁剪后长度 %d 超过 maxBytes %d", len(gotJSON), maxBytes)
	}
	if len(got) == 0 || len(got) == len(full) {
		t.Fatalf("应发生裁剪但条目数 = %d/%d", len(got), len(full))
	}
	// 保留的必须恰好是最近的连续后缀（从最旧开始丢弃）。
	for i := range got {
		if !reflect.DeepEqual(got[i], full[len(full)-len(got)+i]) {
			t.Fatalf("保留条目不是最近后缀: got[%d] = %v, 期望 %v",
				i, got[i], full[len(full)-len(got)+i])
		}
	}
}

// 单条仍超限：截断该条 text 字节，保持合法 UTF-8 前缀且序列化长度 ≤ maxBytes。
func TestExtractStateTruncatesSingleEntry(t *testing.T) {
	long := strings.Repeat("长文本", 500) // 1500 字节的合法 UTF-8
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":"前缀:%s"}]}`, long)

	full := extractOK(t, body, 1<<20)
	fullJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化完整状态失败: %v", err)
	}
	maxBytes := len(fullJSON) / 2

	got := extractOK(t, body, maxBytes)
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("序列化截断状态失败: %v", err)
	}
	if len(gotJSON) > maxBytes {
		t.Fatalf("截断后长度 %d 超过 maxBytes %d", len(gotJSON), maxBytes)
	}
	if len(got) != 1 || got[0]["role"] != "user" {
		t.Fatalf("应保留单条 user 条目, 得到 %v", got)
	}
	text := got[0]["text"]
	if text == full[0]["text"] {
		t.Fatalf("text 应被截断, 长度 %d", len(text))
	}
	if !strings.HasPrefix(full[0]["text"], text) {
		t.Fatalf("text 应为原文前缀: %q", text)
	}
}
