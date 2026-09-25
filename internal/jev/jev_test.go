package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 请求体必须符合官方 SystemOneRequest：字段名是 instructions / criteria，
// 端点由调用方给出。这里顺带守住一个历史坑 —— 旧实现发的字段叫 question/choices，
// 上游会 422；端点也写成 /v1/chat/completions 导致 404。
func TestRequestShape(t *testing.T) {
	var seen map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("缺少 Authorization")
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{
			"legit":{"type":"noul","noul":0.92},
			"intent":{"type":"choice","choice":"get_balance","confidence":1.0,
			          "probabilities":{"get_balance":1.0,"chitchat":0.0}},
			"risk":{"type":"score","score":0.1,"confidence":0.9,
			        "legend":{"0":"正常","1":"留意","2":"高风险"},
			        "probabilities":{"0":0.9,"1":0.05,"2":0.05}}
		},"usage":{"input_tokens":100,"output_tokens":10}}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "k", "jev-latest")
	got, err := c.Evaluate(context.Background(),
		map[string]any{"user_message": "帮我查余额"},
		map[string]Question{
			"legit": {
				Type:         QNoul,
				Instructions: "这是否为租户本人的正常请求？",
				Criteria:     map[string]string{"true": "正常", "false": "异常"},
			},
			"intent": {
				Type:         QChoice,
				Instructions: "意图是哪个？",
				Criteria:     map[string]string{"get_balance": "查余额"},
			},
			"risk": {
				Type:         QScore,
				Instructions: "风险多高？",
				Criteria:     []string{"正常", "留意", "高风险"},
			},
		})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	// 请求结构：走 questions 里的 instructions/criteria，不能出现旧的 question/choices
	qs, _ := seen["questions"].(map[string]any)
	if qs == nil {
		t.Fatalf("请求缺 questions: %+v", seen)
	}
	legitQ, _ := qs["legit"].(map[string]any)
	if _, ok := legitQ["instructions"]; !ok {
		t.Errorf("legit 问题缺 instructions 字段（官方契约用 instructions，不是 question）")
	}
	if _, ok := legitQ["question"]; ok {
		t.Errorf("legit 出现了旧的 question 字段，上游会 422")
	}
	if _, ok := legitQ["criteria"]; !ok {
		t.Errorf("legit 问题缺 criteria 字段")
	}

	// 响应解码：三种类型各自的字段
	if v := got.Answers["legit"].Noul; v != 0.92 {
		t.Errorf("legit.noul = %v, want 0.92", v)
	}
	if v := got.Answers["legit"].Conf(); v != 0.92 {
		t.Errorf("noul 的 Conf() 应等于 noul 概率, got %v", v)
	}
	if v := got.Answers["intent"].Choice; v != "get_balance" {
		t.Errorf("intent.choice = %q", v)
	}
	if v := got.Answers["intent"].Conf(); v != 1.0 {
		t.Errorf("intent.Conf() = %v, want 1.0", v)
	}
	if v := got.Answers["risk"].Score; v != 0.1 {
		t.Errorf("risk.score = %v, want 0.1", v)
	}
	if v := got.Answers["risk"].Conf(); v != 0.9 {
		t.Errorf("risk.Conf() = %v, want 0.9", v)
	}
	if v := got.Answers["intent"].Probability("get_balance"); v != 1.0 {
		t.Errorf("Probability(get_balance) = %v", v)
	}
	if got.Model != "jev-1.13.0" {
		t.Errorf("model = %q, want 透传上游返回的模型名", got.Model)
	}
}

// 非标准形态（答案摊平在顶层）也要能解出来。
func TestDecodeFlatShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"dept":{"type":"choice","choice":"billing","confidence":0.8},
		                        "urgent":{"type":"noul","noul":0.95}}`))
	}))
	defer ts.Close()
	c := NewClient(ts.URL, "k", "")
	got, err := c.Evaluate(context.Background(), "state",
		map[string]Question{"dept": {Type: QChoice, Instructions: "哪个部门？"}})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got.Answers["dept"].Choice != "billing" || got.Answers["dept"].Conf() != 0.8 {
		t.Fatalf("dept = %+v", got.Answers["dept"])
	}
	if _, ok := got.Answers["urgent"]; !ok {
		t.Fatal("缺 urgent 答案")
	}
}

// 默认端点必须是官方 System One（旧值 /v1/chat/completions 会 404）。
func TestDefaultEndpoint(t *testing.T) {
	c := NewClient("", "k", "")
	if c.endpoint != DefaultEndpoint {
		t.Fatalf("endpoint = %q, want %q", c.endpoint, DefaultEndpoint)
	}
	if DefaultEndpoint != "https://api.typesafe.ai/v1/systemone" {
		t.Fatalf("DefaultEndpoint = %q，应为官方 systemone 路径", DefaultEndpoint)
	}
	if c.model != DefaultModel {
		t.Fatalf("model = %q, want %q", c.model, DefaultModel)
	}
}

func TestClientErrors(t *testing.T) {
	c := NewClient("", "", "") // 无 key
	if _, err := c.Evaluate(context.Background(), "s",
		map[string]Question{"q": {Type: QNoul, Instructions: "x"}}); err == nil {
		t.Fatal("无 key 应报错")
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	}))
	defer ts.Close()
	c2 := NewClient(ts.URL, "k", "m")
	if _, err := c2.Evaluate(context.Background(), "s",
		map[string]Question{"q": {Type: QNoul, Instructions: "x"}}); err == nil {
		t.Fatal("500 应报错")
	}
	if _, err := c2.Evaluate(context.Background(), "s", nil); err == nil {
		t.Fatal("空 questions 应报错")
	}
}

func TestFake(t *testing.T) {
	f := NewFake(FakeScript{Answers: map[string]Answer{"ok": AnswerNoul(0.95)}})
	got, err := f.Evaluate(context.Background(), "s",
		map[string]Question{"ok": {Type: QNoul, Instructions: "x"}})
	if err != nil {
		t.Fatalf("fake: %v", err)
	}
	if got.Answers["ok"].Conf() != 0.95 {
		t.Fatalf("fake answer = %+v", got.Answers["ok"])
	}
	if _, err := f.Evaluate(context.Background(), "s",
		map[string]Question{"ok": {Type: QNoul, Instructions: "x"}}); err == nil {
		t.Fatal("脚本耗尽应报错")
	}
}

// 便捷构造器要产出各类型应有的字段。
func TestAnswerHelpers(t *testing.T) {
	n := AnswerNoul(0.9)
	if n.Type != string(QNoul) || n.Noul != 0.9 || n.Conf() != 0.9 {
		t.Fatalf("AnswerNoul = %+v", n)
	}
	c := AnswerChoice("recharge", 0.8)
	if c.Type != string(QChoice) || c.Choice != "recharge" || c.Conf() != 0.8 {
		t.Fatalf("AnswerChoice = %+v", c)
	}
	if c.Probability("recharge") != 0.8 {
		t.Fatalf("AnswerChoice 概率未填: %+v", c.Probabilities)
	}
	s := AnswerScore(1.9, 0.87, []string{"正常", "留意", "高风险"})
	if s.Type != string(QScore) || s.Score != 1.9 || s.Conf() != 0.87 {
		t.Fatalf("AnswerScore = %+v", s)
	}
	if s.Legend["2"] != "高风险" {
		t.Fatalf("AnswerScore legend = %+v", s.Legend)
	}
}
