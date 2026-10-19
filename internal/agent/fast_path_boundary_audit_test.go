package agent

import (
	"math"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// A deterministic audit of the execution gate, not an estimate of JEV accuracy.
// No evaluator, model, database or HTTP request is executed.
func TestSelectiveGateExactBoundaries(t *testing.T) {
	h := &Handler{tb: NewToolbox(nil)}
	h.SetJev(jev.NewFake())
	if err := h.SetRoutingMode("jev_selective"); err != nil {
		t.Fatal(err)
	}
	base := jevVerdict{Available: true, FastFieldsValid: true, Intent: "get_balance", IntentConf: .9, SingleRead: true, ShapeConf: .9, LegitProb: .85, RiskConf: .8, RiskScore: 0}
	for _, tc := range []struct {
		name, want string
		change     func(*jevVerdict, *session)
	}{
		{"inclusive_minimums", "eligible", nil},
		{"intent_just_below", "intent_uncertain", func(v *jevVerdict, _ *session) { v.IntentConf = math.Nextafter(.9, 0) }},
		{"shape_just_below", "not_single_parameterless_read", func(v *jevVerdict, _ *session) { v.ShapeConf = math.Nextafter(.9, 0) }},
		{"legit_just_below", "safety_not_clear", func(v *jevVerdict, _ *session) { v.LegitProb = math.Nextafter(.85, 0) }},
		{"risk_conf_just_below", "safety_not_clear", func(v *jevVerdict, _ *session) { v.RiskConf = math.Nextafter(.8, 0) }},
		{"risk_just_below_half", "eligible", func(v *jevVerdict, _ *session) { v.RiskScore = math.Nextafter(.5, 0) }},
		{"risk_at_half", "safety_not_clear", func(v *jevVerdict, _ *session) { v.RiskScore = .5 }},
		{"intent_nan", "intent_uncertain", func(v *jevVerdict, _ *session) { v.IntentConf = math.NaN() }},
		{"shape_inf", "not_single_parameterless_read", func(v *jevVerdict, _ *session) { v.ShapeConf = math.Inf(1) }},
		{"risk_nan", "safety_not_clear", func(v *jevVerdict, _ *session) { v.RiskScore = math.NaN() }},
		{"negative_risk", "safety_not_clear", func(v *jevVerdict, _ *session) { v.RiskScore = -.01 }},
		{"unavailable", "judge_unavailable", func(v *jevVerdict, _ *session) { v.Available = false }},
		{"invalid_schema", "judge_schema_invalid", func(v *jevVerdict, _ *session) { v.FastFieldsValid = false }},
		{"pending_refund", "conversation_context", func(_ *jevVerdict, s *session) { s.PendingAction = &pendingAction{Tool: "refund_recharge"} }},
		{"followup", "conversation_context", func(_ *jevVerdict, s *session) { s.History = append(s.History, Message{Role: "assistant"}) }},
		{"write_intent", "tool_not_supported", func(v *jevVerdict, _ *session) { v.Intent = "recharge" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			s := &session{History: []Message{{Role: "user", Content: "查余额"}}}
			if tc.change != nil {
				tc.change(&v, s)
			}
			if got := h.fastPathReason("查余额", s, v); got != tc.want {
				t.Fatalf("reason=%s want=%s", got, tc.want)
			}
		})
	}
	for _, intent := range []string{"get_balance", "get_consumption", "list_plans", "get_my_subscription"} {
		v := base
		v.Intent = intent
		if got := h.fastPathReason("查询", &session{History: []Message{{Role: "user"}}}, v); got != "eligible" {
			t.Errorf("allowed read %s: %s", intent, got)
		}
	}
}
