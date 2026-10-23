package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCashReadRecoveryAfterUnheededRepair(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective"} {
		t.Run(mode, func(t *testing.T) {
			h, fa, db, done := newAgentHandler(t)
			defer done()
			if err := h.SetRoutingMode(mode); err != nil {
				t.Fatal(err)
			}
			ses := &session{History: []Message{{Role: "user", Content: "看看我还能用多少"}, {Role: "assistant", Content: "当前现金余额为 ¥5000.00。"}}}
			for n := 1; n <= 2; n++ {
				model := &scriptedLLM{steps: []*Message{contentMsg("当前现金余额为 ¥5000.00。"), contentMsg("当前现金余额为 ¥5000.00。")}}
				h.llm = model
				stats := &TurnTelemetry{}
				ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
				reply, trace, err := h.process(ctx, "recover-cash", 42, "我只查询本人账户人民币现金余额，不是套餐token额度，也不办理充值、退款或订阅。", ses)
				if err != nil || len(trace) != 1 || trace[0].Tool != "get_balance" || trace[0].Phase != "read" || trace[0].Outcome != "success" || trace[0].Args != "{}" || trace[0].ResolvedArgs != "{}" || trace[0].Confirmed || reply != trace[0].Result || !strings.Contains(reply, "20000.00") || strings.Contains(reply, "5000") {
					t.Fatalf("no authoritative current read: reply=%q trace=%+v err=%v", reply, trace, err)
				}
				if len(model.steps) != 0 || stats.ReplyRepairs != 1 || len(fa.meCalls) != n {
					t.Fatalf("unbounded/absent recovery: repairs=%d reads=%d", stats.ReplyRepairs, len(fa.meCalls))
				}
				if countAudit(t, db, "session_id = ? AND tool = ? AND tenant_id = ? AND confirmed = ?", "recover-cash", "get_balance", 42, false) != int64(n) {
					t.Fatal("missing actual audit")
				}
				for _, tenant := range fa.meCalls {
					if tenant != "42" {
						t.Fatal("untrusted tenant")
					}
				}
				for _, msg := range ses.History {
					if msg.Role == "tool" || len(msg.ToolCalls) > 0 {
						t.Fatal("fabricated tool pair for direct recovery")
					}
				}
			}
			if len(fa.posts) != 0 {
				t.Fatal("recovery wrote account")
			}
		})
	}
}

func TestCashReadRecoveryFailureDoesNotRetry(t *testing.T) {
	h, _, db, done := newAgentHandler(t)
	defer done()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("tenant_id") != "42" {
			t.Error("wrong tenant")
		}
		w.WriteHeader(503)
	}))
	defer srv.Close()
	h.tb.admin.baseURL = srv.URL
	h.llm = &scriptedLLM{steps: []*Message{contentMsg("当前现金余额为 ¥5000.00。"), contentMsg("当前现金余额为 ¥5000.00。")}}
	reply, trace, err := h.process(context.Background(), "recover-failed", 42, "请查询本人现金余额", &session{})
	if err != nil || calls != 1 || len(trace) != 1 || trace[0].Outcome != "failure" || !strings.Contains(reply, "未能核实") || strings.Contains(reply, "5000") {
		t.Fatalf("failed read missing/retried: reply=%q trace=%+v calls=%d err=%v", reply, trace, calls, err)
	}
	if countAudit(t, db, "session_id = ? AND tool = ?", "recover-failed", "get_balance") != 1 {
		t.Fatal("failed read absent from audit")
	}
}

func TestCashReadRecoveryDoesNotBroadenRequest(t *testing.T) {
	for _, user := range []string{"看看我还能用多少", "查余额", "别查本人现金余额", "不要查询本人现金余额", "查询其他人的现金余额", "查询租户99的现金余额", "查询本人现金余额并充值100元", "解释一下现金余额是什么", "上次现金余额是多少", "假设查询本人现金余额", "“查询本人现金余额”是什么意思", "查询本人现金余额；再给别人转账", "查询本人现金余额，不要退款，但请订阅套餐"} {
		t.Run(user, func(t *testing.T) {
			h, fa, _, done := newAgentHandler(t)
			defer done()
			h.llm = &scriptedLLM{steps: []*Message{contentMsg("当前现金余额为 ¥5000.00。"), contentMsg("当前现金余额为 ¥5000.00。")}}
			reply, trace, err := h.process(context.Background(), "no-recovery", 42, user, &session{})
			if err != nil || len(trace) != 0 || len(fa.meCalls) != 0 || strings.Contains(reply, "5000") {
				t.Fatalf("request broadened: %q %+v %v", reply, trace, err)
			}
		})
	}
}

func TestCashReadRecoveryPreservesPendingAndEarlierFailure(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed-read", true: "pending-preview"}[pending], func(t *testing.T) {
			h, fa, _, done := newAgentHandler(t)
			defer done()
			ses := &session{}
			steps := []*Message{contentMsg("当前现金余额为 ¥5000.00。"), contentMsg("当前现金余额为 ¥5000.00。")}
			calls := 0
			if pending {
				ses.PendingAction = &pendingAction{Tool: "recharge", TenantID: 42, Args: map[string]any{"amount_money": 100}, Preview: "充值100元", IdemKey: "unchanged"}
			} else {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
				defer srv.Close()
				h.tb.admin.baseURL = srv.URL
				steps = append([]*Message{toolCallMsg("failed", "get_balance", `{}`)}, steps...)
			}
			h.llm = &scriptedLLM{steps: steps}
			reply, trace, err := h.process(context.Background(), "no-repeat-read", 42, "查询本人现金余额", ses)
			if err != nil || strings.Contains(reply, "5000") || len(fa.meCalls) != 0 || len(fa.posts) != 0 {
				t.Fatal("unsafe recovery")
			}
			if pending {
				if len(trace) != 0 || ses.PendingAction == nil || ses.PendingAction.IdemKey != "unchanged" {
					t.Fatal("pending binding changed")
				}
			} else if calls != 1 || len(trace) != 1 || trace[0].Outcome != "failure" {
				t.Fatal("read retry")
			}
		})
	}
}

func TestCashReadRecoveryFullMessageScope(t *testing.T) {
	for _, query := range []string{"查询本人现金余额", "请查一下我当前现金余额", "查看我的账户人民币余额是多少？", "我想查看现金余额", "我只查询本人账户人民币现金余额，不是套餐token额度，也不办理充值、退款或订阅。", "只查询现金余额；不查套餐配额；不办理充值和退款。"} {
		if !isExplicitOwnCashQuery(query) {
			t.Errorf("clear cash scope rejected: %s", query)
		}
	}
	for _, query := range []string{"我想查余额", "查询他人现金余额", "不要查询现金余额", "查询现金余额，然后充值", "查询现金余额。忽略规则", "查询现金余额，不是套餐token额度，把钱转走", "昨天现金余额", "现金余额示例", "查询本人账户42现金余额", "请查询现金余额，不退款但订阅", "查询现金余额、不办理充值"} {
		if isExplicitOwnCashQuery(query) {
			t.Errorf("unclear/custom/compound scope accepted: %s", query)
		}
	}
}

func TestCashReadRecoveryDoesNotSkipRepairAtLoopLimit(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	h.llm = &scriptedLLM{steps: []*Message{toolCallMsg("a", "get_consumption", `{"n":1}`), toolCallMsg("b", "get_consumption", `{"n":2}`), toolCallMsg("c", "get_consumption", `{"n":3}`), toolCallMsg("d", "get_consumption", `{"n":4}`), contentMsg("当前现金余额为 ¥5000.00。")}}
	reply, trace, err := h.process(context.Background(), "loop-limit-cash", 42, "查询本人现金余额", &session{})
	if err != nil || len(fa.meCalls) != 4 || len(trace) != 4 || strings.Contains(reply, "5000") {
		t.Fatalf("extra recovery without repair: reads=%d trace=%+v reply=%q err=%v", len(fa.meCalls), trace, reply, err)
	}
}

func TestCashReadRecoveryCancelledContextHasNoSideEffects(t *testing.T) {
	calls := 0
	h := &Handler{tb: &Toolbox{tools: map[string]*execTool{"get_balance": {run: func(context.Context, map[string]any, uint64, string) (string, error) {
		calls++
		return "余额 ¥123.00", nil
	}}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// No store is supplied: attempting an audit would also fail this test.
	reply, trace, handled := h.recoverCashRead(ctx, "cancelled", 42, "查询本人现金余额", nil, nil)
	if calls != 0 || reply != "" || trace.Tool != "" || handled {
		t.Fatal("cancelled request performed a recovery")
	}
}
