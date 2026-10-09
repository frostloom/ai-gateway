package agent

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

type policyJudge struct {
	calls int
	fail  bool
}

func (j *policyJudge) Evaluate(_ context.Context, _ any, _ map[string]jev.Question) (*jev.Result, error) {
	j.calls++
	if j.fail {
		return nil, errors.New("offline judge failure")
	}
	s := fastVerdict("get_balance", .98)
	s.Answers["legit"] = jev.AnswerNoul(.1)
	return &jev.Result{Answers: s.Answers}, nil
}

func configureRouting(t *testing.T, h *Handler, mode string) {
	t.Helper()
	p, ok := any(h).(interface{ SetRoutingMode(string) error })
	if !ok {
		t.Fatal("routing policy is missing")
	}
	if err := p.SetRoutingMode(mode); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingModesSeparateJevFromExecutionSafety(t *testing.T) {
	for _, mode := range []string{"model", "jev_selective", "jev_hint", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			h := &Handler{}
			configureRouting(t, h, mode)
			j := &policyJudge{}
			h.SetJev(j)
			v := h.evaluateTurn(context.Background(), "查余额", nil)
			if mode == "model" {
				if j.calls != 0 || v.Available {
					t.Fatal("V1 must never call JEV")
				}
			} else if j.calls != 1 {
				t.Fatalf("calls=%d", j.calls)
			}
			if mode != "legacy" && (v.Block || v.Warn) {
				t.Fatal("main/ablation routing added a safety verdict")
			}
			if mode == "jev_selective" && v.Hint != "" {
				t.Fatal("selective fallback must not inject hint")
			}
			before := j.calls
			h.guardWrite(context.Background(), 42, "recharge", map[string]any{"amount": 100}, "充值100")
			if mode != "legacy" && before != j.calls {
				t.Fatal("extra write judge contaminates routing experiment")
			}
		})
	}
}

func TestRoutingPolicyValidation(t *testing.T) {
	h := &Handler{}
	p, ok := any(h).(interface {
		SetRoutingMode(string) error
		RoutingMode() RoutingMode
	})
	if !ok {
		t.Fatal("routing policy is missing")
	}
	if p.RoutingMode() != RoutingLegacy {
		t.Fatal("zero Handler must preserve explicit historical tests")
	}
	if p.SetRoutingMode("unknown") == nil {
		t.Fatal("invalid mode accepted")
	}
	configureRouting(t, h, "jev_selective")
	if !h.jevFastPathConfigured || !h.jevFastPathEnabled || h.jevFastPathMin != .9 {
		t.Fatal("selective policy must enable fixed-default read routing")
	}
}

func TestRoutingJudgeFailureFallsBack(t *testing.T) {
	h := &Handler{}
	configureRouting(t, h, "jev_selective")
	j := &policyJudge{fail: true}
	h.SetJev(j)
	v := h.evaluateTurn(context.Background(), "查余额", nil)
	if j.calls != 1 || v.Available || v.Err == nil || v.Block || v.Warn || v.Hint != "" {
		t.Fatalf("unsafe failure fallback: %+v calls=%d", v, j.calls)
	}
}

func TestSelectiveThresholdCannotLowerContractFloor(t *testing.T) {
	h := &Handler{}
	configureRouting(t, h, "jev_selective")
	if h.SetJevFastPath(true, .89) == nil {
		t.Fatal("selective accepts confidence below frozen contract floor")
	}
	h = &Handler{}
	if err := h.SetJevFastPath(true, .89); err != nil {
		t.Fatal(err)
	}
	if h.SetRoutingMode("jev_selective") == nil {
		t.Fatal("mode switch accepts confidence below contract floor")
	}
}

func TestSelectiveRoutingAcceptanceBoundaries(t *testing.T) {
	h := &Handler{tb: NewToolbox(nil)}
	configureRouting(t, h, "jev_selective")
	h.SetJev(&policyJudge{})
	base := jevVerdict{Available: true, FastFieldsValid: true, Intent: "get_balance", IntentConf: .9, SingleRead: true, ShapeConf: .9, LegitProb: .85, RiskScore: 0, RiskConf: .8}
	for _, tc := range []struct {
		name, message string
		mutate        func(*jevVerdict, *session)
		want          string
	}{
		{"inclusive_floor", "查余额", nil, "eligible"},
		{"intent_below", "查余额", func(v *jevVerdict, _ *session) { v.IntentConf = math.Nextafter(.9, 0) }, "intent_uncertain"},
		{"shape_below", "查余额", func(v *jevVerdict, _ *session) { v.ShapeConf = math.Nextafter(.9, 0) }, "not_single_parameterless_read"},
		{"legit_below", "查余额", func(v *jevVerdict, _ *session) { v.LegitProb = math.Nextafter(.85, 0) }, "safety_not_clear"},
		{"risk_exclusive", "查余额", func(v *jevVerdict, _ *session) { v.RiskScore = .5 }, "safety_not_clear"},
		{"nonfinite", "查余额", func(v *jevVerdict, _ *session) { v.IntentConf = math.NaN() }, "intent_uncertain"},
		{"write", "退款100", func(v *jevVerdict, _ *session) { v.Intent = "refund_recharge" }, "tool_not_supported"},
		{"compound", "查余额并且充值100", nil, "compound_or_action_or_negation"},
		{"context", "查余额", func(_ *jevVerdict, s *session) {
			s.History = append(s.History, Message{Role: "assistant", Content: "你好"})
		}, "conversation_context"},
		{"custom_time", "今天消耗是多少", func(v *jevVerdict, _ *session) { v.Intent = "get_consumption" }, "parameters_or_custom_scope"},
		{"schema", "查余额", func(v *jevVerdict, _ *session) { v.FastFieldsValid = false }, "judge_schema_invalid"},
		{"fault", "查余额", func(v *jevVerdict, _ *session) { v.Available = false }, "judge_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			s := &session{History: []Message{{Role: "user", Content: tc.message}}}
			if tc.mutate != nil {
				tc.mutate(&v, s)
			}
			if got := h.fastPathReason(tc.message, s, v); got != tc.want {
				t.Fatalf("reason=%s want %s", got, tc.want)
			}
		})
	}
	for _, name := range []string{"get_balance", "get_consumption", "list_plans", "get_my_subscription"} {
		v := base
		v.Intent = name
		if got := h.fastPathReason("查询", &session{History: []Message{{Role: "user", Content: "查询"}}}, v); got != "eligible" {
			t.Fatalf("whitelist %s: %s", name, got)
		}
	}
	h.tb.tools["get_balance"].confirm = true
	if got := h.fastPathReason("查余额", &session{History: []Message{{Role: "user", Content: "查余额"}}}, base); got != "tool_not_supported" {
		t.Fatal("write tool masquerading as read accepted")
	}
}
