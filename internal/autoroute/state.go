// autoroute state 提取：把 chat/responses 请求体压成 {"role","text"} 紧凑对话状态，
// 序列化结果裁剪到 maxBytes 内（保留最近的轮次），供 JEV 判定调用直接使用。
package autoroute

import (
	"encoding/json"
	"strings"
)

// ExtractState 从 chat/completions 或 responses 请求体提取对话状态。
//
// 顶层有 "messages" 按 chat 形状解析，否则有 "input" 按 responses 形状解析，
// 两者皆缺或 body 非对象返回 ok=false。每条消息映射为 {"role","text"}：
// content 为字符串直接取值；为数组时拼接其中各 text 字段、忽略无 text 的项（如图片）。
// 返回值实际类型为 []map[string]string，按对话顺序排列，可直接作 JEV 的 state 参数。
//
// 裁剪规则：序列化结果超过 maxBytes 时从最旧（头部）条目逐条丢弃再重新序列化，
// 直到放得下或只剩一条；单条仍超限则截断该条 text 字节（保持合法 UTF-8 边界）。
// 连空 text 骨架都放不下、或截断后无任何文本可保留时同样返回 ok=false。
// ok=true 时保证 json.Marshal(state) 长度 ≤ maxBytes。
func ExtractState(body []byte, maxBytes int) (state any, ok bool) {
	if maxBytes <= 0 {
		return nil, false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false
	}
	var msgs []map[string]string
	if raw, has := top["messages"]; has {
		msgs = chatMessages(raw)
	} else if raw, has := top["input"]; has {
		msgs = responsesInput(raw)
	} else {
		return nil, false
	}
	if len(msgs) == 0 {
		return nil, false
	}
	fitted := fitToBudget(msgs, maxBytes)
	if len(fitted) == 0 {
		return nil, false
	}
	return fitted, true
}

// chatMessages 解析 messages 数组：对象元素取 role 与 content 文本，其余跳过；
// 提取不到文本的条目不保留。
func chatMessages(raw json.RawMessage) []map[string]string {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(arr))
	for _, el := range arr {
		var m map[string]json.RawMessage
		if json.Unmarshal(el, &m) != nil {
			continue
		}
		text := contentText(m["content"])
		if text == "" {
			continue
		}
		out = append(out, map[string]string{"role": rawString(m["role"]), "text": text})
	}
	return out
}

// responsesInput 解析 responses 的 input：字符串整体作为一条 user 输入；
// 数组元素为 {role,content} 对象或字符串时各成一条（字符串按 user），其余跳过。
func responsesInput(raw json.RawMessage) []map[string]string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return nil
		}
		return []map[string]string{{"role": "user", "text": s}}
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(arr))
	for _, el := range arr {
		var m map[string]json.RawMessage
		if json.Unmarshal(el, &m) == nil {
			text := contentText(m["content"])
			if text == "" {
				continue
			}
			out = append(out, map[string]string{"role": rawString(m["role"]), "text": text})
			continue
		}
		var str string
		if json.Unmarshal(el, &str) == nil && str != "" {
			out = append(out, map[string]string{"role": "user", "text": str})
		}
	}
	return out
}

// contentText 提取消息 content 的文本：字符串直接返回；
// 数组拼接其中各对象的 text 字符串字段（缺失或非字符串的项忽略）；其余返回空串。
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		var m map[string]json.RawMessage
		if json.Unmarshal(p, &m) != nil {
			continue
		}
		var t string
		if json.Unmarshal(m["text"], &t) == nil {
			b.WriteString(t)
		}
	}
	return b.String()
}

// rawString 取 JSON 字段的字符串值；缺失、null 或非字符串返回空串。
func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// fitToBudget 裁剪状态到 maxBytes 内：保留能放下的最大最近（尾部）后缀；
// 只剩最后一条仍放不下则截断该条 text 字节。
// 元素单独序列化的长度与位置无关（map 键序固定），故先算好各条长度，再从尾部
// 向前累加一次求出最大可留后缀——O(n) 次序列化，避免逐条丢弃后整段重序列化的 O(n²)
//（旧实现在 1 万条小消息上实测 ~14s CPU）。
// 连空 text 骨架都放不下、或截断后无文本可保留时返回 nil（调用方按 ok=false 处理）。
func fitToBudget(msgs []map[string]string, maxBytes int) []map[string]string {
	if len(msgs) == 0 {
		return nil
	}
	// 后缀 msgs[i:] 的序列化长度 = 2（[]）+ Σ len(elem) + (len(msgs)-1-i)（逗号数）。
	lens := make([]int, len(msgs))
	for i, m := range msgs {
		b, _ := json.Marshal(m)
		lens[i] = len(b)
	}
	i := len(msgs) - 1
	total := 2 + lens[i]
	for i > 0 {
		next := total + lens[i-1] + 1
		if next > maxBytes {
			break
		}
		total = next
		i--
	}
	if total <= maxBytes {
		return msgs[i:]
	}
	// 连最后一条都放不下：截断其 text（与原实现一致，截最近一条）。
	return truncateText(msgs[len(msgs)-1], maxBytes)
}

// truncateText 截断单条 text 使整条序列化 ≤ maxBytes：在合法 UTF-8 边界上寻找
// 最长可保留前缀；空文本骨架仍放不下、或没有任何文本可保留时返回 nil。
func truncateText(entry map[string]string, maxBytes int) []map[string]string {
	text := entry["text"]
	entry["text"] = ""
	base, _ := json.Marshal([]map[string]string{entry})
	if len(base) > maxBytes {
		return nil
	}
	// prevBoundary 把位置回退到合法 UTF-8 边界（len 处本身视为边界）。
	prevBoundary := func(i int) int {
		if i >= len(text) {
			return len(text)
		}
		for i > 0 && text[i]&0xC0 == 0x80 {
			i--
		}
		return i
	}
	// 边界处的序列化长度随前缀增长单调不减，故对「回退到边界后的前缀是否放得下」
	// 做原始位置上的二分，等价于在边界集合上二分，结果为最长可保留边界前缀。
	fits := func(cut int) bool {
		entry["text"] = text[:prevBoundary(cut)]
		b, _ := json.Marshal([]map[string]string{entry})
		return len(b) <= maxBytes
	}
	lo, hi := 0, len(text) // cut=0（空文本）已由 base 检查保证放得下
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	cut := prevBoundary(lo)
	if cut == 0 {
		return nil
	}
	entry["text"] = text[:cut]
	return []map[string]string{entry}
}
