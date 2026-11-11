package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledge"
)

// configureKnowledge loads only reviewed, manifest-bound public documents.
// Disabled is the compatibility default; an invalid enabled corpus fails startup.
func configureKnowledge(tb *agent.Toolbox, get func(string) string) error {
	enabled := strings.TrimSpace(get("AGENT_KNOWLEDGE_ENABLED"))
	if enabled == "" || enabled == "0" {
		return nil
	}
	if enabled != "1" {
		return fmt.Errorf("AGENT_KNOWLEDGE_ENABLED must be 0 or 1")
	}
	dir := strings.TrimSpace(get("AGENT_KNOWLEDGE_DIR"))
	if dir == "" {
		dir = "knowledge/platform"
	}
	corpus, err := knowledge.Load(os.DirFS(dir))
	if err != nil {
		return fmt.Errorf("load platform knowledge: %w", err)
	}
	tb.SetKnowledge(corpus)
	return nil
}
