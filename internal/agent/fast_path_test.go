package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func enableFastPath(t *testing.T, h *Handler, enabled bool, threshold float64) {
	t.Helper()
	config, ok := any(h).(interface{ SetJevFastPath(bool, float64) error })
	if !ok {
		t.Fatal("handler has no high-confidence business fast path")
	}
	if err := config.SetJevFastPath(enabled, threshold); err != nil {
		t.Fatal(err)
	}
}

func fastVerdict(intent string, confidence float64) jev.FakeScript {
	s := verdictScript(.95, intent, confidence, .1, .95)
	s.Answers["route_shape"] = jev.AnswerChoice("single_read", .97)
	return s
}

func TestJevFastPathReadWithoutLLM(t *testing.T) {
	h, fa, db, done := newAgentHandler(t)
	defer done()
	enableFastPath(t, h, true, .9)
	for i, tc := range []struct{ tool, message, want string }{
		{"get_balance", "查一下余额", "¥20000.00"},
		{"get_consumption", "最近七天消耗是多少", "近 7 天没有消耗记录"},
		{"list_plans", "有哪些套餐可以选", "月度·标准"},
		{"get_my_subscription", "我当前订阅是什么", "没有生效中的订阅"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			h.SetJev(jev.NewFake(fastVerdict(tc.tool, .98)))
			h.llm = &scriptedLLM{}
			sid := fmt.Sprintf("sid-fast-read-%d", i)
			rec := cancellationRequest(h, sid, tc.message)
			if rec.Code != 200 {
				t.Fatalf("fast path made unnecessary LLM request: HTTP %d %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Reply     string         `json:"reply"`
				Trace     []traceItem    `json:"trace"`
				Pending   bool           `json:"pending_confirm"`
				Telemetry map[string]any `json:"telemetry"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body.Reply, tc.want) || body.Pending || len(body.Trace) != 1 || body.Trace[0].Tool != tc.tool || body.Trace[0].Confirmed {
				t.Fatalf("wrong real tool reply/trace: %s", rec.Body.String())
			}
			if body.Telemetry["jev_fast_path"] != true || body.Telemetry["llm_calls"] != float64(0) || body.Telemetry["jev_calls"] != float64(1) {
				t.Fatalf("wrong efficiency telemetry: %+v", body.Telemetry)
			}
			if body.Telemetry["jev_intent_confidence"] != .98 {
				t.Fatalf("intent confidence lost: %+v", body.Telemetry)
			}
			saved, err := h.loadSession(context.Background(), sid)
			if err != nil || len(saved.History) != 2 || saved.History[1].Content != body.Reply || len(saved.History[1].ToolCalls) != 0 {
				t.Fatalf("history not reusable after fast path: %+v %v", saved, err)
			}
			if got := countAudit(t, db, "session_id = ? AND tool = ? AND confirmed = ?", sid, tc.tool, false); got != 1 {
				t.Fatalf("read audit rows=%d", got)
			}
		})
	}
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if len(fa.posts) != 0 {
		t.Fatalf("read fast path performed writes: %v", fa.posts)
	}
	for _, tenant := range fa.meCalls {
		if tenant != "42" {
			t.Fatalf("tenant escaped identity: %s", tenant)
		}
	}
}

func TestJevFastPathFallbackBoundaries(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	for _, tc := range []struct {
		name, message string
		configure     func(*jev.FakeScript, *session)
		enabled       bool
	}{
		{"disabled", "查余额", nil, false},
		{"quota_ambiguous", "剩余额度", nil, true},
		{"quota_indirect", "我的额度还有多少没用", nil, true},
		{"quota_english", "remaining quota", nil, true},
		{"malformed_risk", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["risk"] = jev.AnswerChoice("high", .99) }, true},
		{"malformed_legit", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["legit"] = jev.AnswerChoice("yes", .99) }, true},
		{"low_confidence", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["intent"] = jev.AnswerChoice("get_balance", .89) }, true},
		{"ambiguous_shape", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["route_shape"] = jev.AnswerChoice("complex", .99) }, true},
		{"missing_shape", "查余额", func(v *jev.FakeScript, s *session) { delete(v.Answers, "route_shape") }, true},
		{"low_legitimacy", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["legit"] = jev.AnswerNoul(.65) }, true},
		{"uncertain_risk", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["risk"] = jev.AnswerScore(.1, .3, nil) }, true},
		{"warning_risk", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["risk"] = jev.AnswerScore(1, .99, nil) }, true},
		{"compound_write", "先查余额，再充值100元", nil, true},
		{"compound_read", "查余额以及当前订阅", nil, true},
		{"negated_read", "不要查余额", nil, true},
		{"custom_period", "本月消耗是多少", func(v *jev.FakeScript, s *session) { v.Answers["intent"] = jev.AnswerChoice("get_consumption", .99) }, true},
		{"custom_days", "查最近三天消耗", func(v *jev.FakeScript, s *session) { v.Answers["intent"] = jev.AnswerChoice("get_consumption", .99) }, true},
		{"explicit_filter", "看看300元的套餐", func(v *jev.FakeScript, s *session) { v.Answers["intent"] = jev.AnswerChoice("list_plans", .99) }, true},
		{"contextual", "查余额", func(v *jev.FakeScript, s *session) {
			s.History = []Message{{Role: "user", Content: "上一轮"}, {Role: "assistant", Content: "上一轮回复"}}
		}, true},
		{"pending_write", "查余额", func(v *jev.FakeScript, s *session) { s.PendingAction = &pendingAction{Tool: "recharge", TenantID: 42} }, true},
		{"optional_parameter_tool", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["intent"] = jev.AnswerChoice("get_recent_bills", .99) }, true},
		{"unknown_tool", "查余额", func(v *jev.FakeScript, s *session) { v.Answers["intent"] = jev.AnswerChoice("danger_tool", .99) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enableFastPath(t, h, tc.enabled, .9)
			v := fastVerdict("get_balance", .98)
			s := &session{}
			if tc.configure != nil {
				tc.configure(&v, s)
			}
			h.SetJev(jev.NewFake(v))
			h.llm = &scriptedLLM{steps: []*Message{contentMsg("保留完整客服处理。")}}
			stats := &TurnTelemetry{}
			ctx := context.WithValue(context.Background(), telemetryKey{}, stats)
			reply, trace, err := h.process(ctx, "sid-fast-fallback-"+tc.name, 42, tc.message, s)
			if err != nil || reply != "保留完整客服处理。" || len(trace) != 0 {
				t.Fatalf("unsafe direct route: reply=%s trace=%v err=%v", reply, trace, err)
			}
			raw, _ := json.Marshal(stats)
			var telemetry map[string]any
			_ = json.Unmarshal(raw, &telemetry)
			if telemetry["jev_fast_path"] == true || telemetry["jev_fast_path_reason"] == "" {
				t.Fatalf("missing truthful fallback reason: %s", raw)
			}
		})
	}
}

func TestJevFastPathToolFailureIsNotInventedSuccess(t *testing.T) {
	h, _, db, done := newAgentHandler(t)
	defer done()
	enableFastPath(t, h, true, .9)
	h.SetJev(jev.NewFake(fastVerdict("get_balance", .98)))
	h.llm = &scriptedLLM{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	h.tb.admin = NewAdminClient(server.URL)
	rec := cancellationRequest(h, "sid-fast-tool-error", "查询余额")
	if rec.Code == 200 {
		t.Fatalf("failed real tool reported success: %s", rec.Body.String())
	}
	var body struct {
		Error     string         `json:"error"`
		SessionID string         `json:"session_id"`
		Trace     []traceItem    `json:"trace"`
		Telemetry *TurnTelemetry `json:"telemetry"`
		Pending   *bool          `json:"pending_confirm"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 500 || body.Error == "" || body.SessionID != "sid-fast-tool-error" || body.Pending == nil || *body.Pending || len(body.Trace) != 1 || body.Trace[0].Tool != "get_balance" || body.Telemetry == nil || !body.Telemetry.JevFastPath || body.Telemetry.JevFastPathReason != "tool_error" || body.Telemetry.JevCalls != 1 || body.Telemetry.LLMCalls != 0 {
		t.Fatalf("failed read lost telemetry, trace or session evidence: %s", rec.Body.String())
	}
	if got := countAudit(t, db, "session_id = ? AND tool = ? AND confirmed = ?", "sid-fast-tool-error", "get_balance", false); got != 1 {
		t.Fatalf("failed query missing audit rows=%d", got)
	}
}

func TestJevFastPathThresholdValidation(t *testing.T) {
	h := &Handler{}
	config, ok := any(h).(interface{ SetJevFastPath(bool, float64) error })
	if !ok {
		t.Fatal("missing fast path configuration")
	}
	for _, v := range []float64{.79, 1.01, math.NaN(), math.Inf(1)} {
		if config.SetJevFastPath(true, v) == nil {
			t.Fatalf("accepted unsafe threshold %v", v)
		}
	}
	if err := config.SetJevFastPath(true, .9); err != nil {
		t.Fatal(err)
	}
}
