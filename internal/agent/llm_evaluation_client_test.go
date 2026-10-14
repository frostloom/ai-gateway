package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestEvaluationAnthropicNeverGrowsBudget(t *testing.T) {
	var budgets []int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Max int `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		budgets = append(budgets, p.Max)
		w.Write([]byte(`{"model":"local","content":[{"type":"thinking","thinking":"unfinished"}],"stop_reason":"max_tokens","usage":{"input_tokens":8,"output_tokens":128}}`))
	}))
	defer s.Close()
	c, e := NewEvaluationLLMClient(s.URL, "local", "local", "anthropic", s.Client(), 128)
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.Chat(context.Background(), []Message{{Role: "user", Content: "充值100元"}}, nil)
	if e == nil || !strings.Contains(e.Error(), "thinking") || len(budgets) != 1 || budgets[0] != 128 {
		t.Fatalf("fixed evaluation grew budget: %v %v", budgets, e)
	}
}

func TestEvaluationAnthropicExplicitlyDisablesThinking(t *testing.T) {
	var thinking string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Thinking struct {
				Type string `json:"type"`
			} `json:"thinking"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		thinking = p.Thinking.Type
		w.Write([]byte(`{"model":"local","content":[{"type":"text","text":"请问金额？"}],"stop_reason":"end_turn"}`))
	}))
	defer s.Close()
	c, e := NewEvaluationLLMClient(s.URL, "local", "local", "anthropic", s.Client(), 128)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Chat(context.Background(), []Message{{Role: "user", Content: "充值"}}, nil); e != nil {
		t.Fatal(e)
	}
	if thinking != "disabled" {
		t.Fatalf("thinking=%q", thinking)
	}
}
