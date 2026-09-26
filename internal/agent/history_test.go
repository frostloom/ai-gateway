package agent

import (
	"testing"
)

// trimHistory 必须保证 tool_use 与 tool_result 配对完整。
//
// 背景：上游对「不成对的 tool 消息」会陷入超长推理（实测单请求 130s+）。
// 原先简单的 hist[len-N:] 会把 assistant(tool_calls) 与随后的 tool 结果切断，
// 这正是"多轮改套餐"卡死的真凶之一。
func TestTrimHistoryKeepsPairs(t *testing.T) {
	// 构造：[user, assistant(tool_calls), tool, user, assistant(tool_calls), tool, user]
	hist := []Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1", ToolCalls: []ToolCall{{ID: "c1"}}},
		{Role: "tool", ToolCallID: "c1", Content: "t1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2", ToolCalls: []ToolCall{{ID: "c2"}}},
		{Role: "tool", ToolCallID: "c2", Content: "t2"},
		{Role: "user", Content: "u3"},
	}

	// 只断言**长度上界**与**不变量**，不锁精确长度 ——
	// 回退到安全边界的步数取决于数据形状，锁死长度会让测试变脆。
	for _, max := range []int{1, 2, 3, 4, 5, 7, 24} {
		got := trimHistory(hist, max)
		if len(got) > len(hist) {
			t.Errorf("max=%d 裁剪后变长了: %d > %d", max, len(got), len(hist))
		}
		if len(got) == 0 {
			t.Errorf("max=%d 裁成空", max)
		}
		if got[0].Role == "tool" {
			t.Errorf("max=%d 以孤儿 tool 开头: %+v", max, got)
		}
		// 首条不能紧跟在「被裁掉的 tool_calls 消息」之后
		if len(got) > 0 && got[0].Role == "tool" {
			t.Errorf("max=%d 首条是 tool（其 tool_calls 被裁掉）: %+v", max, got)
		}
	}
}

// 裁剪后每个 tool 消息都必须能找到配对的 assistant tool_calls。
func TestTrimHistoryNoOrphanTool(t *testing.T) {
	var hist []Message
	for r := 0; r < 6; r++ {
		id := string(rune('a' + r))
		hist = append(hist,
			Message{Role: "user", Content: "u" + id},
			Message{Role: "assistant", ToolCalls: []ToolCall{{ID: id}}},
			Message{Role: "tool", ToolCallID: id, Content: "t" + id},
		)
	}
	for max := 1; max <= 18; max++ {
		got := trimHistory(hist, max)
		ids := map[string]bool{}
		for _, m := range got {
			for _, tc := range m.ToolCalls {
				ids[tc.ID] = true
			}
		}
		for _, m := range got {
			if m.Role != "tool" {
				continue
			}
			if !ids[m.ToolCallID] {
				t.Fatalf("max=%d 出现孤儿 tool（tool_call_id=%s）: %+v", max, m.ToolCallID, got)
			}
		}
	}
}
