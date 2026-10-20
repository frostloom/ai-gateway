package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

type contextPrefilterJudge struct{ calls int }

func (j *contextPrefilterJudge) Evaluate(context.Context, any, map[string]jev.Question) (*jev.Result, error) {
	j.calls++
	return &jev.Result{Answers: fastVerdict("get_balance", .99).Answers}, nil
}

func TestSelectiveContextPrefilterModes(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective", "jev_hint", "legacy"} {
		for _, tc := range []struct {
			name string
			ses  *session
			skip bool
		}{
			{"nil", nil, false},
			{"empty", &session{}, false},
			{"first", &session{History: []Message{{Role: "user", Content: "查余额"}}}, false},
			{"multi", &session{History: []Message{{Role: "assistant", Content: "上一轮"}, {Role: "user", Content: "查余额"}}}, true},
			{"pending", &session{PendingAction: &pendingAction{Tool: "recharge", TenantID: 42}}, true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				h := &Handler{}
				configureRouting(t, h, mode)
				judge := &policyJudge{}
				h.SetJev(judge)
				stats := &TurnTelemetry{}
				ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
				v := h.evaluateTurn(ctx, "查余额", tc.ses)
				want := 1
				if mode == "model" || (mode == "jev_selective" && tc.skip) {
					want = 0
				}
				if judge.calls != want || stats.JevCalls != want {
					t.Fatalf("judge=%d telemetry=%d want=%d", judge.calls, stats.JevCalls, want)
				}
				if want == 0 && (v.Available || v.Err != nil || v.Hint != "" || v.Block || v.Warn || stats.JevLatencyMS != 0 || stats.JevErrors != 0 || stats.JevUsageMissing != 0) {
					t.Fatalf("skipped judge was recorded as a failure: verdict=%+v stats=%+v", v, stats)
				}
				if mode == "jev_selective" && tc.skip && h.fastPathReason("查余额", tc.ses, v) != "conversation_context" {
					t.Fatal("deterministic context exclusion must not be reported as judge_unavailable")
				}
			})
		}
	}
}

func TestSelectiveContextPrefilterPreservesModelInputsAndTools(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	for _, scenario := range []string{"read", "clarify", "pending_other"} {
		t.Run(scenario, func(t *testing.T) {
			var baselineRequests []string
			var baselineReply string
			for _, mode := range []string{"model", "jev_selective"} {
				configureRouting(t, h, mode)
				judge := &contextPrefilterJudge{}
				h.SetJev(judge)
				ses := &session{History: []Message{{Role: "user", Content: "你好"}, {Role: "assistant", Content: "请问需要什么帮助？"}}}
				message := "查余额"
				llm := &observedLLM{script: scriptedLLM{steps: []*Message{toolCallMsg("read", "get_balance", `{}`), contentMsg("当前余额¥20000.00。")}}}
				if scenario == "clarify" {
					message = "我要充值"
					llm.script.steps = []*Message{contentMsg("请问要充值多少元？")}
				} else if scenario == "pending_other" {
					ses.PendingAction = &pendingAction{Tool: "recharge", TenantID: 42, Args: map[string]any{"amount_money": 100}, Preview: "将模拟充值100元"}
					message = "先说说这个操作"
					llm.script.steps = []*Message{contentMsg("这是模拟充值100元，回复确认或取消。")}
				}
				h.llm = llm
				stats := &TurnTelemetry{}
				ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
				reply, trace, err := h.process(ctx, "prefilter-"+scenario+"-"+mode, 42, message, ses)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "read" && (len(trace) != 1 || trace[0].Tool != "get_balance" || trace[0].Confirmed) {
					t.Fatalf("read tool changed: %+v", trace)
				}
				if scenario != "read" && len(trace) != 0 {
					t.Fatalf("clarification or pending explanation executed a tool: %+v", trace)
				}
				if scenario == "pending_other" && (ses.PendingAction == nil || ses.PendingAction.TenantID != 42) {
					t.Fatal("pending operation lost")
				}
				if mode == "model" {
					baselineRequests, baselineReply = llm.requests, reply
				} else if !reflect.DeepEqual(baselineRequests, llm.requests) || reply != baselineReply || stats.JevCalls != 0 || stats.JevLatencyMS != 0 || stats.JevErrors != 0 || stats.JevFastPath || stats.JevFastPathReason != "conversation_context" || judge.calls != 0 {
					t.Fatalf("prefilter changed fallback or called judge: requests=%v stats=%+v", llm.requests, stats)
				}
			}
		})
	}
	if len(fa.posts) != 0 {
		t.Fatalf("unexpected writes: %+v", fa.posts)
	}
	for _, tenant := range fa.meCalls {
		if tenant != "42" {
			t.Fatalf("tenant boundary escaped: %q", tenant)
		}
	}
}

func TestSelectiveContextPrefilterRuntimeIsDistinguishable(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective", "jev_hint", "legacy"} {
		h := &Handler{}
		configureRouting(t, h, mode)
		raw, err := json.Marshal(h.runtimeMetadata())
		if err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(raw, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata["routing_revision"] != "selective-context-prefilter-v1" {
			t.Fatalf("new routing implementation indistinguishable in %s: %s", mode, raw)
		}
	}
}

func TestSelectiveContextPrefilterPreservesPendingTransitions(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	configureRouting(t, h, "jev_selective")
	for _, message := range []string{"确认", "取消"} {
		t.Run(message, func(t *testing.T) {
			judge := &contextPrefilterJudge{}
			h.SetJev(judge)
			h.llm = &scriptedLLM{}
			ses := &session{PendingAction: &pendingAction{Tool: "recharge", TenantID: 42, Args: map[string]any{"amount_money": float64(100)}, Preview: "将模拟充值100元", IdemKey: "prefilter-confirm"}}
			before := len(fa.posts)
			stats := &TurnTelemetry{}
			ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
			_, trace, err := h.process(ctx, "prefilter-pending", 42, message, ses)
			if err != nil || ses.PendingAction != nil || judge.calls != 0 || stats.JevCalls != 0 || stats.LLMCalls != 0 {
				t.Fatalf("confirmation state changed: err=%v pending=%+v stats=%+v", err, ses.PendingAction, stats)
			}
			if message == "确认" {
				if len(fa.posts)-before != 2 || len(trace) != 1 || !trace[0].Confirmed || trace[0].Outcome != "success" {
					t.Fatalf("confirmation did not execute once: posts=%+v trace=%+v", fa.posts, trace)
				}
			} else if len(fa.posts) != before || len(trace) != 0 {
				t.Fatal("cancellation executed a write")
			}
		})
	}
}
