// Package supportconfig defines the server-side customer-service configuration.
package supportconfig

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/frostloom/ai-gateway/internal/pkg/upstream"
)

const MasterKeyEnv = "CUSTOMER_SERVICE_MASTER_KEY"

type LLMConfig struct {
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	Protocol        string `json:"protocol"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	APIKey          string `json:"-"`
}

type JEVConfig struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	APIKey   string `json:"-"`
}

type Config struct {
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
	LLM       LLMConfig `json:"llm"`
	JEV       JEVConfig `json:"jev"`
}

type LLMUpdate struct {
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	Protocol        string `json:"protocol"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	APIKey          string `json:"api_key"`
}

type JEVUpdate struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

type Update struct {
	Revision uint64    `json:"revision"`
	LLM      LLMUpdate `json:"llm"`
	JEV      JEVUpdate `json:"jev"`
}

type LLMView struct {
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	Protocol         string `json:"protocol"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	APIKeyConfigured bool   `json:"api_key_configured"`
}

type JEVView struct {
	Enabled          bool   `json:"enabled"`
	Endpoint         string `json:"endpoint"`
	Model            string `json:"model"`
	APIKeyConfigured bool   `json:"api_key_configured"`
}

type View struct {
	Revision        uint64    `json:"revision"`
	UpdatedAt       time.Time `json:"updated_at"`
	Configured      bool      `json:"configured"`
	EncryptionReady bool      `json:"encryption_ready"`
	LLM             LLMView   `json:"llm"`
	JEV             JEVView   `json:"jev"`
}

func Redact(cfg *Config) View {
	if cfg == nil {
		return View{}
	}
	publicValue := func(value string) string {
		decoded, _ := url.PathUnescape(value)
		for _, key := range []string{cfg.LLM.APIKey, cfg.JEV.APIKey} {
			if key != "" && (strings.Contains(value, key) || strings.Contains(decoded, key)) {
				return ""
			}
		}
		return value
	}
	llmBase, jevEndpoint := cfg.LLM.BaseURL, cfg.JEV.Endpoint
	if !validEndpoint(llmBase) {
		llmBase = ""
	}
	if jevEndpoint != "" && !validEndpoint(jevEndpoint) {
		jevEndpoint = ""
	}
	return View{
		Revision: cfg.Revision, UpdatedAt: cfg.UpdatedAt, Configured: cfg.Revision > 0,
		LLM: LLMView{BaseURL: publicValue(llmBase), Model: publicValue(cfg.LLM.Model), Protocol: publicValue(cfg.LLM.Protocol), MaxOutputTokens: cfg.LLM.MaxOutputTokens, APIKeyConfigured: cfg.LLM.APIKey != ""},
		JEV: JEVView{Enabled: cfg.JEV.Enabled, Endpoint: publicValue(jevEndpoint), Model: publicValue(cfg.JEV.Model), APIKeyConfigured: cfg.JEV.APIKey != ""},
	}
}

func validText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && s == strings.TrimSpace(s) && (!required || s != "") && !strings.ContainsFunc(s, unicode.IsControl)
}

func validEndpoint(value string) bool {
	if !validText(value, 512, true) {
		return false
	}
	_, err := upstream.ParseBase(value)
	return err == nil
}

func ValidateConfig(cfg Config) error {
	if !validEndpoint(cfg.LLM.BaseURL) {
		return errors.New("客服 LLM 地址须为不含凭据或查询参数的 HTTP(S) 地址")
	}
	if !validText(cfg.LLM.Model, 128, true) {
		return errors.New("客服 LLM 模型名称无效")
	}
	if cfg.LLM.Protocol != "openai" && cfg.LLM.Protocol != "anthropic" {
		return errors.New("客服 LLM 协议仅支持 openai 或 anthropic")
	}
	if cfg.LLM.MaxOutputTokens < 1 || cfg.LLM.MaxOutputTokens > 65536 {
		return errors.New("最大输出须为 1 至 65536 的整数")
	}
	if !validText(cfg.LLM.APIKey, 4096, true) {
		return errors.New("请配置有效的客服 LLM 密钥")
	}
	if cfg.JEV.Enabled && (!validEndpoint(cfg.JEV.Endpoint) || !validText(cfg.JEV.Model, 128, true) || cfg.JEV.APIKey == "") {
		return errors.New("启用 JEV 须配置有效地址、模型和密钥")
	}
	if cfg.JEV.Endpoint != "" && !validEndpoint(cfg.JEV.Endpoint) {
		return errors.New("JEV 地址须为不含凭据或查询参数的 HTTP(S) 地址")
	}
	if !validText(cfg.JEV.Model, 128, false) || !validText(cfg.JEV.APIKey, 4096, false) {
		return errors.New("JEV 模型或密钥无效")
	}
	for _, key := range []string{cfg.LLM.APIKey, cfg.JEV.APIKey} {
		if len(key) < 8 {
			continue
		}
		for _, value := range []string{cfg.LLM.BaseURL, cfg.LLM.Model, cfg.JEV.Endpoint, cfg.JEV.Model} {
			decoded, _ := url.PathUnescape(value)
			if strings.Contains(value, key) || strings.Contains(decoded, key) {
				return errors.New("模型信息和地址不能包含 API 密钥")
			}
		}
	}
	return nil
}

// SameOrigin includes the scheme and port so retaining a secret cannot silently
// redirect it to a new service or downgrade its transport.
func SameOrigin(first, second string) bool {
	a, err := upstream.ParseBase(first)
	if err != nil {
		return false
	}
	b, err := upstream.ParseBase(second)
	if err != nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func DefaultFromEnv(get func(string) string) Config {
	value := func(key, fallback string) string {
		if s := get(key); s != "" {
			return s
		}
		return fallback
	}
	base := value("AGENT_LLM_BASE_URL", "https://api.deepseek.com/v1")
	protocol := strings.ToLower(strings.TrimSpace(get("AGENT_LLM_PROTOCOL")))
	switch protocol {
	case "claude", "messages":
		protocol = "anthropic"
	case "chat", "chat_completions":
		protocol = "openai"
	}
	if protocol == "" {
		protocol = "openai"
		if strings.Contains(strings.ToLower(base), "anthropic") || strings.Contains(strings.ToLower(base), "messages") {
			protocol = "anthropic"
		}
	}
	maxTokens, err := strconv.Atoi(get("AGENT_LLM_MAX_TOKENS"))
	if err != nil || maxTokens <= 0 {
		maxTokens = 8192
	}
	return Config{LLM: LLMConfig{BaseURL: base, Model: value("AGENT_LLM_MODEL", "deepseek-flash"), Protocol: protocol, MaxOutputTokens: maxTokens, APIKey: get("AGENT_LLM_API_KEY")}, JEV: JEVConfig{Enabled: value("AGENT_ROUTING_MODE", "jev_selective") == "jev_selective", Endpoint: value("JEV_ENDPOINT", "https://api.typesafe.ai/v1/systemone"), Model: value("JEV_MODEL", "jev-latest"), APIKey: get("JEV_API_KEY")}}
}
