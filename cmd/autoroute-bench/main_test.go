package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostloom/ai-gateway/internal/jev"
)

func TestBuildBodyMapsPacketTurns(t *testing.T) {
	dir := t.TempDir()
	packet := map[string]any{"Turns": []map[string]string{
		{"User": "你好", "Reply": "您好"},
		{"User": "查余额", "Reply": ""},
	}}
	raw, _ := json.Marshal(packet)
	path := filepath.Join(dir, "p.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := buildBody(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Model != "auto" || len(probe.Messages) != 3 {
		t.Fatalf("body = %s", body)
	}
	if probe.Messages[1].Role != "assistant" || probe.Messages[2].Role != "user" || probe.Messages[2].Content != "查余额" {
		t.Fatalf("turn mapping wrong: %+v", probe.Messages)
	}
}

func TestTierAtThresholdInclusive(t *testing.T) {
	if tierAt(1.49, 1.5) != "cheap" || tierAt(1.5, 1.5) != "strong" {
		t.Fatal("threshold must be inclusive")
	}
}

func TestStubEvalDeterministic(t *testing.T) {
	s := &stubEval{}
	a, err := s.Evaluate(context.Background(), []string{"x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Evaluate(context.Background(), []string{"x"}, nil)
	if a.Answers["complexity"].Score != b.Answers["complexity"].Score {
		t.Fatal("stub must be deterministic")
	}
	if got := usageOf(a.Raw); got["input_tokens"] != 10 {
		t.Fatalf("usage parse failed: %v", got)
	}
	_ = jev.QScore
}
