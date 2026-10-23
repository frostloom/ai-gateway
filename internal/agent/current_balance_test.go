package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCurrentBalanceClaimRequiresThisTurnRead(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective"} {
		t.Run(mode, func(t *testing.T) {
			h, fa, _, done := newAgentHandler(t)
			defer done()
			if err := h.SetRoutingMode(mode); err != nil {
				t.Fatal(err)
			}
			ses := &session{}
			h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("first", "get_balance", `{}`), contentMsg("当前现金余额 ¥20000.00。")}}
			if _, _, err := h.process(context.Background(), "fresh", 42, "查询余额", ses); err != nil {
				t.Fatal(err)
			}
			before := len(fa.meCalls)
			h.llm = &scriptedLLM{steps: []*Message{contentMsg("当前现金余额 ¥20000.00。"), toolCallMsg("second", "get_balance", `{}`), contentMsg("当前现金余额 ¥20000.00。")}}
			_, trace, err := h.process(context.Background(), "fresh", 42, "只查询本人人民币现金余额", ses)
			if err != nil || len(trace) != 1 || trace[0].Tool != "get_balance" || len(fa.meCalls) != before+1 {
				t.Fatalf("history bypassed current read: trace=%+v reads=%d err=%v", trace, len(fa.meCalls), err)
			}
		})
	}
}

func TestUnsupportedCurrentBalanceClaimStopsAfterOneRepair(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{contentMsg("当前现金余额为 ¥5000.00。"), contentMsg("当前现金余额为 ¥5000.00。")}}
	reply, trace, err := h.process(context.Background(), "unsupported", 42, "查余额", &session{})
	if err != nil || strings.Contains(reply, "5000") || !strings.Contains(reply, "未能核实") || len(trace) != 0 {
		t.Fatalf("unsupported cash escaped: %q %v", reply, err)
	}
}

func TestCashClaimFailedOrUnrelatedReadIsNotAuthority(t *testing.T) {
	for _, kind := range []string{"failed-balance", "subscription", "clarification"} {
		t.Run(kind, func(t *testing.T) {
			h, _, _, done := newAgentHandler(t)
			defer done()
			if kind == "clarification" {
				h.llm = &scriptedLLM{steps: []*Message{contentMsg("您想查现金余额还是套餐token额度？")}}
				reply, trace, err := h.process(context.Background(), "clarify-cash", 42, "看看还能用多少", &session{})
				if err != nil || len(trace) != 0 || !strings.Contains(reply, "token") {
					t.Fatalf("unnecessary correction: %q %v", reply, err)
				}
				return
			}
			tool := "get_my_subscription"
			if kind == "failed-balance" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
				defer server.Close()
				h.tb.admin.baseURL = server.URL
				tool = "get_balance"
			}
			h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("other", tool, `{}`), contentMsg("当前现金余额为 ¥5000.00。"), contentMsg("当前现金余额为 ¥5000.00。")}}
			reply, trace, err := h.process(context.Background(), "failed-cash", 42, "查询余额", &session{})
			if err != nil || strings.Contains(reply, "5000") || !strings.Contains(reply, "未能核实") || len(trace) != 1 {
				t.Fatalf("wrong authority accepted: %q %+v %v", reply, trace, err)
			}
		})
	}
}

func TestScopedHistoricalAndExampleCashDoesNotForceRead(t *testing.T) {
	for _, reply := range []string{"您上轮查到的现金余额为 5000 元，这不代表当前余额。", "例如现金余额为 100 元时，元是人民币金额单位。", "您说的“余额5000元”是用户提供的信息，并非本轮查询。"} {
		t.Run(reply, func(t *testing.T) {
			h, fa, _, done := newAgentHandler(t)
			defer done()
			h.llm = &scriptedLLM{steps: []*Message{contentMsg(reply)}}
			got, trace, err := h.process(context.Background(), "history-explanation", 42, "解释一下刚才说的金额", &session{})
			if err != nil || got != reply || len(trace) != 0 || len(fa.meCalls) != 0 {
				t.Fatalf("scoped statement forced read: %q %v", got, err)
			}
		})
	}
}
func TestPreviewContentCannotBypassCurrentCashBoundary(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	msg := toolCallMsg("preview", "recharge", `{"amount_money":100}`)
	msg.Content = "当前现金余额为 ¥5000.00。"
	h.llm = &scriptedLLM{steps: []*Message{msg}}
	ses := &session{}
	reply, trace, err := h.process(context.Background(), "preview-cash", 42, "查询现金余额并充值100元", ses)
	if err != nil || strings.Contains(reply, "5000") || ses.PendingAction == nil || len(trace) != 1 || !strings.Contains(reply, "确认") {
		t.Fatalf("unsupported cash or lost preview: %q %v", reply, err)
	}
}

func TestHistoricalClauseDoesNotAuthorizeCurrentCashClaim(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	reply := "上次现金余额为5000元，当前现金余额为5000元。"
	h.llm = &scriptedLLM{steps: []*Message{contentMsg(reply), contentMsg(reply)}}
	got, _, err := h.process(context.Background(), "mixed-cash", 42, "查当前余额", &session{})
	if err != nil || strings.Contains(got, "5000") || !strings.Contains(got, "未能核实") {
		t.Fatalf("history masked current claim: %q %v", got, err)
	}
}
