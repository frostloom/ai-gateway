package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/frostloom/ai-gateway/internal/autoroute"
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

// autoRouteTestEnv 建 gin 引擎：该中间件 + 桩下游。桩读全量 body 记录字节与
// ContentLength 后回 200；downstream 闭包捕获供断言。
func autoRouteTestEnv(t *testing.T, mw gin.HandlerFunc) (r *gin.Engine, downstream func() ([]byte, int64, bool)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var got []byte
	var gotLen int64
	called := false
	r = gin.New()
	r.POST("/v1/chat/completions", mw, func(c *gin.Context) {
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
			mw := AutoRoute(autoRouteTestConfig(autoroute.ModeOn), autoroute.NewSelector(autoRouteTestConfig(autoroute.ModeOn), scoredEval(2)), nil, nil)
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
	mw := AutoRoute(autoRouteTestConfig(autoroute.ModeOff), nil, nil, nil)
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
			mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(tc.score)), nil, nil)
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
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil)
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
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, jev.NewFake()), nil, nil)
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
		mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil)
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
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, nil)
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
	mw := AutoRoute(cfg, autoroute.NewSelector(cfg, scoredEval(2)), nil, log)
	r, _ := autoRouteTestEnv(t, mw)
	postAutoBody(t, r, autoRouteChatBody)

	out := buf.String()
	for _, key := range []string{"auto_route", `"mode":"on"`, `"model":"test-pro"`, `"would_be":"test-pro"`, "decision_ms"} {
		if !strings.Contains(out, key) {
			t.Fatalf("日志缺少 %s: %s", key, out)
		}
	}
}
