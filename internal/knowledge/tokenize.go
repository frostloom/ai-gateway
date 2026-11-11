package knowledge

import (
	"strings"
	"unicode"
)

// Business terms provide meaningful boundaries for short Chinese queries. The
// remaining Han runs use bigrams so new wording does not require a dictionary update.
var domainTerms = map[string]string{
	"一次性兑换码": "兑换码", "兑换码": "兑换码", "充值码": "兑换码", "兑换券": "兑换码",
	"取消订阅": "退订", "取消套餐": "退订", "退订": "退订", "续费": "续费",
	"退款": "退款", "退钱": "退款", "充值": "充值", "充钱": "充值",
	"现金余额": "余额", "余额": "余额", "钱包": "钱包", "月卡": "月卡", "套餐": "套餐",
	"订阅": "订阅", "额度": "额度", "配额": "额度", "滚动": "滚动", "恢复": "恢复",
	"输入": "输入", "输出": "输出", "计费": "计费", "收费": "计费", "价格": "价格",
	"扣费": "扣费", "扣款": "扣费", "消费": "消费", "用量": "用量", "账单": "账单",
	"订单": "订单", "模型": "模型", "密钥": "密钥", "接口": "接口", "接入": "接入",
	"上下文": "上下文", "窗口": "窗口", "周期": "周期", "超额": "超额", "超限": "超额",
	"缓存": "缓存", "确认": "确认", "取消": "取消", "权限": "权限", "账户": "账户",
	"账号": "账户", "兑换": "兑换码", "有效期": "有效期", "删除": "删除", "停用": "停用",
}

var ignoredTerms = map[string]bool{
	"请问": true, "怎么": true, "如何": true, "什么": true, "哪些": true, "可以": true,
	"是否": true, "帮我": true, "一下": true, "需要": true, "关于": true, "告诉": true,
	"告诉我": true, "我的": true, "本人": true, "这个": true, "那个": true, "平台": true,
	"现在": true, "当前": true, "最近": true, "你好": true, "您好": true, "规则": true, "流程": true, "问题": true, "一下子": true,
	"the": true, "a": true, "an": true, "is": true, "are": true, "how": true, "what": true,
	"can": true, "i": true, "my": true, "please": true, "to": true, "of": true, "and": true,
}

func normalizeQuery(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= '！' && r <= '～' {
			return unicode.ToLower(r - 0xfee0)
		}
		if r == '\u3000' {
			return ' '
		}
		return unicode.ToLower(r)
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func addTerm(out map[string]bool, term string) {
	if term == "tokens" {
		term = "token"
	}
	if len([]rune(term)) > 1 && !ignoredTerms[term] {
		out[term] = true
	}
}

func terms(s string) map[string]bool {
	out := map[string]bool{}
	runes := []rune(normalizeQuery(s))
	for i := 0; i < len(runes); {
		if unicode.Is(unicode.Han, runes[i]) {
			end := i + 1
			for end < len(runes) && unicode.Is(unicode.Han, runes[end]) {
				end++
			}
			tokenizeHan(runes[i:end], out)
			i = end
			continue
		}
		if unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) {
			end := i + 1
			for end < len(runes) && identifierRune(runes[end]) {
				end++
			}
			word := strings.Trim(string(runes[i:end]), "_./-")
			if !onlyDigits(word) {
				addTerm(out, word)
				for _, part := range strings.FieldsFunc(word, func(r rune) bool { return r == '_' || r == '/' || r == '.' || r == '-' }) {
					if !onlyDigits(part) {
						addTerm(out, part)
					}
				}
				if word == "apikey" || word == "api_key" {
					addTerm(out, "api")
					addTerm(out, "key")
				}
			}
			i = end
			continue
		}
		i++
	}
	return out
}

func identifierRune(r rune) bool {
	return !unicode.Is(unicode.Han, r) && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_./-", r))
}

func onlyDigits(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func tokenizeHan(run []rune, out map[string]bool) {
	unknownStart := 0
	flushUnknown := func(end int) {
		for i := unknownStart + 1; i < end; i++ {
			addTerm(out, string(run[i-1:i+1]))
		}
	}
	for i := 0; i < len(run); {
		matched := ""
		length := 0
		for n := min(6, len(run)-i); n >= 2; n-- {
			if canonical, ok := domainTerms[string(run[i:i+n])]; ok {
				matched, length = canonical, n
				break
			}
		}
		if length == 0 {
			i++
			continue
		}
		flushUnknown(i)
		addTerm(out, matched)
		i += length
		unknownStart = i
	}
	flushUnknown(len(run))
}
