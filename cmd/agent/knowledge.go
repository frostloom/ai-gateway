package main

import (
	"fmt"
	"github.com/frostloom/ai-gateway/internal/agent"
	"github.com/frostloom/ai-gateway/internal/knowledge"
	"os"
)

func configureKnowledge(tb *agent.Toolbox, get func(string) string) error {
	switch get("AGENT_KNOWLEDGE_ENABLED") {
	case "", "0", "false":
		return nil
	case "1", "true":
	default:
		return fmt.Errorf("invalid AGENT_KNOWLEDGE_ENABLED")
	}
	dir := get("AGENT_KNOWLEDGE_DIR")
	if dir == "" {
		return fmt.Errorf("AGENT_KNOWLEDGE_DIR is required")
	}
	c, e := knowledge.Load(os.DirFS(dir))
	if e != nil {
		return fmt.Errorf("invalid knowledge corpus: %w", e)
	}
	return tb.SetKnowledge(c)
}
