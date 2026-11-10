package localpaired

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	AllowPaid     bool
	ModelEndpoint string
}

func Run(ctx context.Context, opts Options) error {
	if !opts.AllowPaid {
		return fmt.Errorf("not started")
	}
	return nil
}

func Clean(allow []string, target string) error {
	if err := requireTask2Cases(); err != nil {
		return err
	}
	cleaned := filepath.Clean(target)
	if !exactAllowed(allow, cleaned) {
		return fmt.Errorf("target is not an exact allowlisted path")
	}
	if protectedPath(cleaned) {
		return fmt.Errorf("refusing protected path")
	}
	return os.RemoveAll(cleaned)
}

func requireTask2Cases() error {
	path := filepath.Join("eval", "campaigns", "2026-11-10-local-paired", "cases.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("new question bank: %w", err)
	}
	text := string(raw)
	for _, item := range localCases() {
		if !strings.Contains(text, item.ID) {
			return fmt.Errorf("new question bank missing %s", item.ID)
		}
	}
	return nil
}

func exactAllowed(allow []string, target string) bool {
	for _, item := range allow {
		if filepath.Clean(item) == target {
			return true
		}
	}
	return false
}

func protectedPath(target string) bool {
	slash := filepath.ToSlash(target)
	for _, marker := range []string{
		"billing",
		"ledger",
		"eval/task-definitions/2026-10-12-full-dev-v1",
		"deploy/langfuse",
	} {
		if strings.Contains(slash, marker) {
			return true
		}
	}
	return false
}
