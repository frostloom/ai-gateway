package localpaired

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const frozenDefinitionsDir = "../../eval/task-definitions/2026-10-12-full-dev-v1"

func TestCampaignIsOnePassTwoArms(t *testing.T) {
	campaign, err := LoadCampaign(frozenDefinitionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if campaign.Passes != 1 {
		t.Fatalf("Passes = %d, want 1", campaign.Passes)
	}
	if want := []string{"model", "jev_selective"}; !reflect.DeepEqual(campaign.Arms, want) {
		t.Fatalf("Arms = %q, want %q", campaign.Arms, want)
	}
	if len(campaign.Selected) != 1220 {
		t.Fatalf("len(Selected) = %d, want 1220", len(campaign.Selected))
	}
	if !sort.StringsAreSorted(campaign.Selected) {
		t.Fatal("Selected is not sorted")
	}
	seen := make(map[string]bool, len(campaign.Selected))
	prefixCounts := map[string]int{}
	for _, id := range campaign.Selected {
		if seen[id] {
			t.Fatalf("duplicate selected source id %q", id)
		}
		seen[id] = true
		prefixCounts[quotaGroup(id)]++
	}
	for _, prefix := range []string{"balance", "consumption", "plans", "subscription", "chitchat"} {
		if prefixCounts[prefix] != 100 {
			t.Errorf("%s selected count = %d, want 100", prefix, prefixCounts[prefix])
		}
	}
	removed := []string{"balance-099", "balance-100", "chitchat-032", "chitchat-033"}
	for _, id := range removed {
		if seen[id] {
			t.Errorf("selected still contains %s", id)
		}
	}
	for _, id := range addedCaseIDs {
		if !seen[id] {
			t.Errorf("selected missing %s", id)
		}
	}
	if !seen["chitchat-001"] {
		t.Error("chitchat-001 is not selected")
	}
	sourceIDs := map[string]bool{}
	for _, conversation := range campaign.Source.Cases {
		sourceIDs[conversation.SourceID] = true
	}
	if len(sourceIDs) != 1220 {
		t.Fatalf("source ids = %d, want 1220", len(sourceIDs))
	}
	for id := range sourceIDs {
		if isRemovedSource(id) {
			continue
		}
		if !seen[id] {
			t.Errorf("selected missing source id %s", id)
		}
	}
	if users := uniqueUsers(campaign, "balance-099"); !reflect.DeepEqual(users, []string{"余额发我"}) {
		t.Errorf("balance-099 users = %q", users)
	}
	if users := uniqueUsers(campaign, "balance-100"); !reflect.DeepEqual(users, []string{"快速查下余额"}) {
		t.Errorf("balance-100 users = %q", users)
	}
	if users := uniqueUsers(campaign, "chitchat-032"); !reflect.DeepEqual(users, []string{"嗨"}) {
		t.Errorf("chitchat-032 users = %q", users)
	}
	if users := uniqueUsers(campaign, "chitchat-033"); !reflect.DeepEqual(users, []string{"在吗"}) {
		t.Errorf("chitchat-033 users = %q", users)
	}
	if len(campaign.Source.Cases) != 4291 {
		t.Fatalf("len(Source.Cases) = %d, want 4291", len(campaign.Source.Cases))
	}
	if len(campaign.Added) != len(addedCaseIDs) {
		t.Fatalf("len(Added) = %d, want %d", len(campaign.Added), len(addedCaseIDs))
	}
	gotAdded := make([]string, 0, len(campaign.Added))
	for _, item := range campaign.Added {
		gotAdded = append(gotAdded, item.ID)
	}
	sort.Strings(gotAdded)
	wantAdded := append([]string(nil), addedCaseIDs...)
	sort.Strings(wantAdded)
	if !reflect.DeepEqual(gotAdded, wantAdded) {
		t.Fatalf("Added ids = %q, want %q", gotAdded, wantAdded)
	}
}

func TestNewCasesCoverRoutingHoles(t *testing.T) {
	campaign, err := LoadCampaign(frozenDefinitionsDir)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Case{}
	for _, item := range campaign.Added {
		cases[item.ID] = item
	}
	checks := []struct {
		id    string
		user  string
		kind  string
		texts []string
	}{
		{id: "local-read-history-001", user: "查一下余额", kind: "read_balance", texts: []string{"回复当前余额，数值来自工具结果。"}},
		{id: "local-offtopic-history-001", user: "帮我写一段代码，实现python的简单加减乘除", kind: "off_topic", texts: []string{wantFixedRefusal}},
		{id: "local-read-pending-001", user: "查一下余额", kind: "pending_read_balance", texts: []string{"回复当前余额，并保留待确认操作。"}},
		{id: "local-offtopic-pending-001", user: "帮我写一段代码，实现python的简单加减乘除", kind: "pending_off_topic", texts: []string{wantFixedRefusal, "待确认操作仍然保留。"}},
	}
	if len(cases) != len(checks) {
		t.Fatalf("len(Added) = %d, want %d", len(cases), len(checks))
	}
	for _, check := range checks {
		item, ok := cases[check.id]
		if !ok {
			t.Fatalf("missing case %s", check.id)
		}
		if len(item.Turns) != 1 || item.Turns[0].User != check.user || item.Turns[0].Kind != check.kind {
			t.Fatalf("%s turns = %+v, want user %q kind %q", check.id, item.Turns, check.user, check.kind)
		}
		if !reflect.DeepEqual(rubricTexts(item.Rubric), check.texts) {
			t.Fatalf("%s rubric = %#v, want %#v", check.id, rubricTexts(item.Rubric), check.texts)
		}
		seenItem := map[string]bool{}
		for _, rubric := range item.Rubric {
			if rubric.ID == "" || seenItem[rubric.ID] {
				t.Fatalf("%s rubric id %q", check.id, rubric.ID)
			}
			seenItem[rubric.ID] = true
		}
	}
}

func TestGreetingStaysBusiness(t *testing.T) {
	campaign, err := LoadCampaign(frozenDefinitionsDir)
	if err != nil {
		t.Fatal(err)
	}
	sourceChitchat := map[string]bool{}
	for _, conversation := range campaign.Source.Cases {
		if strings.HasPrefix(conversation.SourceID, "chitchat-") {
			sourceChitchat[conversation.SourceID] = true
		}
	}
	if len(sourceChitchat) != 100 {
		t.Fatalf("chitchat source ids = %d, want 100", len(sourceChitchat))
	}
	hello := rubricTexts(campaign.Rubrics["chitchat-001"])
	if users := uniqueUsers(campaign, "chitchat-001"); !reflect.DeepEqual(users, []string{"你好"}) {
		t.Fatalf("chitchat-001 users = %q", users)
	}
	if !reflect.DeepEqual(hello, []string{"给出合适的问候。"}) {
		t.Fatalf("chitchat-001 rubric = %#v", hello)
	}
	if strings.Contains(hello[0], wantFixedRefusal) {
		t.Fatal("greeting rubric uses the fixed refusal")
	}
	for id := range sourceChitchat {
		got := rubricTexts(campaign.Rubrics[id])
		users := uniqueUsers(campaign, id)
		if want, unrelated := unrelatedChitchat[id]; unrelated {
			if !reflect.DeepEqual(users, []string{want}) {
				t.Fatalf("%s users = %q, want %q", id, users, want)
			}
			if !reflect.DeepEqual(got, []string{wantFixedRefusal}) {
				t.Fatalf("%s rubric = %#v, want fixed refusal", id, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Fatalf("%s has no rubric", id)
		}
		for _, text := range got {
			if text == wantFixedRefusal || strings.Contains(text, wantFixedRefusal) {
				t.Fatalf("%s uses fixed refusal for an account-related chitchat turn: %q", id, text)
			}
			if !strings.Contains(text, "合适") {
				t.Fatalf("%s rubric = %q, want a suitable reply", id, text)
			}
		}
	}
	for _, id := range campaign.Selected {
		prefix, _, _ := strings.Cut(id, "-")
		want, ok := whitelistRubric[prefix]
		if !ok {
			continue
		}
		got := rubricTexts(campaign.Rubrics[id])
		if !reflect.DeepEqual(got, []string{want}) {
			t.Fatalf("%s rubric = %#v, want %q", id, got, want)
		}
		if !strings.Contains(want, "来自工具结果") {
			t.Fatalf("%s rubric does not cite the tool result", id)
		}
		for _, conversation := range campaign.Source.Cases {
			if conversation.SourceID != id {
				continue
			}
			for _, assertion := range conversation.Assertions {
				for _, text := range got {
					if text == assertion.Requirement || strings.Contains(text, assertion.Requirement) || strings.Contains(assertion.Requirement, text) {
						t.Fatalf("%s rubric copies answer assertion %q", id, assertion.Requirement)
					}
				}
			}
		}
	}
}

const wantFixedRefusal = "这个问题和模型商城的账户业务无关。我只能帮你处理余额、消费、账单、套餐和订阅，或办理充值、退款、订购和退订。"

var addedCaseIDs = []string{
	"local-read-history-001",
	"local-read-pending-001",
	"local-offtopic-history-001",
	"local-offtopic-pending-001",
}

var unrelatedChitchat = map[string]string{
	"chitchat-061": "今天几号",
	"chitchat-062": "现在几点了",
	"chitchat-063": "天气怎么样",
	"chitchat-064": "帮我讲个笑话",
	"chitchat-065": "你吃饭了吗",
	"chitchat-067": "北京在哪",
	"chitchat-068": "1+1等于几",
	"chitchat-069": "唱首歌",
	"chitchat-070": "你会写诗吗",
}

var whitelistRubric = map[string]string{
	"balance":      "回复当前余额，数值来自工具结果。",
	"consumption":  "回复近7天消耗，数值来自工具结果。",
	"plans":        "回复套餐目录，内容来自工具结果。",
	"subscription": "回复当前订阅，内容来自工具结果。",
}

func quotaGroup(id string) string {
	switch {
	case strings.HasPrefix(id, "local-read-"):
		return "balance"
	case strings.HasPrefix(id, "local-offtopic-"):
		return "chitchat"
	default:
		prefix, _, _ := strings.Cut(id, "-")
		return prefix
	}
}

func isRemovedSource(id string) bool {
	switch id {
	case "balance-099", "balance-100", "chitchat-032", "chitchat-033":
		return true
	default:
		return false
	}
}

func uniqueUsers(campaign Campaign, id string) []string {
	seen := map[string]bool{}
	var users []string
	for _, conversation := range campaign.Source.Cases {
		if conversation.SourceID != id {
			continue
		}
		for _, turn := range conversation.Scenario.Turns {
			if seen[turn.User] {
				continue
			}
			seen[turn.User] = true
			users = append(users, turn.User)
		}
	}
	return users
}

func rubricTexts(items []RubricItem) []string {
	texts := make([]string, 0, len(items))
	for _, item := range items {
		texts = append(texts, item.Text)
	}
	return texts
}

func TestFrozenDefinitionsUntouched(t *testing.T) {
	raw, err := os.ReadFile(frozenDefinitionsDir + "/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(raw))
	const want = "afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181"
	if got != want {
		t.Fatalf("manifest SHA256 = %s, want %s", got, want)
	}
}
