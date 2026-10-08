package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// Configure a shadow decision in both modes so hint/fast experiments submit
// identical JEV questions. Disabling changes execution, not the judge inputs.
func (h *Handler) SetJevFastPath(enabled bool, minConfidence float64) error {
	if math.IsNaN(minConfidence) || math.IsInf(minConfidence, 0) || minConfidence < .8 || minConfidence > 1 {
		return fmt.Errorf("JEV_FAST_PATH_MIN must be finite and between 0.80 and 1.00")
	}
	h.jevFastPathConfigured = true
	h.jevFastPathEnabled = enabled
	h.jevFastPathMin = minConfidence
	return nil
}

func boundedProbability(p float64) bool {
	return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= 0 && p <= 1
}

func (h *Handler) fastPathReason(userMsg string, ses *session, v jevVerdict) string {
	if h.jev == nil || !v.Available {
		return "judge_unavailable"
	}
	if !v.FastFieldsValid {
		return "judge_schema_invalid"
	}
	if v.Block || v.Warn || !boundedProbability(v.LegitProb) || v.LegitProb < .85 || !boundedProbability(v.RiskConf) || v.RiskConf < .8 || math.IsNaN(v.RiskScore) || math.IsInf(v.RiskScore, 0) || v.RiskScore < 0 || v.RiskScore >= .5 {
		return "safety_not_clear"
	}
	if !boundedProbability(v.IntentConf) || v.IntentConf < h.jevFastPathMin {
		return "intent_uncertain"
	}
	if !v.SingleRead || !boundedProbability(v.ShapeConf) || v.ShapeConf < h.jevFastPathMin {
		return "not_single_parameterless_read"
	}
	if ses == nil || ses.PendingAction != nil || len(ses.History) != 1 {
		return "conversation_context"
	}
	switch v.Intent {
	case "get_balance", "get_consumption", "list_plans", "get_my_subscription":
	default:
		return "tool_not_supported"
	}
	text := strings.ToLower(userMsg)
	// A conservative execution-level exclusion supplements the judge. It does
	// not classify positive intent or pick a tool and may only force fallback.
	for _, term := range []string{"充", "退款", "退订", "改套餐", "换套餐", "订购", "下单", "购买", "开通", "转账", "然后", "再帮", "并且", "同时", "顺便", "以及", "不要", "不用", "别查", "不查", "recharge", "refund", "cancel", "subscribe", "switch", "buy", "pay", " and ", "don't", "do not"} {
		if strings.Contains(text, term) {
			return "compound_or_action_or_negation"
		}
	}
	if customReadScope(v.Intent, text) {
		return "parameters_or_custom_scope"
	}
	if h.tb == nil {
		return "tool_not_supported"
	}
	tool, exists := h.tb.tools[v.Intent]
	if !exists || tool == nil || tool.confirm || tool.run == nil {
		return "tool_not_supported"
	}
	return "eligible"
}

// Direct execution requires typed, explicitly supplied decision fields. The
// HTTP client always supplies Raw; typed in-process evaluators are also checked.
func validFastAnswers(r *jev.Result) bool {
	if r == nil {
		return false
	}
	fields := map[string][]string{"intent": {"choice", "confidence"}, "route_shape": {"choice", "confidence"}, "legit": {"noul"}, "risk": {"score", "confidence"}}
	for name := range fields {
		a, ok := r.Answers[name]
		want := "choice"
		if name == "legit" {
			want = "noul"
		}
		if name == "risk" {
			want = "score"
		}
		if !ok || a.Type != want {
			return false
		}
		if want != "noul" && (a.Noul != 0 || !boundedProbability(a.Confidence) || a.Confidence == 0) {
			return false
		}
	}
	if r.Raw == "" {
		return true
	}
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(r.Raw), &root) != nil {
		return false
	}
	answers := root
	if raw, ok := root["answers"]; ok {
		if json.Unmarshal(raw, &answers) != nil {
			return false
		}
	}
	for name, required := range fields {
		var a map[string]json.RawMessage
		if json.Unmarshal(answers[name], &a) != nil {
			return false
		}
		for _, field := range required {
			raw, ok := a[field]
			if !ok || string(raw) == "null" {
				return false
			}
			if field == "choice" {
				var value string
				if json.Unmarshal(raw, &value) != nil || value == "" {
					return false
				}
			} else {
				var value float64
				if json.Unmarshal(raw, &value) != nil {
					return false
				}
			}
		}
	}
	return true
}

func (h *Handler) tryJevFastPath(ctx context.Context, sid string, tenant uint64, msg string, ses *session, v jevVerdict) (string, []traceItem, bool, error) {
	stats := turnTelemetry(ctx)
	reason := h.fastPathReason(msg, ses, v)
	if stats != nil {
		stats.JevIntent = v.Intent
		stats.JevIntentConfidence = v.IntentConf
		stats.JevFastPathEligible = reason == "eligible"
		stats.JevFastPathReason = reason
	}
	if !h.jevFastPathEnabled {
		if stats != nil && reason == "eligible" {
			stats.JevFastPathReason = "shadow_only"
		}
		return "", nil, false, nil
	}
	if reason != "eligible" {
		return "", nil, false, nil
	}
	args := map[string]any{}
	if stats != nil {
		stats.JevFastPath = true
		stats.JevFastPathTool = v.Intent
		stats.JevFastPathReason = "direct_read"
	}
	result, err := h.runTimedTool(ctx, v.Intent, args, tenant, "")
	outcome := result
	if err != nil {
		outcome = "查询失败：" + err.Error()
		if stats != nil {
			stats.JevFastPathReason = "tool_error"
		}
	}
	h.audit(sid, tenant, v.Intent, args, false, "", outcome, guardJSON("jev", "fast_read", "", map[string]any{"intent_confidence": v.IntentConf, "shape_confidence": v.ShapeConf, "threshold": h.jevFastPathMin}))
	trace := []traceItem{{Tool: v.Intent, Args: "{}", Result: outcome}}
	if err != nil {
		return "", trace, true, err
	}
	reply := sanitizeReply(result)
	ses.History = append(ses.History, Message{Role: "assistant", Content: reply})
	jevDecisions.WithLabelValues("intent", "fast_read").Inc()
	return reply, trace, true, nil
}

var readDays = regexp.MustCompile(`([0-9]+|[一二三四五六七八九十两]+)\s*(?:天|日|days?)`)

func customReadScope(tool, text string) bool {
	if tool == "get_consumption" {
		for _, term := range []string{"今天", "昨天", "前天", "本周", "上周", "本月", "上月", "今年", "去年", "today", "yesterday", "month", "year", "从", "到"} {
			if strings.Contains(text, term) {
				return true
			}
		}
		for _, m := range readDays.FindAllStringSubmatch(text, -1) {
			if m[1] != "7" && m[1] != "七" {
				return true
			}
		}
		return false
	}
	for _, r := range text {
		if unicode.IsDigit(r) {
			return true
		}
	}
	if tool == "list_plans" {
		for _, term := range []string{"月度", "旗舰", "标准", "限量", "比较", "推荐", "最便宜", "适合", "筛选", "￥", "¥"} {
			if strings.Contains(text, term) {
				return true
			}
		}
	}
	return false
}

func (h *Handler) runTimedTool(ctx context.Context, name string, args map[string]any, tenant uint64, idem string) (string, error) {
	start := time.Now()
	defer func() {
		if stats := turnTelemetry(ctx); stats != nil {
			stats.ToolLatencyMS += float64(time.Since(start)) / float64(time.Millisecond)
		}
	}()
	return h.tb.Run(ctx, name, args, tenant, idem)
}

func (h *Handler) previewTimedTool(ctx context.Context, name string, args map[string]any, tenant uint64) (string, error) {
	start := time.Now()
	defer func() {
		if stats := turnTelemetry(ctx); stats != nil {
			stats.ToolLatencyMS += float64(time.Since(start)) / float64(time.Millisecond)
		}
	}()
	return h.tb.Preview(ctx, name, args, tenant)
}
