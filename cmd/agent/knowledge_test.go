package main

import (
	"github.com/frostloom/ai-gateway/internal/agent"
	"testing"
)

func TestKnowledgeUnavailable(t *testing.T) {
	for _, env := range []map[string]string{{}, {"AGENT_KNOWLEDGE_ENABLED": "no"}, {"AGENT_KNOWLEDGE_ENABLED": "true"}, {"AGENT_KNOWLEDGE_ENABLED": "true", "AGENT_KNOWLEDGE_DIR": "/nonexistent"}, {"AGENT_KNOWLEDGE_ENABLED": "true", "AGENT_KNOWLEDGE_DIR": "../../knowledge/platform"}} {
		tb := agent.NewToolbox(nil)
		e := configureKnowledge(tb, func(k string) string { return env[k] })
		good := len(env) == 0 || env["AGENT_KNOWLEDGE_DIR"] == "../../knowledge/platform"
		if (e == nil) != good {
			t.Fatalf("config result %v", e)
		}
	}
}
