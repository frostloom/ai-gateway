package agent

import (
	"net/http"
	"testing"
)

func TestExplicitEvaluationConfigurationIgnoresGlobalReaders(t *testing.T) {
	oldP, oldM := protoFromEnv, maxTokensFromEnv
	t.Cleanup(func() { protoFromEnv, maxTokensFromEnv = oldP, oldM })
	protoFromEnv = func() string { t.Fatal("evaluation read global protocol"); return "" }
	maxTokensFromEnv = func() int { t.Fatal("evaluation read global output"); return 0 }
	c, e := NewEvaluationLLMClient("https://provider.invalid/anthropic", "not-a-live-key", "configured", "anthropic", &http.Client{Transport: http.DefaultTransport}, 128)
	if e != nil || c.Protocol() != "anthropic" || c.maxTokens != 128 {
		t.Fatal("explicit evaluation configuration missing")
	}
	if _, e = NewEvaluationLLMClient("https://provider.invalid", "x", "x", "guess", &http.Client{Transport: http.DefaultTransport}, 128); e == nil {
		t.Fatal("implicit protocol accepted")
	}
	a := NewAdminClientWithToken("http://local.invalid", "explicit")
	if a.token != "explicit" {
		t.Fatal("tool token not explicit")
	}
}
