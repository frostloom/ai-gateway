package tokens

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// 预扣口径与 New API tokenkit 相同。
// DeepSeek 属于 unknown：按下面的权重估算，不跑分词器。
// 对话请求另加每条消息 3、再加 3，见 gateway 的 estimateMessages。

type family string

const (
	familyOpenAI   family = "openai"
	familyClaude   family = "claude"
	familyClaude47 family = "claude4.7"
	familyGemini   family = "gemini"
	familyUnknown  family = "unknown"
)

type weights struct {
	word, number, cjk, symbol, mathSymbol, urlDelim, atSign, emoji, newline, space float64
}

var familyWeights = map[family]weights{
	familyGemini:   {0.89, 2.42, 0.93, 0.65, 1.05, 1.3, 2.5, 1.08, 1.28, 0},
	familyClaude:   {1.24, 1.53, 1.38, 0.66, 4.52, 0.74, 2.82, 2.6, 1.19, 0.14},
	familyClaude47: {1.52, 1.77, 1.4, 0.89, 4.52, 1.45, 2.82, 2.6, 1.38, 0.21},
	familyOpenAI:   {0.75, 1.86, 0.96, 0.6, 2.68, 0.77, 2.0, 2.12, 0.95, 0.17},
	familyUnknown:  {1.02, 1.55, 0.85, 0.4, 2.68, 1.0, 2.0, 2.12, 0.5, 0.42},
}

// Count 按 New API 的模型族估算文本 token。空文本为 0。
func Count(model, text string) int {
	if text == "" {
		return 0
	}
	w, ok := familyWeights[modelFamily(model)]
	if !ok {
		w = familyWeights[familyUnknown]
	}
	var count float64
	const (
		none = iota
		latin
		number
	)
	current := none
	for _, r := range text {
		if unicode.IsSpace(r) {
			current = none
			if r == '\n' || r == '\t' {
				count += w.newline
			} else {
				count += w.space
			}
			continue
		}
		if isCJK(r) {
			current = none
			count += w.cjk
			continue
		}
		if isEmoji(r) {
			current = none
			count += w.emoji
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			next := latin
			if unicode.IsNumber(r) {
				next = number
			}
			if current != next {
				if next == number {
					count += w.number
				} else {
					count += w.word
				}
				current = next
			}
			continue
		}
		current = none
		switch {
		case isMathSymbol(r):
			count += w.mathSymbol
		case r == '@':
			count += w.atSign
		case isURLDelim(r):
			count += w.urlDelim
		default:
			count += w.symbol
		}
	}
	return int(math.Ceil(count))
}

func modelFamily(model string) family {
	model = strings.ToLower(model)
	switch {
	case strings.Contains(model, "gemini"):
		return familyGemini
	case strings.Contains(model, "claude"):
		if claudeUsesOpus47(model) {
			return familyClaude47
		}
		return familyClaude
	case isOpenAITextModel(model):
		return familyOpenAI
	default:
		return familyUnknown
	}
}

func isOpenAITextModel(model string) bool {
	for _, marker := range []string{"gpt-", "o1", "o3", "o4", "chatgpt"} {
		if strings.Contains(model, marker) {
			return true
		}
	}
	return false
}

func claudeUsesOpus47(model string) bool {
	major, minor, ok := modelVersion(model, "claude-")
	if !ok {
		return true
	}
	return major > 4 || (major == 4 && minor >= 7)
}

func modelVersion(model, prefix string) (major, minor int, ok bool) {
	_, name, found := strings.Cut(model, prefix)
	if !found {
		return 0, 0, false
	}
	version := make([]int, 0, 2)
	for part := range strings.FieldsFuncSeq(name, func(r rune) bool { return r == '-' || r == '.' }) {
		digits := len(part) - len(strings.TrimLeft(part, "0123456789"))
		if digits == 0 || digits > 2 {
			if len(version) > 0 {
				break
			}
			continue
		}
		n, _ := strconv.Atoi(part[:digits])
		version = append(version, n)
		if len(version) == 2 || digits < len(part) {
			break
		}
	}
	switch len(version) {
	case 0:
		return 0, 0, false
	case 1:
		return version[0], 0, true
	default:
		return version[0], version[1], true
	}
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7A3)
}

func isEmoji(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1F9FF) ||
		(r >= 0x2600 && r <= 0x26FF) ||
		(r >= 0x2700 && r <= 0x27BF) ||
		(r >= 0x1FA00 && r <= 0x1FAFF)
}

func isMathSymbol(r rune) bool {
	if strings.ContainsRune("∑∫∂√∞≤≥≠≈±×÷∈∉∋∌⊂⊃⊆⊇∪∩∧∨¬∀∃∄∅∆∇∝∟∠∡∢°′″‴⁺⁻⁼⁽⁾ⁿ₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎²³¹⁴⁵⁶⁷⁸⁹⁰", r) {
		return true
	}
	return (r >= 0x2200 && r <= 0x22FF) ||
		(r >= 0x2A00 && r <= 0x2AFF) ||
		(r >= 0x1D400 && r <= 0x1D7FF)
}

func isURLDelim(r rune) bool {
	return strings.ContainsRune("/:?&=;#%", r)
}
