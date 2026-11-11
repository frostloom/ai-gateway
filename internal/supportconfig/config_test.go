package supportconfig

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func configured() Config {
	return Config{LLM: LLMConfig{BaseURL: "https://llm.example/v1", Model: "model", Protocol: "openai", MaxOutputTokens: 8192, APIKey: "private-llm-key"}, JEV: JEVConfig{Enabled: true, Endpoint: "https://jev.example/v1/systemone", Model: "jev-latest", APIKey: "private-jev-key"}}
}

func TestCipherBindsSecretsToFieldAndMaster(t *testing.T) {
	c, err := NewCipher(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal("private-secret", "llm")
	if err != nil || strings.Contains(sealed, "private-secret") {
		t.Fatal("secret not encrypted", err)
	}
	other, err := c.Seal("private-secret", "llm")
	if err != nil || sealed == other {
		t.Fatal("nonce was reused", err)
	}
	opened, err := c.Open(sealed, "llm")
	if err != nil || opened != "private-secret" {
		t.Fatal("could not decrypt", err)
	}
	if _, err := c.Open(sealed, "jev"); err == nil {
		t.Fatal("field-swapped key decrypted")
	}
	wrong, _ := NewCipher(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))))
	if _, err := wrong.Open(sealed, "llm"); err == nil {
		t.Fatal("wrong master decrypted")
	}
	if _, err := c.Open(sealed[:len(sealed)-4]+"AAAA", "llm"); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	if _, err := c.Open("plaintext", "llm"); err == nil {
		t.Fatal("plaintext accepted")
	}
	var absent *Cipher
	if _, err := absent.Seal("", "llm"); err == nil {
		t.Fatal("absent cipher accepted")
	}
	for _, key := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := NewCipher(key); err == nil {
			t.Fatal("invalid master accepted")
		}
	}
}

func TestConfigRedactionAndValidation(t *testing.T) {
	cfg := configured()
	for _, value := range []any{cfg, Redact(&cfg)} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), cfg.LLM.APIKey) || strings.Contains(string(raw), cfg.JEV.APIKey) {
			t.Fatal("secret exposed in JSON")
		}
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"credential URL", func(c *Config) { c.LLM.BaseURL = "https://user:password@example.com" }},
		{"query URL", func(c *Config) { c.LLM.BaseURL = "https://example.com?key=secret" }},
		{"model control", func(c *Config) { c.LLM.Model = "model\n" }},
		{"unsupported protocol", func(c *Config) { c.LLM.Protocol = "responses" }},
		{"zero budget", func(c *Config) { c.LLM.MaxOutputTokens = 0 }},
		{"large budget", func(c *Config) { c.LLM.MaxOutputTokens = 65537 }},
		{"missing llm key", func(c *Config) { c.LLM.APIKey = "" }},
		{"missing jev key", func(c *Config) { c.JEV.APIKey = "" }},
		{"header injection", func(c *Config) { c.JEV.APIKey = "secret\r\ninject" }},
		{"reflected secret", func(c *Config) { c.LLM.Model = c.LLM.APIKey }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := cfg
			tc.change(&bad)
			err := ValidateConfig(bad)
			if err == nil {
				t.Fatal("invalid config accepted")
			}
			if strings.Contains(err.Error(), cfg.LLM.APIKey) || strings.Contains(err.Error(), cfg.JEV.APIKey) {
				t.Fatal("error exposed secret")
			}
		})
	}
	cfg.JEV = JEVConfig{}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal("disabled empty JEV rejected", err)
	}
	if !SameOrigin("https://EXAMPLE.com/v1", "https://example.com/v2") {
		t.Fatal("same origin rejected")
	}
	for _, url := range []string{"http://example.com/v1", "https://example.com:444/v1", "https://other.example/v1"} {
		if SameOrigin("https://example.com/v1", url) {
			t.Fatal("different origin accepted")
		}
	}
}

func TestDefaultFromEnvRetainsKeysWithoutSerializing(t *testing.T) {
	values := map[string]string{"AGENT_LLM_API_KEY": "env-private-llm", "JEV_API_KEY": "env-private-jev", "AGENT_LLM_PROTOCOL": "messages"}
	cfg := DefaultFromEnv(func(k string) string { return values[k] })
	if cfg.LLM.Protocol != "anthropic" || cfg.LLM.MaxOutputTokens != 8192 || cfg.LLM.APIKey != values["AGENT_LLM_API_KEY"] || !cfg.JEV.Enabled {
		t.Fatal("incorrect environment defaults")
	}
}

func TestRedactDoesNotEchoSecretsFromInvalidEnvironmentMetadata(t *testing.T) {
	cfg := configured()
	cfg.LLM.BaseURL = "https://llm.example/" + cfg.LLM.APIKey
	cfg.JEV.Model = cfg.JEV.APIKey
	raw, _ := json.Marshal(Redact(&cfg))
	if strings.Contains(string(raw), cfg.LLM.APIKey) || strings.Contains(string(raw), cfg.JEV.APIKey) {
		t.Fatal("environment metadata echoed a secret")
	}
	cfg.LLM.BaseURL = "https://llm.example?api_key=another-secret"
	if Redact(&cfg).LLM.BaseURL != "" {
		t.Fatal("unsafe environment URL shown")
	}
}
