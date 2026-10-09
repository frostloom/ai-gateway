package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestPlanColloquialTargetResolvesWithoutReasking(t *testing.T) {
	for _, name := range []string{"旗舰档", "旗舰套餐", "月度标准套餐", "标准档"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(store.MeView{Plans: []store.Plan{
					{ID: 3, Name: "月度·标准", PlanType: store.PlanTypeRecurring},
					{ID: 4, Name: "月度·旗舰", PlanType: store.PlanTypeRecurring},
				}})
			}))
			defer server.Close()
			args := map[string]any{"plan_name": name}
			preview, err := NewToolbox(NewAdminClient(server.URL)).Preview(context.Background(), "subscribe_plan", args, 42)
			if err != nil || !strings.Contains(preview, "将订阅") || args["plan_id"] == nil {
				t.Fatalf("unique user target should preview: %q %v", preview, err)
			}
		})
	}
}

type observedLLM struct {
	script   scriptedLLM
	requests []string
}

func (llm *observedLLM) Chat(ctx context.Context, messages []Message, tools []Tool) (*Message, error) {
	llm.requests = append(llm.requests, jsonString(struct {
		Messages []Message
		Tools    []Tool
	}{messages, tools}))
	return llm.script.Chat(ctx, messages, tools)
}

func TestPrimaryFallbackUsesIdenticalModelInputs(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	var baseline []string
	for _, mode := range []string{"model", "jev_selective"} {
		if err := h.SetRoutingMode(mode); err != nil {
			t.Fatal(err)
		}
		h.SetJev(jev.NewFake(fastVerdict("get_balance", .5)))
		llm := &observedLLM{script: scriptedLLM{steps: []*Message{toolCallMsg("read-1", "get_balance", `{}`), contentMsg("当前余额¥20000.00。")}}}
		h.llm = llm
		stats := &TurnTelemetry{}
		ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
		_, _, err := h.process(ctx, "same-input-"+mode, 42, "查余额", &session{})
		if err != nil {
			t.Fatal(err)
		}
		if mode == "model" {
			baseline = llm.requests
			if stats.JevCalls != 0 {
				t.Fatal("V1 called JEV")
			}
		} else if !reflect.DeepEqual(baseline, llm.requests) || stats.JevCalls != 1 || stats.JevBlocked {
			t.Fatalf("V2 fallback changed model inputs or added judge calls: calls=%d input=%v", stats.JevCalls, llm.requests)
		}
	}
}

func TestSelectiveWriteAndConfirmationShareExecutionBoundary(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	if err := h.SetRoutingMode("jev_selective"); err != nil {
		t.Fatal(err)
	}
	h.SetJev(jev.NewFake(fastVerdict("recharge", .99)))
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("write-1", "recharge", `{"amount_money":100}`)}}
	stats := &TurnTelemetry{}
	ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
	ses := &session{}
	_, trace, err := h.process(ctx, "pure-v2-write", 42, "充值100元", ses)
	if err != nil || stats.JevCalls != 1 || ses.PendingAction == nil || len(fa.posts) != 0 || len(trace) != 1 || trace[0].Phase != "preview" || trace[0].DurationMS <= 0 {
		t.Fatalf("write bypassed shared preview or added JEV guard: stats=%+v trace=%+v err=%v", stats, trace, err)
	}
	_, trace, err = h.process(ctx, "pure-v2-write", 42, "确认", ses)
	if err != nil || stats.JevCalls != 1 || ses.PendingAction != nil || len(fa.posts) != 2 || trace[0].Phase != "execute" || trace[0].Outcome != "success" {
		t.Fatalf("confirmation diverged: trace=%+v err=%v", trace, err)
	}
}

func TestNoPendingCannotInviteConfirmation(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{
		contentMsg("回复确认即可充值100元。"),
		toolCallMsg("repair-1", "recharge", `{"amount_money":100}`),
	}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "no-fake-confirm", 42, "充值100元", ses)
	if err != nil || ses.PendingAction == nil || !strings.Contains(reply, "确认") {
		t.Fatalf("confirmation invitation must have actual pending preview: %q pending=%+v err=%v", reply, ses.PendingAction, err)
	}
}

func TestGivenAmountDoesNotGetAskedAgain(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{
		contentMsg("请问您要充值多少元？"),
		toolCallMsg("repair-2", "recharge", `{"amount_money":100}`),
	}}
	ses := &session{}
	_, _, err := h.process(context.Background(), "amount-present", 42, "帮我充值100元", ses)
	if err != nil || ses.PendingAction == nil || ses.PendingAction.Tool != "recharge" {
		t.Fatalf("explicit amount was lost: pending=%+v err=%v", ses.PendingAction, err)
	}
}

func TestNecessaryAmountClarificationIsKept(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{contentMsg("请问您要充值多少元？")}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "amount-missing", 42, "帮我充值", ses)
	if err != nil || ses.PendingAction != nil || !strings.Contains(reply, "多少") {
		t.Fatalf("necessary clarification changed: %q %v", reply, err)
	}
}

func TestExistingPendingIsNotSilentlyReplaced(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("new-write", "recharge", `{"amount_money":200}`)}}
	old := &pendingAction{Tool: "recharge", Args: map[string]any{"amount_money": 100}, Preview: "充值100元", TenantID: 42}
	ses := &session{PendingAction: old}
	reply, _, err := h.process(context.Background(), "preserve-pending", 42, "另外充值200元", ses)
	if err != nil || ses.PendingAction != old || !strings.Contains(reply, "取消") {
		t.Fatalf("new request silently replaced preview: %q pending=%+v err=%v", reply, ses.PendingAction, err)
	}
}

func TestCompositeConsultationSurvivesWritePreview(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.MeView{Subscription: &store.Subscription{PlanID: 3}})
	}))
	defer server.Close()
	h.tb = NewToolbox(NewAdminClient(server.URL))
	msg := toolCallMsg("cancel-renew", "cancel_subscription", `{}`)
	msg.Content = "余额退款可提供充值订单号或退款金额；退订仅停止下期续费，当期不退。"
	h.llm = &scriptedLLM{steps: []*Message{msg}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "composite-help", 42, "不续了，余额怎么退", ses)
	if err != nil || ses.PendingAction == nil || !strings.Contains(reply, "订单号或退款金额") {
		t.Fatalf("preview dropped other request: %q pending=%+v err=%v", reply, ses.PendingAction, err)
	}
}

func TestCompositeRefundQuestionIsKeptWhenModelOnlyCallsCancel(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(store.MeView{Subscription: &store.Subscription{PlanID: 3}})
	}))
	defer server.Close()
	h.tb = NewToolbox(NewAdminClient(server.URL))
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("cancel-only", "cancel_subscription", `{}`)}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "composite-without-content", 42, "不续了，余额怎么退？", ses)
	if err != nil || ses.PendingAction == nil || !strings.Contains(reply, "订单号或退款金额") {
		t.Fatalf("empty model explanation drops refund question: %q err=%v", reply, err)
	}
}

func TestRefundDirectionNeedsClarification(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("wrong-direction", "refund_recharge", `{"amount_money":100}`)}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "refund-direction", 42, "退100到余额", ses)
	if err != nil || ses.PendingAction != nil || len(fa.posts) != 0 || !strings.Contains(reply, "方向") {
		t.Fatalf("ambiguous refund was previewed: %q pending=%+v err=%v", reply, ses.PendingAction, err)
	}
}

func TestConfirmationRepairIsBounded(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{contentMsg("回复确认即可办理。"), contentMsg("回复确认即可办理。")}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "bounded-repair", 42, "充值100元", ses)
	if err != nil || ses.PendingAction != nil || strings.Contains(reply, "确认即可") || !strings.Contains(reply, "没有") {
		t.Fatalf("repair must not loop or return false invitation: %q err=%v", reply, err)
	}
}

func TestConsultationCannotCreateWritePreview(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("consult-not-authorized", "refund_recharge", `{"amount_money":100}`)}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "consult-only", 42, "怎么退款100元", ses)
	if err != nil || ses.PendingAction != nil || !strings.Contains(reply, "尚未") {
		t.Fatalf("consultation became a write: %q pending=%+v err=%v", reply, ses.PendingAction, err)
	}
}

func TestConsultationWithExplicitApplicationCanPreview(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("consult-and-request", "recharge", `{"amount_money":100}`)}}
	ses := &session{}
	_, _, err := h.process(context.Background(), "consult-and-apply", 42, "怎么充值？直接帮我充值100元。", ses)
	if err != nil || ses.PendingAction == nil || ses.PendingAction.Tool != "recharge" {
		t.Fatalf("explicit application overridden by consultation: pending=%+v err=%v", ses.PendingAction, err)
	}
}

func TestCreditsDoNotBecomeUnspecifiedYuan(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("ambiguous-credit", "recharge", `{"amount_money":100}`)}}
	ses := &session{}
	reply, _, err := h.process(context.Background(), "credit-unit", 42, "add 100 credits", ses)
	if err != nil || ses.PendingAction != nil || !strings.Contains(reply, "单位") {
		t.Fatalf("credits guessed as yuan: %q pending=%+v err=%v", reply, ses.PendingAction, err)
	}
}
