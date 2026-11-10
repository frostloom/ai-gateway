package agent

import "fmt"

// RoutingMode separates the primary routing experiment from historical safety
// and hint experiments. A zero-value Handler retains legacy test behavior;
// NewHandler and the executable explicitly choose jev_selective.
type RoutingMode string

const (
	RoutingModel        RoutingMode = "model"
	RoutingJevSelective RoutingMode = "jev_selective"
	RoutingJevHint      RoutingMode = "jev_hint"
	RoutingLegacy       RoutingMode = "legacy"
)

func (h *Handler) RoutingMode() RoutingMode {
	if h.routingMode == "" {
		return RoutingLegacy
	}
	return h.routingMode
}

func (h *Handler) SetRoutingMode(mode string) error {
	m := RoutingMode(mode)
	switch m {
	case RoutingModel, RoutingJevSelective, RoutingJevHint, RoutingLegacy:
	default:
		return fmt.Errorf("AGENT_ROUTING_MODE must be model, jev_selective, jev_hint, or legacy")
	}
	if m == RoutingJevSelective && h.jevFastPathMin != 0 && h.jevFastPathMin < .90 {
		return fmt.Errorf("jev_selective requires JEV_FAST_PATH_MIN at least 0.90")
	}
	h.routingMode = m
	if m == RoutingJevSelective {
		h.jevFastPathConfigured = true
		h.jevFastPathEnabled = true
		if h.jevFastPathMin == 0 {
			h.jevFastPathMin = .90
		}
	}
	return nil
}

func (h *Handler) usesJevTurn() bool       { return h.RoutingMode() != RoutingModel }
func (h *Handler) usesJevWriteGuard() bool { return h.RoutingMode() == RoutingLegacy }
func (h *Handler) usesJevFastPath() bool {
	return h.RoutingMode() == RoutingJevSelective || (h.RoutingMode() == RoutingLegacy && h.jevFastPathEnabled)
}

func (h *Handler) decideRoutingVerdict(v *jevVerdict) {
	switch h.RoutingMode() {
	case RoutingLegacy:
		h.decideVerdict(v)
	case RoutingJevHint:
		min := h.jevIntentMin
		if min <= 0 {
			min = .75
		}
		if v.Intent != "" && v.Intent != "chitchat" && boundedProbability(v.IntentConf) && v.IntentConf >= min {
			v.Hint = fmt.Sprintf("【意图预判（置信度 %.2f）】用户意图很可能是「%s」。参数齐全且用户明确要求办理时可调用对应工具；缺参数先澄清，不确信可忽略本条。", v.IntentConf, v.Intent)
		}
	}
}
