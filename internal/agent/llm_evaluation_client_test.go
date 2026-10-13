package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLLMEvaluationClientExplicitBudget(t *testing.T) {
	seen := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Max int `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		seen = p.Max
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"简短回答"}}]}`))
	}))
	defer s.Close()
	c := NewLLMClient(s.URL, "local-only", "local-model")
	if err := c.ConfigureEvaluationClient(s.Client(), 128); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "test"}}, nil); err != nil {
		t.Fatal(err)
	}
	if seen != 128 {
		t.Fatalf("output cap=%d", seen)
	}
	if err := c.ConfigureEvaluationClient(nil, 128); err == nil {
		t.Fatal("nil client accepted")
	}
}
