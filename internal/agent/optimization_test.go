package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestGuardRejectionStopsTurn(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	h.SetJev(jev.NewFake(
		verdictScript(0.99, "refund_recharge", 0.99, 0.0, 0.9),
		jev.FakeScript{Answers: map[string]jev.Answer{"legit_request": jev.AnswerNoul(0.1)}},
	))
	// No second response: a rejected operation must not be retried or reinterpreted by the model.
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("call-1", "refund_recharge", `{"amount_money":50}`)}}
	ses := &session{}
	reply, trace, err := h.process(context.Background(), "reject-stop", 42, "退款50元", ses)
	if err != nil || !strings.Contains(reply, "拦截") {
		t.Fatalf("rejection did not end the turn: %q, %v", reply, err)
	}
	if ses.PendingAction != nil || len(trace) != 1 || len(fa.posts) != 0 {
		t.Fatalf("rejected operation changed state: pending=%v, trace=%+v, posts=%+v", ses.PendingAction, trace, fa.posts)
	}
}

func TestRepeatedReadStopsAndNextTurnIsFresh(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("read-1", "get_balance", `{}`),
		toolCallMsg("read-2", "get_balance", `{}`),
		toolCallMsg("read-loop", "get_balance", `{}`),
	}}
	ses := &session{}
	reply, trace, err := h.process(context.Background(), "read-once", 42, "查询现在余额", ses)
	if err != nil || !strings.Contains(reply, "余额") || len(trace) != 1 || len(fa.meCalls) != 1 {
		t.Fatalf("duplicate read was not stopped: reply=%q trace=%+v requests=%d err=%v", reply, trace, len(fa.meCalls), err)
	}
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("read-3", "get_balance", `{}`), contentMsg("余额已重新查询。")}}
	if _, _, err := h.process(context.Background(), "read-once", 42, "再查一下最新余额", ses); err != nil {
		t.Fatal(err)
	}
	if len(fa.meCalls) != 2 {
		t.Fatal("read cache leaked across turns")
	}
	// Every tool result retained in history still has its assistant call.
	if _, err := buildAnthropicRequest("test", buildMessages(ses, nil, jevVerdict{}), h.tb.All(), 100); err != nil {
		t.Fatalf("stopping a duplicate damaged the protocol history: %v", err)
	}
}

func TestReadReuseDoesNotDropCompositeWriteRequest(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{
		toolCallMsg("balance-1", "get_balance", `{}`),
		toolCallMsg("balance-2", "get_balance", `{}`),
		toolCallMsg("refund-1", "refund_recharge", `{"amount_money":50}`),
	}}
	ses := &session{}
	_, trace, err := h.process(context.Background(), "composite", 42, "查余额后帮我退款50元", ses)
	// Only two real tool operations: the repeated read is reuse, and refund preview
	// independently obtains the current balance. No write is executed before confirmation.
	if err != nil || ses.PendingAction == nil || ses.PendingAction.Tool != "refund_recharge" || len(trace) != 2 || len(fa.posts) != 0 {
		t.Fatalf("reusing a read dropped the explicit write request: pending=%+v trace=%+v err=%v", ses.PendingAction, trace, err)
	}
}
