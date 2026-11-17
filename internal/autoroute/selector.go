// autoroute 选档决策器：Decide 按「熔断放行 → ExtractState → 带超时 JEV 判定 →
// score 校验 → 阈值比较」产出 Decision，成功与失败都回写熔断器。复杂度问题固定为
// score 型 complexity（期望分 0–3，score ≥ 阈值选强档）；shadow 模式实际服务恒为
// ShadowServe、判定档只进 WouldBeModel，on 模式按判定档（或兜底档）改写。
package autoroute

import (
	"context"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// complexityQuestionName 复杂度问题的固定问题名。
const complexityQuestionName = "complexity"

// maxScore 复杂度期望分的合法上限（Criteria 四档对应 0–3）。
const maxScore = 3

// complexityQuestion 复杂度问题：score 型四档有序量表，取期望分（0=简单 … 3=极复杂）。
var complexityQuestion = jev.Question{
	Type:         jev.QScore,
	Instructions: "判断这轮对话任务对客服模型的复杂程度",
	Criteria:     []string{"简单", "一般", "复杂", "极复杂"},
}

// Decision 一次选档决策结果。ServedModel 是实际服务模型（shadow 恒为 ShadowServe，
// on 为判定档或兜底档）；WouldBeModel 是路由判定档（判定成功为强弱档，失败为兜底档）；
// Applied 表示是否按判定改写请求（仅 on 为 true）；Judged 表示 JEV 判定成功；
// Fallback 为兜底原因码，判定成功时为空串。
type Decision struct {
	ServedModel   string
	WouldBeModel  string
	Applied       bool
	Judged        bool
	Score         float64
	Probabilities map[string]float64
	LatencyMS     float64
	Fallback      string
}

// Selector auto 选档决策器：持配置、JEV 评估器与每实例熔断器。
type Selector struct {
	cfg     Config
	eval    jev.Evaluator
	breaker *Breaker
}

// NewSelector 构造决策器：熔断参数取配置的 BreakerFailures/BreakerCooldownS。
func NewSelector(cfg Config, eval jev.Evaluator) *Selector {
	return &Selector{
		cfg:     cfg,
		eval:    eval,
		breaker: NewBreaker(cfg.BreakerFailures, time.Duration(cfg.BreakerCooldownS)*time.Second),
	}
}

// Decide 对单个请求做出选档决策（顺序：熔断放行 → state 提取 → 带超时判定 →
// score 校验 → 阈值）。now 只驱动熔断器状态机，延迟用墙钟实测；任何路径都不返回
// error、不 panic——判定不可用时按 Fallback 原因码兜底。评估错误、答案缺失或 score
// 越界都回写熔断器失败；unparseable 与熔断开路不涉及 JEV，不回写。
func (s *Selector) Decide(ctx context.Context, now time.Time, body []byte) Decision {
	start := time.Now()
	if !s.breaker.Allow(now) {
		return s.fallbackDecision("breaker_open", elapsedMS(start))
	}
	state, ok := ExtractState(body, s.cfg.MaxInputBytes)
	if !ok {
		return s.fallbackDecision("unparseable", elapsedMS(start))
	}
	evalCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutMS)*time.Millisecond)
	defer cancel()

	result, err := s.eval.Evaluate(evalCtx, state, map[string]jev.Question{
		complexityQuestionName: complexityQuestion,
	})
	latency := elapsedMS(start)
	if err != nil {
		s.breaker.RecordFailure(now)
		return s.fallbackDecision(fallbackReason(err), latency)
	}
	answer, has := result.Answers[complexityQuestionName]
	if !has || answer.Score < 0 || answer.Score > maxScore {
		s.breaker.RecordFailure(now)
		return s.fallbackDecision("decode", latency)
	}
	s.breaker.RecordSuccess(now)

	tier := s.cfg.CheapTier
	if answer.Score >= s.cfg.Threshold {
		tier = s.cfg.StrongTier
	}
	served := tier
	if s.cfg.Mode == ModeShadow {
		served = s.cfg.ShadowServe // shadow 只记录判定，实际服务固定档
	}
	return Decision{
		ServedModel:   served,
		WouldBeModel:  tier,
		Applied:       s.cfg.Mode == ModeOn,
		Judged:        true,
		Score:         answer.Score,
		Probabilities: answer.Probabilities,
		LatencyMS:     latency,
	}
}

// BreakerState 返回熔断器当前相位（"closed" | "open" | "half_open"），供观测。
func (s *Selector) BreakerState() string {
	return s.breaker.State()
}

// fallbackDecision 构造兜底决策：判定未成功，路由判定档为兜底档；shadow 模式
// 实际服务仍为固定档，on 模式按兜底档改写。
func (s *Selector) fallbackDecision(reason string, latencyMS float64) Decision {
	fallback := s.cfg.FallbackTier()
	served := fallback
	if s.cfg.Mode == ModeShadow {
		served = s.cfg.ShadowServe
	}
	return Decision{
		ServedModel:  served,
		WouldBeModel: fallback,
		Applied:      s.cfg.Mode == ModeOn,
		Judged:       false,
		LatencyMS:    latencyMS,
		Fallback:     reason,
	}
}

// fallbackReason 把 JEV 评估错误映射为兜底原因码：timeout 单列；decode 与
// response_read 归为 decode；其余（http/transport/canceled 及非 jev 错误）归 jev_error。
func fallbackReason(err error) string {
	switch jev.FailureDetails(err).Kind {
	case "timeout":
		return "timeout"
	case "decode", "response_read":
		return "decode"
	default:
		return "jev_error"
	}
}

// elapsedMS 自 start 起的耗时毫秒数。
func elapsedMS(start time.Time) float64 {
	return time.Since(start).Seconds() * 1000
}
