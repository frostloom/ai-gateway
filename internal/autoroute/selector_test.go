// autoroute 选档决策测试：阈值边界（恰等 1.5）、on/shadow 两模式的 Served/WouldBe/
// Applied、score 越界与缺答案的 decode 兜底、错误 Kind 映射、熔断开路不再调用评估器、
// 成功清零熔断计数、unparseable 兜底且不触碰熔断器、TimeoutMS 真实生效、已取消
// context 走 jev_error 不 panic。成功答案用 jev.NewFake 脚本化；错误脚本、调用计数、
// 问题/state 捕获与延迟模拟用测试桩 stubEval。
package autoroute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/jev"
)

// selectorNow 以 Unix 纪元为基线的确定性时钟：now = step 秒，与熔断冷却 30s 配合。
func selectorNow(step int) time.Time {
	return time.Unix(int64(step), 0).UTC()
}

// onConfig on 模式测试配置：阈值 1.5、兜底便宜档、熔断 3 次/冷却 30s。
func onConfig() Config {
	return Config{
		Mode:             ModeOn,
		CheapTier:        "cheap-model",
		StrongTier:       "strong-model",
		Threshold:        1.5,
		FailMode:         "cheap",
		ShadowServe:      "cheap-model",
		TimeoutMS:        100,
		MaxInputBytes:    8192,
		BreakerFailures:  3,
		BreakerCooldownS: 30,
	}
}

// chatBody 最小可解析的 chat 请求体（ExtractState 输入）。
var chatBody = []byte(`{"model":"auto","messages":[{"role":"user","content":"帮我查一下订单"}]}`)

// stubEntry 单条脚本：err 非 nil 时返回该错误，否则返回 complexity score 答案。
type stubEntry struct {
	score float64
	err   error
}

// stubEval 测试评估器桩：补充 jev.NewFake 不支持的能力——脚本化错误、调用计数、
// 问题/state 捕获与延迟模拟。取消检查与生产 Client 一致（先于消费脚本）。
type stubEval struct {
	script    []stubEntry
	delay     time.Duration
	calls     int
	lastQ     map[string]jev.Question
	lastState any
}

func (s *stubEval) Evaluate(ctx context.Context, state any, questions map[string]jev.Question) (*jev.Result, error) {
	s.calls++
	s.lastQ = questions
	s.lastState = state
	if err := ctx.Err(); err != nil {
		kind := "canceled"
		if errors.Is(err, context.DeadlineExceeded) {
			kind = "timeout"
		}
		return nil, jev.NewFailure(kind, 0, false)
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
		if err := ctx.Err(); err != nil {
			return nil, jev.NewFailure("timeout", 0, false)
		}
	}
	if len(s.script) == 0 {
		return nil, errors.New("stubEval 脚本耗尽")
	}
	e := s.script[0]
	s.script = s.script[1:]
	if e.err != nil {
		return nil, e.err
	}
	return &jev.Result{Answers: map[string]jev.Answer{
		complexityQuestionName: jev.AnswerScore(e.score, 0.9, nil),
	}}, nil
}

func TestDecideThresholdBoundary(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0, "cheap-model"},
		{1.49, "cheap-model"},
		{1.5, "strong-model"}, // 恰等阈值选强档
		{3, "strong-model"},
	}
	for _, tc := range cases {
		eval := jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
			complexityQuestionName: jev.AnswerScore(tc.score, 0.9, []string{"简单", "一般", "复杂", "极复杂"}),
		}})
		d := NewSelector(onConfig(), eval).Decide(context.Background(), selectorNow(0), chatBody)
		if d.Fallback != "" {
			t.Fatalf("score %v：成功判定 Fallback 应为空，得到 %q", tc.score, d.Fallback)
		}
		if !d.Judged {
			t.Fatalf("score %v：应 Judged=true", tc.score)
		}
		if d.Score != tc.score {
			t.Fatalf("score %v：Decision.Score 应透传，得到 %v", tc.score, d.Score)
		}
		if d.WouldBeModel != tc.want {
			t.Fatalf("score %v：WouldBeModel 应为 %q，得到 %q", tc.score, tc.want, d.WouldBeModel)
		}
	}
}

func TestDecideOnModeAppliesJudgedTier(t *testing.T) {
	probabilities := map[string]float64{"简单": 0.1, "一般": 0.2, "复杂": 0.6, "极复杂": 0.1}
	eval := jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
		complexityQuestionName: {
			Type:          string(jev.QScore),
			Score:         2,
			Confidence:    0.9,
			Probabilities: probabilities,
		},
	}})
	d := NewSelector(onConfig(), eval).Decide(context.Background(), selectorNow(0), chatBody)
	if d.Fallback != "" || !d.Judged || d.Score != 2 {
		t.Fatalf("成功判定字段不符：Fallback=%q Judged=%v Score=%v", d.Fallback, d.Judged, d.Score)
	}
	if !d.Applied {
		t.Fatal("on 模式应 Applied=true")
	}
	if d.ServedModel != "strong-model" || d.WouldBeModel != "strong-model" {
		t.Fatalf("on 模式 Served/WouldBe 应为强档，得到 %q/%q", d.ServedModel, d.WouldBeModel)
	}
	if len(d.Probabilities) != len(probabilities) {
		t.Fatalf("Probabilities 应透传 %d 项，得到 %d 项", len(probabilities), len(d.Probabilities))
	}
	for name, p := range probabilities {
		if d.Probabilities[name] != p {
			t.Fatalf("Probabilities[%q] 应为 %v，得到 %v", name, p, d.Probabilities[name])
		}
	}
	if d.LatencyMS < 0 {
		t.Fatalf("LatencyMS 应非负，得到 %v", d.LatencyMS)
	}
}

func TestDecideShadowModeRecordsWithoutApply(t *testing.T) {
	cfg := onConfig()
	cfg.Mode = ModeShadow
	cfg.ShadowServe = "cheap-model"
	eval := jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
		complexityQuestionName: jev.AnswerScore(2, 0.9, nil),
	}})
	d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(0), chatBody)
	if d.Applied {
		t.Fatal("shadow 模式应 Applied=false")
	}
	if d.ServedModel != cfg.ShadowServe {
		t.Fatalf("shadow Served 应为 ShadowServe %q，得到 %q", cfg.ShadowServe, d.ServedModel)
	}
	if d.WouldBeModel != "strong-model" {
		t.Fatalf("shadow WouldBe 应为判定档 strong-model，得到 %q", d.WouldBeModel)
	}
	if !d.Judged || d.Score != 2 || d.Fallback != "" {
		t.Fatalf("shadow 判定仍应成功：Judged=%v Score=%v Fallback=%q", d.Judged, d.Score, d.Fallback)
	}
}

func TestDecideShadowFallbackServesShadowServe(t *testing.T) {
	cfg := onConfig()
	cfg.Mode = ModeShadow
	eval := &stubEval{script: []stubEntry{{err: jev.NewFailure("transport", 0, false)}}}
	d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(0), chatBody)
	if d.Fallback != "jev_error" || d.Judged {
		t.Fatalf("shadow 失败判定：Fallback=%q Judged=%v", d.Fallback, d.Judged)
	}
	if d.ServedModel != cfg.ShadowServe {
		t.Fatalf("shadow 兜底时实际服务仍应为固定档 %q，得到 %q", cfg.ShadowServe, d.ServedModel)
	}
	if d.WouldBeModel != cfg.FallbackTier() {
		t.Fatalf("shadow 兜底 WouldBe 应为兜底档 %q，得到 %q", cfg.FallbackTier(), d.WouldBeModel)
	}
	if d.Applied {
		t.Fatal("shadow 兜底应 Applied=false")
	}
}

func TestDecideScoreOutOfRangeFallsBackDecode(t *testing.T) {
	cfg := onConfig()
	for _, score := range []float64{-0.5, 3.5} {
		eval := jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
			complexityQuestionName: jev.AnswerScore(score, 0.9, nil),
		}})
		d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(0), chatBody)
		if d.Fallback != "decode" {
			t.Fatalf("score %v 越界：Fallback 应为 decode，得到 %q", score, d.Fallback)
		}
		if d.Judged {
			t.Fatalf("score %v 越界：不应 Judged", score)
		}
		if d.ServedModel != cfg.FallbackTier() {
			t.Fatalf("score %v 越界：Served 应为兜底档 %q，得到 %q", score, cfg.FallbackTier(), d.ServedModel)
		}
		if d.Score != 0 {
			t.Fatalf("score %v 越界：Decision.Score 不应透传非法值，得到 %v", score, d.Score)
		}
	}
}

func TestDecideMissingComplexityAnswerFallsBackDecode(t *testing.T) {
	cfg := onConfig()
	eval := jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
		"mood": jev.AnswerScore(2, 0.9, nil), // 响应缺 complexity
	}})
	d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(0), chatBody)
	if d.Fallback != "decode" || d.Judged {
		t.Fatalf("缺 complexity 答案：Fallback=%q Judged=%v", d.Fallback, d.Judged)
	}
	if d.ServedModel != cfg.FallbackTier() {
		t.Fatalf("Served 应为兜底档 %q，得到 %q", cfg.FallbackTier(), d.ServedModel)
	}
}

func TestDecideFallbackTierHonorsFailModeStrong(t *testing.T) {
	cfg := onConfig()
	cfg.FailMode = "strong"
	eval := jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
		complexityQuestionName: jev.AnswerScore(-1, 0.9, nil),
	}})
	d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(0), chatBody)
	if d.Fallback != "decode" {
		t.Fatalf("Fallback 应为 decode，得到 %q", d.Fallback)
	}
	if d.ServedModel != cfg.StrongTier {
		t.Fatalf("FailMode=strong 兜底档应为强档 %q，得到 %q", cfg.StrongTier, d.ServedModel)
	}
}

func TestDecideErrorKindMapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{jev.NewFailure("timeout", 0, false), "timeout"},
		{jev.NewFailure("decode", 200, true), "decode"},
		{jev.NewFailure("response_read", 200, true), "decode"},
		{jev.NewFailure("http", 503, true), "jev_error"},
		{jev.NewFailure("transport", 0, false), "jev_error"},
		{jev.NewFailure("canceled", 0, false), "jev_error"},
		{errors.New("非 jev 错误"), "jev_error"},
	}
	cfg := onConfig()
	for i, tc := range cases {
		eval := &stubEval{script: []stubEntry{{err: tc.err}}}
		d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(i), chatBody)
		kind := jev.FailureDetails(tc.err).Kind
		if d.Fallback != tc.want {
			t.Fatalf("Kind=%q：Fallback 应为 %q，得到 %q", kind, tc.want, d.Fallback)
		}
		if d.Judged {
			t.Fatalf("Kind=%q：不应 Judged", kind)
		}
		if d.ServedModel != cfg.FallbackTier() {
			t.Fatalf("Kind=%q：Served 应为兜底档 %q，得到 %q", kind, cfg.FallbackTier(), d.ServedModel)
		}
	}
}

func TestDecideBreakerOpenSkipsEvaluate(t *testing.T) {
	eval := &stubEval{script: []stubEntry{
		{err: jev.NewFailure("timeout", 0, false)},
		{err: jev.NewFailure("timeout", 0, false)},
		{err: jev.NewFailure("timeout", 0, false)},
		{score: 2}, // 若熔断失效，这条会被消费
	}}
	cfg := onConfig()
	s := NewSelector(cfg, eval)
	for step := 0; step < 3; step++ {
		d := s.Decide(context.Background(), selectorNow(step), chatBody)
		if d.Fallback != "timeout" {
			t.Fatalf("第 %d 次：Fallback 应为 timeout，得到 %q", step+1, d.Fallback)
		}
	}
	if got := s.BreakerState(); got != "open" {
		t.Fatalf("3 连败后熔断应 open，得到 %q", got)
	}
	d := s.Decide(context.Background(), selectorNow(3), chatBody) // now=3 < opensAt=32s
	if d.Fallback != "breaker_open" {
		t.Fatalf("熔断打开时 Fallback 应为 breaker_open，得到 %q", d.Fallback)
	}
	if eval.calls != 3 {
		t.Fatalf("熔断打开后不应再调 Evaluate：调用计数应停在 3，得到 %d", eval.calls)
	}
	if d.Judged || d.ServedModel != cfg.FallbackTier() {
		t.Fatalf("breaker_open 兜底：Judged=%v Served=%q", d.Judged, d.ServedModel)
	}
}

func TestDecideRecordSuccessResetsBreakerCount(t *testing.T) {
	// 2 败 → 1 成功（清零）→ 2 败：若成功未回写熔断器，第 4 次会被开路拒掉。
	eval := &stubEval{script: []stubEntry{
		{err: errors.New("e1")}, {err: errors.New("e2")}, {score: 1}, {err: errors.New("e4")},
	}}
	s := NewSelector(onConfig(), eval)
	for step := 0; step < 4; step++ {
		d := s.Decide(context.Background(), selectorNow(step), chatBody)
		if d.Fallback == "breaker_open" {
			t.Fatalf("第 %d 次不应被熔断开路（成功应清零连续失败计数）", step+1)
		}
	}
	if eval.calls != 4 {
		t.Fatalf("4 次判定都应放行：调用计数 %d", eval.calls)
	}
	if got := s.BreakerState(); got != "closed" {
		t.Fatalf("末次失败未达阈值应保持 closed，得到 %q", got)
	}
}

func TestDecideUnparseableBody(t *testing.T) {
	eval := &stubEval{script: []stubEntry{{score: 2}}}
	cfg := onConfig()
	s := NewSelector(cfg, eval)
	for _, body := range [][]byte{
		[]byte("{}"),
		[]byte(`{"model":"auto"}`), // 无 messages/input
		[]byte("not json"),
	} {
		d := s.Decide(context.Background(), selectorNow(0), body)
		if d.Fallback != "unparseable" {
			t.Fatalf("body %s：Fallback 应为 unparseable，得到 %q", body, d.Fallback)
		}
		if d.Judged {
			t.Fatalf("body %s：不应 Judged", body)
		}
		if d.ServedModel != cfg.FallbackTier() {
			t.Fatalf("body %s：Served 应为兜底档 %q，得到 %q", body, cfg.FallbackTier(), d.ServedModel)
		}
	}
	if eval.calls != 0 {
		t.Fatalf("unparseable 不应调用 Evaluate：调用计数 %d", eval.calls)
	}
	// unparseable 不回写熔断器：多次坏 body 后正常判定仍放行。
	d := s.Decide(context.Background(), selectorNow(0), chatBody)
	if d.Fallback != "" || !d.Judged {
		t.Fatalf("坏 body 不应污染熔断器：Fallback=%q Judged=%v", d.Fallback, d.Judged)
	}
	if eval.calls != 1 {
		t.Fatalf("正常判定应恰好调用 1 次 Evaluate：调用计数 %d", eval.calls)
	}
}

func TestDecideCanceledContextReturnsJEVError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	eval := &stubEval{script: []stubEntry{{score: 2}}} // 不应被消费
	cfg := onConfig()
	d := NewSelector(cfg, eval).Decide(ctx, selectorNow(0), chatBody)
	if d.Fallback != "jev_error" {
		t.Fatalf("已取消 ctx 应走 jev_error 兜底，得到 %q", d.Fallback)
	}
	if d.Judged {
		t.Fatal("已取消 ctx 不应 Judged")
	}
	if !d.Applied || d.ServedModel != cfg.FallbackTier() {
		t.Fatalf("on 模式兜底应 Applied=true 且按兜底档服务：Applied=%v Served=%q", d.Applied, d.ServedModel)
	}
	if eval.calls != 1 {
		t.Fatalf("取消检查应发生在消费脚本前：调用计数 %d", eval.calls)
	}
	// 本测试正常结束即证明该路径不 panic（Go 测试框架捕获 panic 判失败）。
}

func TestDecideAppliesTimeoutMS(t *testing.T) {
	cfg := onConfig()
	cfg.TimeoutMS = 1
	eval := &stubEval{script: []stubEntry{{score: 2}}, delay: 20 * time.Millisecond} // 超出 1ms 预算
	d := NewSelector(cfg, eval).Decide(context.Background(), selectorNow(0), chatBody)
	if d.Fallback != "timeout" {
		t.Fatalf("超出 TimeoutMS 预算应映射为 timeout，得到 %q", d.Fallback)
	}
	if d.Judged {
		t.Fatal("超时不应 Judged")
	}
}

func TestDecideEvaluatesComplexityQuestionOnExtractedState(t *testing.T) {
	eval := &stubEval{script: []stubEntry{{score: 1}}}
	NewSelector(onConfig(), eval).Decide(context.Background(), selectorNow(0), chatBody)
	if eval.calls != 1 {
		t.Fatalf("应恰好一次判定调用：调用计数 %d", eval.calls)
	}
	if len(eval.lastQ) != 1 {
		t.Fatalf("应恰好一个问题，得到 %d 个", len(eval.lastQ))
	}
	q, has := eval.lastQ[complexityQuestionName]
	if !has {
		t.Fatalf("应询问 %q 问题，实际询问 %v", complexityQuestionName, eval.lastQ)
	}
	if q.Type != jev.QScore {
		t.Fatalf("问题类型应为 score，得到 %q", q.Type)
	}
	if q.Instructions != "判断这轮对话任务对客服模型的复杂程度" {
		t.Fatalf("Instructions 不符：%q", q.Instructions)
	}
	criteria, isList := q.Criteria.([]string)
	if !isList || len(criteria) != 4 || criteria[0] != "简单" || criteria[1] != "一般" || criteria[2] != "复杂" || criteria[3] != "极复杂" {
		t.Fatalf("Criteria 应为四档有序量表，得到 %v", q.Criteria)
	}
	state, ok := eval.lastState.([]map[string]string)
	if !ok || len(state) != 1 || state[0]["role"] != "user" || state[0]["text"] != "帮我查一下订单" {
		t.Fatalf("state 应为 ExtractState 的提取结果，得到 %#v", eval.lastState)
	}
}
