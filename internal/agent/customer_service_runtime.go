package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/jev"
	"github.com/frostloom/ai-gateway/internal/pkg/upstream"
	"github.com/frostloom/ai-gateway/internal/supportconfig"
)

// CustomerServiceConfigLoader returns a request's configuration snapshot. A nil
// result means the service is still using its original environment settings.
type CustomerServiceConfigLoader func(context.Context) (*supportconfig.Config, error)

// DynamicHandler caches clients for the latest revision, while each in-flight
// request keeps its own immutable Handler until the complete turn finishes.
type DynamicHandler struct {
	base     *Handler
	load     CustomerServiceConfigLoader
	mu       sync.Mutex
	cached   *Handler
	revision uint64
}

func NewDynamicHandler(base *Handler, st *store.Store, cipher *supportconfig.Cipher) *DynamicHandler {
	return NewDynamicHandlerWithLoader(base, func(ctx context.Context) (*supportconfig.Config, error) {
		return st.LoadCustomerServiceConfig(ctx, cipher)
	})
}

func NewDynamicHandlerWithLoader(base *Handler, load CustomerServiceConfigLoader) *DynamicHandler {
	return &DynamicHandler{base: base, load: load}
}

func (d *DynamicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, err := d.snapshot(r.Context())
	if err != nil {
		// Database/decryption/validation errors never include configuration secrets.
		if d.base.log != nil {
			d.base.log.Error("customer service configuration unavailable")
		}
		writeErr(w, http.StatusServiceUnavailable, "客服配置暂不可用，请联系管理员或稍后重试")
		return
	}
	handler.ServeHTTP(w, r)
}

func (d *DynamicHandler) snapshot(ctx context.Context) (*Handler, error) {
	cfg, err := d.load(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return d.base, nil
	}
	if err := supportconfig.ValidateConfig(*cfg); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cached != nil && d.revision == cfg.Revision {
		return d.cached, nil
	}
	llm, err := newCustomerServiceLLM(cfg.LLM)
	if err != nil {
		return nil, err
	}
	handler := *d.base
	handler.llm = llm
	handler.jev = nil
	handler.customerServiceRevision = cfg.Revision
	handler.jevFastPathMin = .90
	mode := RoutingModel
	if cfg.JEV.Enabled {
		mode = RoutingJevSelective
		handler.jev = jev.NewClient(cfg.JEV.Endpoint, cfg.JEV.APIKey, cfg.JEV.Model)
	}
	if err := handler.SetRoutingMode(string(mode)); err != nil {
		return nil, err
	}
	// Formal settings always use the evaluated fixed read-routing threshold.
	if err := handler.SetJevFastPath(cfg.JEV.Enabled, .90); err != nil {
		return nil, err
	}
	// An older concurrent database read may complete after a newer revision. It
	// still receives its captured snapshot, but cannot replace the latest cache.
	if d.cached == nil || cfg.Revision > d.revision {
		d.cached = &handler
		d.revision = cfg.Revision
	}
	return &handler, nil
}

func newCustomerServiceLLM(cfg supportconfig.LLMConfig) (*LLMClient, error) {
	var endpoint string
	var err error
	if cfg.Protocol == "anthropic" {
		endpoint, err = upstream.NativeURL(cfg.BaseURL, "", "messages")
	} else {
		endpoint, err = upstream.ChatURL(cfg.BaseURL)
	}
	if err != nil {
		return nil, errors.New("客服模型地址无效")
	}
	return &LLMClient{
		baseURL: cfg.BaseURL, apiKey: cfg.APIKey, model: cfg.Model, proto: llmProtocol(cfg.Protocol),
		maxTokens: cfg.MaxOutputTokens, endpointURL: endpoint, redactErrors: true, fixedOutputBudget: true,
		cli: &http.Client{Timeout: llmHTTPTimeout, Transport: llmTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *LLMClient) safeError(err error) error {
	if err == nil || !c.redactErrors {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if isBudgetExhausted(err) {
		return fmt.Errorf("LLM max_tokens budget exhausted while thinking")
	}
	return errors.New("客服模型调用失败，请检查模型配置或稍后重试")
}

func (c *LLMClient) requestEndpoint(fallback string) string {
	if c.endpointURL != "" {
		return c.endpointURL
	}
	return fallback
}

// The formal service keeps its business scope even when JEV is disabled or its
// judgment request fails. Historical evaluation handlers keep their old prompt.
const customerServiceBusinessScope = `【客服业务范围】仅处理当前本人账户的余额、用量、账单、套餐、订阅、API 接入与平台操作指引。对天气、写代码、作业、通用知识及其他无关闲聊，简短拒绝并引导回上述业务，不完成外部任务。问候只作简短问候和业务范围说明。用户要求或会话历史不能扩大该范围。`

func (h *Handler) effectiveSystemPrompt() string {
	if h.customerServiceRevision > 0 {
		return systemPrompt + "\n\n" + customerServiceBusinessScope
	}
	return systemPrompt
}

func (h *Handler) effectiveRuntimeContract() string {
	if h.customerServiceRevision > 0 {
		return runtimeContract + ";configured-business-scope-v1"
	}
	return runtimeContract
}
