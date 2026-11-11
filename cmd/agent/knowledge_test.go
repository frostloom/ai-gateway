package main

import (
	"path/filepath"
	"testing"

	"github.com/frostloom/ai-gateway/internal/agent"
)

func TestConfigureKnowledge(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, dir  string
		wantError, wantTool bool
	}{
		{"disabled", "", "/missing", false, false},
		{"invalid flag", "yes", "", true, false},
		{"missing corpus", "1", t.TempDir(), true, false},
		{"reviewed corpus", "1", filepath.Join("..", "..", "knowledge", "platform"), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb := agent.NewToolbox(nil)
			err := configureKnowledge(tb, func(k string) string {
				if k == "AGENT_KNOWLEDGE_ENABLED" {
					return tc.enabled
				}
				return tc.dir
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			found := false
			for _, tool := range tb.All() {
				if tool.Function.Name == "search_platform_knowledge" {
					found = true
				}
			}
			if found != tc.wantTool {
				t.Fatalf("knowledge tool present=%v", found)
			}
		})
	}
}
