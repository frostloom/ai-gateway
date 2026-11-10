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
	if pathRefused(target) || pathRefused(cleaned) {
		return fmt.Errorf("refusing protected path")
	}
	if !exactAllowed(allow, cleaned) {
		return fmt.Errorf("target is not an exact allowlisted path")
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

var protectedRoots = [][]string{
	{"billing"},
	{"ledger"},
	{"eval", "task-definitions", "2026-10-12-full-dev-v1"},
	{"deploy", "langfuse"},
}

func pathRefused(target string) bool {
	if target == "" || target == "." || target == string(filepath.Separator) {
		return true
	}
	if hasDotDot(target) {
		return true
	}
	return hitsProtected(target)
}

func hasDotDot(target string) bool {
	for _, part := range strings.Split(filepath.ToSlash(target), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func hitsProtected(target string) bool {
	parts := splitPath(target)
	for _, root := range protectedRoots {
		if containsSequence(parts, root) || isParentOfRoot(parts, root) {
			return true
		}
	}
	return false
}

func splitPath(target string) []string {
	raw := strings.Split(strings.ToLower(filepath.ToSlash(target)), "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func containsSequence(parts, root []string) bool {
	if len(root) == 0 || len(parts) < len(root) {
		return false
	}
	for i := 0; i+len(root) <= len(parts); i++ {
		if equalParts(parts[i:i+len(root)], root) {
			return true
		}
	}
	return false
}

func isParentOfRoot(parts, root []string) bool {
	for n := 1; n < len(root); n++ {
		if len(parts) >= n && equalParts(parts[len(parts)-n:], root[:n]) {
			return true
		}
	}
	return false
}

func equalParts(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
