package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/autoroute"
	"github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/jev"
)

// autoRouteTestConfig 构造测试配置：两档 flash/pro、阈值 1.5、兜底便宜档。
func autoRouteTestConfig(mode autoroute.Mode) autoroute.Config {
	return autoroute.Config{
		Mode:             mode,
		CheapTier:        "test-flash",
		StrongTier:       "test-pro",
		Threshold:        1.5,
		FailMode:         "cheap",
		ShadowServe:      "test-flash",
		TimeoutMS:        100,
		MaxInputBytes:    8192,
		BreakerFailures:  3,
		BreakerCooldownS: 30,
	}
}

// scoredEval 构造固定返回 score 的假 JEV 评估器。
func scoredEval(score float64) jev.Evaluator {
	return jev.NewFake(jev.FakeScript{Answers: map[string]jev.Answer{
		"complexity": jev.AnswerScore(score, 0.9, []string{"简单", "一般", "复杂", "极复杂"}),
	}})
}

// autoRouteTestEnv 建 gin 引擎：Idempotency（生产两组都在 AutoRoute 之前生成 request_id）
// + 该中间件 + 桩下游。桩读全量 body 记录字节与 ContentLength 后回 200；
// downstream 闭包捕获供断言。
func autoRouteTestEnv(t *testing.T, mw gin.HandlerFunc) (r *gin.Engine, downstream func() ([]byte, int64, bool)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var got []byte
	var gotLen int64
	called := false
	r = gin.New()
	r.POST("/v1/chat/completions", Idempotency(), mw, func(c *gin.Context) {
		called = true
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Errorf("下游读 body 失败: %v", err)
		}
		got = b
		gotLen = c.Request.ContentLength
		c.Status(http.StatusOK)
	})
	return r, func() ([]byte, int64, bool) { return got, gotLen, called }
}

// postAutoBody 向引擎发一次 POST，body 原样、ContentLength 显式设置。
func postAutoBody(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// assertNoAutoHeaders 断言响应不含任何 X-Auto- 前缀头。
func assertNoAutoHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for k := range w.Header() {
		if strings.HasPrefix(k, "X-Auto-") {
			t.Fatalf("不应出现 X-Auto- 头，发现 %s: %s", k, w.Header().Get(k))
		}
	}
}

const autoRouteChatBody = `{"model":"auto","messages":[{"role":"user","content":"帮我查一下话费余额"}]}`

func TestAutoRouteNonAutoPassthrough(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"显式模型", `{"model":"test-flash","messages":[{"role":"user","content":"hi"}]}`},
		{"非JSON", `这不是JSON`},
		{"JSON缺model", `{"messages":[{"role":"user","content":"hi"}]}`},
		{"model非字符串", `{"model":123,"messages":[{"role":"user","content":"hi"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// mode=on + 可判定 eval：若误触发了 auto 路径，字节必然改变，逐字节断言即暴露
			mw := AutoRoute(autoRouteTestConfig(autoroute.ModeOn), autoroute.NewSelector(autoRouteTestConfig(autoroute.ModeOn), scoredEval(2)), nil, nil, nil)
			r, downstream := autoRouteTestEnv(t, mw)
			w := postAutoBody(t, r, tc.body)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			got, gotLen, _ := downstream()
			if !bytes.Equal(got, []byte(tc.body)) {
				t.Fatalf("下游收到的字节与发送不一致:\nsent: %q\ngot:  %q", tc.body, string(got))
			}
			if gotLen != int64(len(tc.body)) {
				t.Fatalf("ContentLength = %d, want %d（不应改变）", gotLen, len(tc.body))
			}
			assertNoAutoHeaders(t, w)
		})
	}
}

func TestAutoRouteOffRejectsAutoModel(t *testing.T) {
	// off 态 sel == nil 合法
	mw := AutoRoute(autoRouteTestConfig(autoroute.ModeOff), nil, nil, nil, nil)
	r, downstream := autoRouteTestEnv(t, mw)
	w := postAutoBody(t, r, autoRouteChatBody)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var errBody struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("错误体不是合法 JSON: %v\nbody: %s", err, w.Body.String())
	}
	want := "model not listed (auto routing disabled)"
	if errBody.Error.Message != want {
		t.Fatalf("message = %q, want %q", errBody.Error.Message, want)
	}
	if _, _, called := downstream(); called {
		t.Fatal("off 态 auto 请求不应触达下游")
	}
	assertNoAutoHeaders(t, w)
}

func TestAutoRouteOnRewritesToJudgedTier(t *testing.T) {
	cases := []struct {
		name      string
		score     float64
		wantModel string
	}{
		{"高score选强档", 2, "test-pro"},
		{"低score选便宜档", 1, "test-flash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := autoRouteTestConfig(autoroute.ModeOn)
			mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(tc.score)), nil, nil, nil)
			r, downstream := autoRouteTestEnv(t, mw)
			w := postAutoBody(t, r, autoRouteChatBody)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			got, gotLen, _ := downstream()
			var top map[string]json.RawMessage
			if err := json.Unmarshal(got, &top); err != nil {
				t.Fatalf("下游 body 不是合法 JSON: %v\nbody: %s", err, string(got))
			}
			var gotModel string
			if err := json.Unmarshal(top["model"], &gotModel); err != nil || gotModel != tc.wantModel {
				t.Fatalf("下游 model = %q (err=%v), want %q", gotModel, err, tc.wantModel)
			}
			if gotLen != int64(len(got)) {
				t.Fatalf("ContentLength = %d, want %d（需与新 body 同步）", gotLen, len(got))
			}
			if v := w.Header().Get("X-Auto-Model"); v != tc.wantModel {
				t.Fatalf("X-Auto-Model = %q, want %q", v, tc.wantModel)
			}
			if v := w.Header().Get("X-Auto-Applied"); v != "true" {
				t.Fatalf("X-Auto-Applied = %q, want true", v)
			}
			if v := w.Header().Get("X-Auto-Would-Be"); v != tc.wantModel {
				t.Fatalf("X-Auto-Would-Be = %q, want %q", v, tc.wantModel)
			}
			score, err := strconv.ParseFloat(w.Header().Get("X-Auto-Score"), 64)
			if err != nil || score != tc.score {
				t.Fatalf("X-Auto-Score = %q (err=%v), want %v", w.Header().Get("X-Auto-Score"), err, tc.score)
			}
			if _, err := strconv.ParseFloat(w.Header().Get("X-Auto-Decision-Ms"), 64); err != nil {
				t.Fatalf("X-Auto-Decision-Ms = %q 不可解析: %v", w.Header().Get("X-Auto-Decision-Ms"), err)
			}
			if v := w.Header().Get("X-Auto-Fallback"); v != "" {
				t.Fatalf("判定成功不应有 X-Auto-Fallback，got %q", v)
			}
		})
	}
}

func TestAutoRouteShadowServesFixedTier(t *testing.T) {
	cfg := autoRouteTestConfig(autoroute.ModeShadow)
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil, nil)
	r, downstream := autoRouteTestEnv(t, mw)
	w := postAutoBody(t, r, autoRouteChatBody)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got, gotLen, _ := downstream()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatalf("下游 body 不是合法 JSON: %v\nbody: %s", err, string(got))
	}
	var gotModel string
	if err := json.Unmarshal(top["model"], &gotModel); err != nil || gotModel != "test-flash" {
		t.Fatalf("shadow 下游 model = %q (err=%v), want ShadowServe test-flash", gotModel, err)
	}
	if gotLen != int64(len(got)) {
		t.Fatalf("ContentLength = %d, want %d", gotLen, len(got))
	}
	if v := w.Header().Get("X-Auto-Model"); v != "test-flash" {
		t.Fatalf("X-Auto-Model = %q, want %q", v, "test-flash")
	}
	if v := w.Header().Get("X-Auto-Applied"); v != "false" {
		t.Fatalf("X-Auto-Applied = %q, want false", v)
	}
	if v := w.Header().Get("X-Auto-Would-Be"); v != "test-pro" {
		t.Fatalf("X-Auto-Would-Be = %q, want 判定档 test-pro", v)
	}
}

func TestAutoRouteFallbackOnJEVFailure(t *testing.T) {
	t.Run("JEV评估失败兜底", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		// 空 script：Evaluate 即报错 → 非JEV错误归 jev_error 兜底
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, jev.NewFake()), nil, nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("兜底不应拒绝请求，status = %d", w.Code)
		}
		got, _, _ := downstream()
		var top map[string]json.RawMessage
		if err := json.Unmarshal(got, &top); err != nil {
			t.Fatalf("下游 body 不是合法 JSON: %v", err)
		}
		var gotModel string
		if err := json.Unmarshal(top["model"], &gotModel); err != nil || gotModel != cfg.FallbackTier() {
			t.Fatalf("兜底下游 model = %q (err=%v), want FallbackTier %q", gotModel, err, cfg.FallbackTier())
		}
		if v := w.Header().Get("X-Auto-Fallback"); v != "jev_error" {
			t.Fatalf("X-Auto-Fallback = %q, want jev_error", v)
		}
		if v := w.Header().Get("X-Auto-Applied"); v != "true" {
			t.Fatalf("on 模式兜底仍按判定改写，X-Auto-Applied = %q, want true", v)
		}
		// 未判定：无 Would-Be / Score
		if v := w.Header().Get("X-Auto-Would-Be"); v != "" {
			t.Fatalf("未判定不应有 X-Auto-Would-Be，got %q", v)
		}
		if v := w.Header().Get("X-Auto-Score"); v != "" {
			t.Fatalf("未判定不应有 X-Auto-Score，got %q", v)
		}
	})

	t.Run("body形状认不出unparseable兜底", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		// 有 model 但无 messages/input：Decide 内部 ExtractState 失败 → unparseable 兜底
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, `{"model":"auto"}`)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got, _, _ := downstream()
		var top map[string]json.RawMessage
		if err := json.Unmarshal(got, &top); err != nil {
			t.Fatalf("下游 body 不是合法 JSON: %v", err)
		}
		var gotModel string
		if err := json.Unmarshal(top["model"], &gotModel); err != nil || gotModel != cfg.FallbackTier() {
			t.Fatalf("unparseable 兜底下游 model = %q (err=%v), want %q", gotModel, err, cfg.FallbackTier())
		}
		if v := w.Header().Get("X-Auto-Fallback"); v != "unparseable" {
			t.Fatalf("X-Auto-Fallback = %q, want unparseable", v)
		}
	})
}

func TestAutoRouteStreamKeyPreserved(t *testing.T) {
	cfg := autoRouteTestConfig(autoroute.ModeOn)
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil, nil)
	r, downstream := autoRouteTestEnv(t, mw)
	// stream 值带原样空白与转义字符，改写后其余键字节语义保留
	body := "{\n  \"model\": \"auto\",\n  \"stream\" : true,\n  \"extra\": \"a<&>b\",\n  \"messages\": [{\"role\":\"user\",\"content\":\"帮我查话费\"}]\n}"
	w := postAutoBody(t, r, body)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got, _, _ := downstream()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatalf("下游 body 不是合法 JSON: %v\nbody: %s", err, string(got))
	}
	if !bytes.Equal(bytes.TrimSpace(top["stream"]), []byte("true")) {
		t.Fatalf("stream 键原字节被改变: %s", string(top["stream"]))
	}
	var gotModel string
	if err := json.Unmarshal(top["model"], &gotModel); err != nil || gotModel != "test-pro" {
		t.Fatalf("下游 model = %q (err=%v), want test-pro", gotModel, err)
	}
}

func TestAutoRouteLogsDecision(t *testing.T) {
	cfg := autoRouteTestConfig(autoroute.ModeOn)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, log, nil)
	r, _ := autoRouteTestEnv(t, mw)
	w := postAutoBody(t, r, autoRouteChatBody)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	out := buf.String()
	// request_id 来自 Idempotency（本环境已挂载），应非空且带 req_ 前缀；
	// scoredEval 不产 probabilities → nil 记空对象；threshold 来自配置。
	for _, key := range []string{
		"auto_route", `"mode":"on"`, `"model":"test-pro"`, `"would_be":"test-pro"`, "decision_ms",
		`"degraded":""`, `"breaker_state":"closed"`,
		`"request_id":"req_`, `"threshold":1.5`, `"probabilities":{}`,
	} {
		if !strings.Contains(out, key) {
			t.Fatalf("日志缺少 %s: %s", key, out)
		}
	}
}

// recordingProbe 计数探测桩：记录每次探测的模型名；fail 为 true 时返回错误模拟不可路由。
func recordingProbe(calls *[]string, fail bool) func(context.Context, string) error {
	return func(_ context.Context, model string) error {
		*calls = append(*calls, model)
		if fail {
			return errors.New("route unavailable")
		}
		return nil
	}
}

// assertAutoDegradedAbsence 断言响应无 X-Auto-Degraded 头。
func assertAutoDegradedAbsence(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if v := w.Header().Get("X-Auto-Degraded"); v != "" {
		t.Fatalf("不应有 X-Auto-Degraded 头，got %q", v)
	}
}

// downstreamModel 解析下游收到的 body 的 model 字段。
func downstreamModel(t *testing.T, got []byte) string {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatalf("下游 body 不是合法 JSON: %v\nbody: %s", err, string(got))
	}
	var model string
	if err := json.Unmarshal(top["model"], &model); err != nil {
		t.Fatalf("下游 model 非字符串: %v\nraw: %s", err, string(top["model"]))
	}
	return model
}

func TestAutoRouteDegradeStrongUnavailable(t *testing.T) {
	t.Run("probe失败且degrade开改写便宜档", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		cfg.DegradeStrongUnavailable = true
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("降级不应拒绝请求，status = %d", w.Code)
		}
		got, gotLen, _ := downstream()
		if m := downstreamModel(t, got); m != "test-flash" {
			t.Fatalf("降级下游 model = %q, want 便宜档 test-flash", m)
		}
		if gotLen != int64(len(got)) {
			t.Fatalf("ContentLength = %d, want %d（需与新 body 同步）", gotLen, len(got))
		}
		if v := w.Header().Get("X-Auto-Degraded"); v != "strong_unavailable" {
			t.Fatalf("X-Auto-Degraded = %q, want strong_unavailable", v)
		}
		if v := w.Header().Get("X-Auto-Model"); v != "test-flash" {
			t.Fatalf("X-Auto-Model = %q, want 更新为 test-flash", v)
		}
		if v := w.Header().Get("X-Auto-Would-Be"); v != "test-pro" {
			t.Fatalf("X-Auto-Would-Be = %q, want 保持判定档 test-pro", v)
		}
		if len(probed) != 1 || probed[0] != "test-pro" {
			t.Fatalf("probe 调用记录 = %v, want 恰一次且只探判定档 [test-pro]（降级后不二次探测）", probed)
		}
	})

	t.Run("probe失败且degrade关保持strong无头", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		cfg.DegradeStrongUnavailable = false
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got, _, _ := downstream()
		if m := downstreamModel(t, got); m != "test-pro" {
			t.Fatalf("degrade 关下游 model = %q, want 保持 test-pro（真实链路由 handler Route 503）", m)
		}
		assertAutoDegradedAbsence(t, w)
		if v := w.Header().Get("X-Auto-Model"); v != "test-pro" {
			t.Fatalf("X-Auto-Model = %q, want test-pro", v)
		}
		if len(probed) != 1 || probed[0] != "test-pro" {
			t.Fatalf("probe 调用记录 = %v, want 恰一次 [test-pro]", probed)
		}
	})
}

func TestAutoRouteCheapUnavailable(t *testing.T) {
	t.Run("upgrade关默认不改写无头", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		cfg.UpgradeCheapUnavailable = false
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(1)), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got, _, _ := downstream()
		if m := downstreamModel(t, got); m != "test-flash" {
			t.Fatalf("upgrade 关下游 model = %q, want 保持 test-flash", m)
		}
		assertAutoDegradedAbsence(t, w)
		if v := w.Header().Get("X-Auto-Model"); v != "test-flash" {
			t.Fatalf("X-Auto-Model = %q, want test-flash", v)
		}
		if len(probed) != 1 || probed[0] != "test-flash" {
			t.Fatalf("probe 调用记录 = %v, want 恰一次 [test-flash]", probed)
		}
	})

	t.Run("upgrade开改写strong", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		cfg.UpgradeCheapUnavailable = true
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(1)), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("升级不应拒绝请求，status = %d", w.Code)
		}
		got, gotLen, _ := downstream()
		if m := downstreamModel(t, got); m != "test-pro" {
			t.Fatalf("升级下游 model = %q, want 强档 test-pro", m)
		}
		if gotLen != int64(len(got)) {
			t.Fatalf("ContentLength = %d, want %d（需与新 body 同步）", gotLen, len(got))
		}
		if v := w.Header().Get("X-Auto-Degraded"); v != "cheap_unavailable" {
			t.Fatalf("X-Auto-Degraded = %q, want cheap_unavailable", v)
		}
		if v := w.Header().Get("X-Auto-Model"); v != "test-pro" {
			t.Fatalf("X-Auto-Model = %q, want 更新为 test-pro", v)
		}
		if v := w.Header().Get("X-Auto-Would-Be"); v != "test-flash" {
			t.Fatalf("X-Auto-Would-Be = %q, want 保持判定档 test-flash", v)
		}
		if len(probed) != 1 || probed[0] != "test-flash" {
			t.Fatalf("probe 调用记录 = %v, want 恰一次且只探判定档 [test-flash]（升级后不二次探测）", probed)
		}
	})
}

func TestAutoRouteProbeSuccessNoDegrade(t *testing.T) {
	cfg := autoRouteTestConfig(autoroute.ModeOn)
	var probed []string
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), recordingProbe(&probed, false), nil, nil)
	r, downstream := autoRouteTestEnv(t, mw)
	w := postAutoBody(t, r, autoRouteChatBody)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got, _, _ := downstream()
	if m := downstreamModel(t, got); m != "test-pro" {
		t.Fatalf("probe 成功下游 model = %q, want test-pro", m)
	}
	assertAutoDegradedAbsence(t, w)
	if len(probed) != 1 || probed[0] != "test-pro" {
		t.Fatalf("probe 调用记录 = %v, want 恰一次 [test-pro]", probed)
	}
}

func TestAutoRouteProbeOnlyOnJudgedOnPath(t *testing.T) {
	t.Run("非auto请求不探测", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		body := `{"model":"test-flash","messages":[{"role":"user","content":"hi"}]}`
		w := postAutoBody(t, r, body)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got, gotLen, _ := downstream()
		if !bytes.Equal(got, []byte(body)) || gotLen != int64(len(body)) {
			t.Fatalf("非 auto 请求应零变化透传: got %q len %d", string(got), gotLen)
		}
		if len(probed) != 0 {
			t.Fatalf("非 auto 请求不应探测，probe 调用 = %v", probed)
		}
	})

	t.Run("off模式auto请求不探测", func(t *testing.T) {
		var probed []string
		mw := AutoRoute(autoRouteTestConfig(autoroute.ModeOff), nil, recordingProbe(&probed, true), nil, nil)
		r, _ := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
		if len(probed) != 0 {
			t.Fatalf("off 模式不应探测，probe 调用 = %v", probed)
		}
	})

	t.Run("shadow模式不探测", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeShadow)
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got, _, _ := downstream()
		if m := downstreamModel(t, got); m != "test-flash" {
			t.Fatalf("shadow 下游 model = %q, want 固定档 test-flash", m)
		}
		assertAutoDegradedAbsence(t, w)
		if len(probed) != 0 {
			t.Fatalf("shadow 实际服务固定档不应探测，probe 调用 = %v", probed)
		}
	})

	t.Run("兜底判定不探测", func(t *testing.T) {
		cfg := autoRouteTestConfig(autoroute.ModeOn)
		var probed []string
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, jev.NewFake()), recordingProbe(&probed, true), nil, nil)
		r, downstream := autoRouteTestEnv(t, mw)
		w := postAutoBody(t, r, autoRouteChatBody)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		got, _, _ := downstream()
		if m := downstreamModel(t, got); m != cfg.FallbackTier() {
			t.Fatalf("兜底下游 model = %q, want %q", m, cfg.FallbackTier())
		}
		assertAutoDegradedAbsence(t, w)
		if len(probed) != 0 {
			t.Fatalf("未判定（兜底）不应探测，probe 调用 = %v", probed)
		}
	})
}

// TestNewAutoRouteProbe 缝隙测试：真实 client.Router 打到 httptest 的 /route，
// 验证提取后的探测函数把 router 的「无可用 provider」信号（HTTP 503 → (nil, nil)）
// 映射为 error。旧闭包直接丢弃结果只透传 err，该场景被误判为探测成功，
// strong→cheap 降级的主触发路径失效、请求带判定档进 handler 再 503。
func TestNewAutoRouteProbe(t *testing.T) {
	t.Run("503无可用provider映射为error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/route" {
				t.Errorf("探测应请求 /route, got %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		probe := NewAutoRouteProbe(client.NewRouter(srv.URL, ""))
		err := probe(context.Background(), "test-pro")
		if err == nil {
			t.Fatal("router 返回 (nil,nil)=无可用 provider 时探测必须报错，否则降级不触发")
		}
		if !strings.Contains(err.Error(), "no available provider") {
			t.Fatalf("错误应说明无可用 provider, got %q", err.Error())
		}
	})

	t.Run("200有provider返回nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"provider_id":7,"name":"pro","base_url":"https://up.example"}`))
		}))
		defer srv.Close()
		probe := NewAutoRouteProbe(client.NewRouter(srv.URL, ""))
		if err := probe(context.Background(), "test-pro"); err != nil {
			t.Fatalf("有可用 provider 时探测不应报错: %v", err)
		}
	})
}

type fakeAutoRecorder struct {
	calls  []map[string]string
	states []any
}

func (f *fakeAutoRecorder) RecordAutoRoute(_ context.Context, state any, fields map[string]string) {
	f.calls = append(f.calls, fields)
	f.states = append(f.states, state)
}

func TestAutoRouteRecordsDecisionToObserver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := &fakeAutoRecorder{}
	cfg := autoRouteTestConfig(autoroute.ModeOn)
	env := gin.New()
	env.Use(Idempotency())
	env.Use(AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil, rec))
	env.POST("/chat", func(c *gin.Context) { c.Status(200) })
	body := `{"model":"auto","messages":[{"role":"user","content":"帮我写一个 LRU 缓存"}]}`
	w := httptest.NewRecorder()
	env.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body)))
	if w.Code != 200 || len(rec.calls) != 1 {
		t.Fatalf("code=%d calls=%d", w.Code, len(rec.calls))
	}
	f := rec.calls[0]
	if f["served_model"] != cfg.StrongTier || f["tier"] != "strong" || f["mode"] != "on" ||
		f["request_id"] == "" || f["threshold"] == "" || f["breaker_state"] == "" {
		t.Fatalf("fields=%v", f)
	}
	if s := fmt.Sprint(rec.states[0]); !strings.Contains(s, "LRU") {
		t.Fatalf("state not exported: %v", rec.states[0])
	}
}

func TestAutoRouteDoesNotRecordNonAuto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := &fakeAutoRecorder{}
	cfg := autoRouteTestConfig(autoroute.ModeOn)
	env := gin.New()
	env.Use(AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil, rec))
	env.POST("/chat", func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	env.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/chat",
		strings.NewReader(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"你好"}]}`)))
	if w.Code != 200 || len(rec.calls) != 0 {
		t.Fatalf("code=%d calls=%d", w.Code, len(rec.calls))
	}
}
