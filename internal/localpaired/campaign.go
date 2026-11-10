package localpaired

import (
	"sort"
	"strings"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

type Campaign struct {
	Passes      int
	Arms        []string
	GraderModel string
	Source      taskbench.ConversationSuite
	Selected    []string
	Added       []Case
	Rubrics     map[string][]RubricItem
}

type Case struct {
	ID     string
	Turns  []Turn
	Rubric []RubricItem
}

type Turn struct {
	User string
	Kind string
}

type RubricItem struct {
	ID   string
	Text string
}

const fixedAccountRefusal = "这个问题和模型商城的账户业务无关。我只能帮你处理余额、消费、账单、套餐和订阅，或办理充值、退款、订购和退订。"

func LoadCampaign(definitionsDir string) (Campaign, error) {
	source, err := taskbench.LoadConversations(definitionsDir)
	if err != nil {
		return Campaign{}, err
	}

	sourceIDs := make(map[string]struct{}, len(source.Cases))
	firstUser := make(map[string]string, len(source.Cases))
	for _, conversation := range source.Cases {
		sourceIDs[conversation.SourceID] = struct{}{}
		if _, seen := firstUser[conversation.SourceID]; seen || len(conversation.Scenario.Turns) == 0 {
			continue
		}
		firstUser[conversation.SourceID] = conversation.Scenario.Turns[0].User
	}
	selected := make([]string, 0, len(sourceIDs))
	for sourceID := range sourceIDs {
		if _, drop := replacedSourceIDs[sourceID]; drop {
			continue
		}
		selected = append(selected, sourceID)
	}
	added := localCases()
	for _, item := range added {
		selected = append(selected, item.ID)
	}
	sort.Strings(selected)

	rubrics := make(map[string][]RubricItem, len(sourceIDs)+len(added))
	for sourceID := range sourceIDs {
		if rubric, ok := sourceRubric(sourceID, firstUser[sourceID]); ok {
			rubrics[sourceID] = rubric
		}
	}
	for _, item := range added {
		rubrics[item.ID] = item.Rubric
	}

	return Campaign{
		Passes:      1,
		Arms:        []string{"model", "jev_selective"},
		GraderModel: "gpt-6.1-sol",
		Source:      source,
		Selected:    selected,
		Added:       added,
		Rubrics:     rubrics,
	}, nil
}

var replacedSourceIDs = map[string]struct{}{
	"balance-099":  {},
	"balance-100":  {},
	"chitchat-032": {},
	"chitchat-033": {},
}

func localCases() []Case {
	const balance = "查一下余额"
	const code = "帮我写一段代码，实现python的简单加减乘除"
	return []Case{
		{
			ID:     "local-read-history-001",
			Turns:  []Turn{{User: balance, Kind: "read_balance"}},
			Rubric: []RubricItem{{ID: "local-read-history-001:balance", Text: "回复当前余额，数值来自工具结果。"}},
		},
		{
			ID:     "local-read-pending-001",
			Turns:  []Turn{{User: balance, Kind: "pending_read_balance"}},
			Rubric: []RubricItem{{ID: "local-read-pending-001:balance", Text: "回复当前余额，并保留待确认操作。"}},
		},
		{
			ID:     "local-offtopic-history-001",
			Turns:  []Turn{{User: code, Kind: "off_topic"}},
			Rubric: []RubricItem{{ID: "local-offtopic-history-001:refusal", Text: fixedAccountRefusal}},
		},
		{
			ID:    "local-offtopic-pending-001",
			Turns: []Turn{{User: code, Kind: "pending_off_topic"}},
			Rubric: []RubricItem{
				{ID: "local-offtopic-pending-001:refusal", Text: fixedAccountRefusal},
				{ID: "local-offtopic-pending-001:pending", Text: "待确认操作仍然保留。"},
			},
		},
	}
}

func sourceRubric(id, user string) ([]RubricItem, bool) {
	prefix, _, _ := strings.Cut(id, "-")
	switch prefix {
	case "balance":
		return []RubricItem{{ID: id + ":tool", Text: "回复当前余额，数值来自工具结果。"}}, true
	case "consumption":
		return []RubricItem{{ID: id + ":tool", Text: "回复近7天消耗，数值来自工具结果。"}}, true
	case "plans":
		return []RubricItem{{ID: id + ":tool", Text: "回复套餐目录，内容来自工具结果。"}}, true
	case "subscription":
		return []RubricItem{{ID: id + ":tool", Text: "回复当前订阅，内容来自工具结果。"}}, true
	case "chitchat":
		return chitchatRubric(id, user), true
	default:
		return nil, false
	}
}

func chitchatRubric(id, user string) []RubricItem {
	switch {
	case unrelatedChitchatUser[user]:
		return []RubricItem{{ID: id + ":refusal", Text: fixedAccountRefusal}}
	case capabilityChitchatUser[user]:
		return []RubricItem{{ID: id + ":capability", Text: "给出合适回复，说明助手是谁或能做什么。"}}
	case greetingChitchatUser[user]:
		return []RubricItem{{ID: id + ":greeting", Text: "给出合适的问候。"}}
	default:
		return []RubricItem{{ID: id + ":reply", Text: "给出合适回复。"}}
	}
}

var unrelatedChitchatUser = map[string]bool{
	"今天几号":   true,
	"现在几点了":  true,
	"天气怎么样":  true,
	"帮我讲个笑话": true,
	"你吃饭了吗":  true,
	"北京在哪":   true,
	"1+1等于几": true,
	"唱首歌":    true,
	"你会写诗吗":  true,
}

var greetingChitchatUser = map[string]bool{
	"你好": true, "你好呀": true, "在吗": true, "嗨": true, "早上好": true,
	"在不在": true, "喂": true, "有人吗": true, "哈喽": true, "哟": true,
	"嘿嘿": true, "嗨喽": true, "哟呵": true, "哈喽啊": true, "在么": true,
	"喂喂喂": true, "人呢": true, "在不在（2）": true, "嗯": true, "哦": true,
	"在": true, "你号": true, "你嚎": true, "拟好": true, "泥好": true,
	"打搅一下": true, "在不": true, "hi": true, "hello": true, "hi there": true,
	"nice to meet you": true, "hello world": true,
}

var capabilityChitchatUser = map[string]bool{
	"你是谁": true, "介绍一下你自己": true, "你能做什么": true, "你叫什么名字": true,
	"请问你是什么": true, "你是机器人吗": true, "你能帮我做什么": true, "介绍一下你的功能": true,
	"你会什么": true, "你好，我是第一次使用你们的平台，想先了解一下你能帮我做些什么": true,
	"我想先跟你打个招呼，顺便问问你这个助手主要负责哪些方面的工作": true,
	"我是一个开发者，想了解一下你的能力范围，先来认识一下":     true,
	"你好，我想问一下，你是真人还是 AI 呀":           true,
	"初次见面，请多关照，你能介绍一下自己吗":            true,
	"我想确认一下这个客服助手的服务范围，麻烦介绍一下":       true,
	"随便问问，你平时都处理些什么问题呀":              true,
	"you are AI?": true, "what can you do": true, "who are you": true, "are you a bot": true,
	"请问你是做什么的": true, "麻烦介绍一下你自己好吗": true, "能不能告诉我你能做什么": true,
	"劳驾问一下你是什么助手": true, "请问怎么称呼你呢": true, "方便介绍一下你的功能吗": true,
	"能否告诉我你的能力范围": true, "麻烦您介绍一下自己": true, "请问你能帮我做什么呢": true,
	"自我介绍": true, "说说你能干啥": true, "介绍功能": true, "报上名来": true,
	"说你叫什么": true, "讲一下你的功能": true, "介绍下自己": true, "说说你会啥": true,
	"报一下身份": true, "给我介绍下你": true, "说说你的能力": true, "快速介绍一下": true,
}
