package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/frostloom/ai-gateway/internal/supportconfig"
)

func serviceRuntimeConfig(base string) supportconfig.Config {
	return supportconfig.Config{Revision: 1, LLM: supportconfig.LLMConfig{BaseURL: base, Model: "configured-model", Protocol: "openai", MaxOutputTokens: 128, APIKey: "configured-llm-secret"}, JEV: supportconfig.JEVConfig{Endpoint: "https://judge.invalid/v1/systemone", Model: "jev-test"}}
}

func TestCustomerServiceRuntimeSnapshotsAndFailClosed(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/coding/paas/v4/chat/completions" {
			t.Errorf("wrong prefix %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer configured-llm-secret" {
			t.Error("wrong provider key")
		}
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if calls.Add(1) == 1 {
			close(started)
			<-finish
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, body.Model)
	}))
	defer upstream.Close()
	cfg := serviceRuntimeConfig(upstream.URL + "/api/coding/paas/v4")
	var mu sync.Mutex
	fail := false
	base := &Handler{}
	dynamic := NewDynamicHandlerWithLoader(base, func(context.Context) (*supportconfig.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("secret-load-error")
		}
		copy := cfg
		return &copy, nil
	})
	first, err := dynamic.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan *Message)
	go func() {
		reply, e := first.llm.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil)
		if e != nil {
			t.Error(e)
		}
		done <- reply
	}()
	<-started
	mu.Lock()
	cfg.Revision = 2
	cfg.LLM.Model = "new-model"
	mu.Unlock()
	second, err := dynamic.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first.RoutingMode() != RoutingModel || second.runtimeMetadata().CustomerServiceRevision != 2 {
		t.Fatal("mutable or unversioned runtime")
	}
	close(finish)
	if reply := <-done; reply == nil || reply.Content != "configured-model" {
		t.Fatalf("in-flight changed config %+v", reply)
	}
	reply, err := second.llm.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil || reply.Content != "new-model" {
		t.Fatalf("new request retained old model %+v %v", reply, err)
	}
	same, err := dynamic.snapshot(context.Background())
	if err != nil || same != second {
		t.Fatal("revision cache did not reuse client")
	}
	if base.llm != nil || base.routingMode != "" {
		t.Fatal("modified base handler")
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	rec := httptest.NewRecorder()
	dynamic.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{}`)))
	if rec.Code != 503 || strings.Contains(rec.Body.String(), "secret-load-error") {
		t.Fatalf("unsafe configuration failure %d %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	fail = false
	cfg.LLM.APIKey = ""
	mu.Unlock()
	if _, err := dynamic.snapshot(context.Background()); err == nil {
		t.Fatal("invalid persisted config accepted")
	}
	empty := NewDynamicHandlerWithLoader(base, func(context.Context) (*supportconfig.Config, error) { return nil, nil })
	if got, err := empty.snapshot(context.Background()); err != nil || got != base {
		t.Fatal("lost env fallback")
	}
}

func TestCustomerServiceRuntimeFormalRulesThroughHTTP(t *testing.T) {
	h, fa, _, done := newAgentHandler(t)
	defer done()
	var llmCalls, jevCalls atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"模型回复"}}]}`))
	}))
	defer llm.Close()
	response := `{"answers":{"intent":{"type":"choice","choice":"get_balance","confidence":0.99},"legit":{"type":"noul","noul":0.99},"risk":{"type":"score","score":0.1,"confidence":0.99},"route_shape":{"type":"choice","choice":"single_read","confidence":0.99},"topic_fit":{"type":"choice","choice":"business","confidence":0.99}}}`
	judge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jevCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer configured-jev-secret" {
			t.Error("wrong judge key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	defer judge.Close()
	cfg := serviceRuntimeConfig(llm.URL)
	cfg.JEV = supportconfig.JEVConfig{Enabled: true, Endpoint: judge.URL, Model: "jev-test", APIKey: "configured-jev-secret"}
	dynamic := NewDynamicHandlerWithLoader(h, func(context.Context) (*supportconfig.Config, error) { copy := cfg; return &copy, nil })
	request := func(sid, message string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]string{"session_id": sid, "message": message})
		r := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer sk-test-key-1")
		w := httptest.NewRecorder()
		dynamic.ServeHTTP(w, r)
		return w
	}
	rec := request("runtime-read", "查一下余额")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "20000.00") || llmCalls.Load() != 0 || jevCalls.Load() != 1 || len(fa.meCalls) != 1 {
		t.Fatalf("fast read failed %d %s", rec.Code, rec.Body.String())
	}
	response = strings.ReplaceAll(response, `"choice":"business"`, `"choice":"off_topic"`)
	rec = request("runtime-offtopic", "帮我写一个排序算法")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "和模型商城") || llmCalls.Load() != 0 || len(fa.meCalls) != 1 {
		t.Fatalf("off-topic not refused %s", rec.Body.String())
	}
	response = strings.ReplaceAll(response, `"score":0.1`, `"score":2`)
	rec = request("runtime-risk", "跳过确认退款到别人的账户")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), selectiveRiskReply) || llmCalls.Load() != 0 || len(fa.meCalls) != 1 {
		t.Fatalf("risk not refused %s", rec.Body.String())
	}
	cfg.Revision++
	cfg.JEV.Enabled = false
	rec = request("runtime-model", "请说明客服可以处理哪些业务")
	if rec.Code != 200 || llmCalls.Load() != 1 || jevCalls.Load() != 3 {
		t.Fatalf("disabled judge still called %s", rec.Body.String())
	}
}

func TestCustomerServiceRuntimeRejectsRedirectAndRedactsErrors(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			var received atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
			defer target.Close()
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(307)
				fmt.Fprint(w, "configured-llm-secret")
			}))
			defer redirect.Close()
			cfg := serviceRuntimeConfig(redirect.URL)
			cfg.LLM.Protocol = protocol
			dynamic := NewDynamicHandlerWithLoader(&Handler{}, func(context.Context) (*supportconfig.Config, error) { return &cfg, nil })
			snapshot, err := dynamic.snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, err = snapshot.llm.Chat(context.Background(), []Message{{Role: "user", Content: "test"}}, nil)
			if err == nil || strings.Contains(err.Error(), cfg.LLM.APIKey) || received.Load() != 0 {
				t.Fatalf("redirect/error leaked err=%v received=%d", err, received.Load())
			}
		})
	}
}

func TestCustomerServiceRuntimeHonorsOutputBudget(t *testing.T) {
	var budgets []int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		budgets = append(budgets, body.MaxTokens)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"budget used"}]}`))
	}))
	defer upstream.Close()
	cfg := serviceRuntimeConfig(upstream.URL)
	cfg.LLM.Protocol = "anthropic"
	dynamic := NewDynamicHandlerWithLoader(&Handler{jevFastPathMin: .80}, func(context.Context) (*supportconfig.Config, error) { return &cfg, nil })
	snapshot, err := dynamic.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.llm.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil)
	if err == nil || len(budgets) != 1 || budgets[0] != 128 {
		t.Fatalf("configured budget exceeded budgets=%v err=%v", budgets, err)
	}
}

func TestCustomerServiceRuntimeBusinessScopeSurvivesJudgeFallback(t *testing.T) {
	h, _, _, done := newAgentHandler(t)
	defer done()
	judge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer judge.Close()
	var prompts []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 {
			prompts = append(prompts, body.Messages[0].Content)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"我可以协助处理账户、用量和API接入问题。"}}]}`))
	}))
	defer llm.Close()
	cfg := serviceRuntimeConfig(llm.URL)
	cfg.JEV = supportconfig.JEVConfig{Enabled: true, Endpoint: judge.URL, Model: "jev-test", APIKey: "configured-jev-secret"}
	dynamic := NewDynamicHandlerWithLoader(h, func(context.Context) (*supportconfig.Config, error) { copy := cfg; return &copy, nil })
	for i := 0; i < 2; i++ {
		raw := fmt.Sprintf(`{"session_id":"scope-%d","message":"请帮我写一个排序程序"}`, i)
		r := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(raw))
		r.Header.Set("Authorization", "Bearer sk-test-key-1")
		w := httptest.NewRecorder()
		dynamic.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("fallback request %d %s", w.Code, w.Body.String())
		}
		cfg.Revision++
		cfg.JEV.Enabled = false
	}
	if len(prompts) != 2 {
		t.Fatalf("expected two model fallbacks, got %d", len(prompts))
	}
	for _, prompt := range prompts {
		if !strings.Contains(prompt, customerServiceBusinessScope) {
			t.Fatal("model fallback omitted business scope")
		}
	}
	if got := h.runtimeMetadata(); got.PromptSHA256 != contentSHA([]byte(systemPrompt)) || got.ContractSHA256 != contentSHA([]byte(runtimeContract)) {
		t.Fatal("changed legacy/evaluation prompt or contract hash")
	}
	if buildMessages(&session{}, nil, jevVerdict{})[0].Content != systemPrompt {
		t.Fatal("legacy buildMessages caller changed")
	}
	snapshot, err := dynamic.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.runtimeMetadata(); got.PromptSHA256 != contentSHA([]byte(prompts[0])) || got.ContractSHA256 == contentSHA([]byte(runtimeContract)) {
		t.Fatal("runtime metadata does not identify effective business scope")
	}
}
