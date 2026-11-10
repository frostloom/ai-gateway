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
	return fmt.Errorf("not started")
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
		if containsSequence(parts, root) {
			return true
		}
	}
	return removalDeletesProtected(target)
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

func removalDeletesProtected(target string) bool {
	targetForms, err := absoluteForms(target)
	if err != nil {
		return true
	}
	for _, form := range targetForms {
		if isFilesystemRoot(form) {
			return true
		}
	}
	cwdForms, err := absoluteForms(".")
	if err != nil {
		return true
	}
	for _, targetForm := range targetForms {
		for _, cwdForm := range cwdForms {
			if strings.EqualFold(targetForm, cwdForm) {
				return true
			}
		}
	}
	for _, cwdForm := range cwdForms {
		for _, root := range protectedRoots {
			protectedForm := filepath.Clean(filepath.Join(append([]string{cwdForm}, root...)...))
			for _, targetForm := range targetForms {
				if deletesByRemoval(targetForm, protectedForm) {
					return true
				}
			}
		}
	}
	return false
}

func absoluteForms(target string) ([]string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	forms := []string{abs}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return forms, nil
	}
	resolved = filepath.Clean(resolved)
	if !strings.EqualFold(resolved, abs) {
		forms = append(forms, resolved)
	}
	return forms, nil
}

func isFilesystemRoot(target string) bool {
	clean := filepath.Clean(target)
	return clean == string(filepath.Separator)
}

func deletesByRemoval(target, protected string) bool {
	target = strings.ToLower(filepath.Clean(target))
	protected = strings.ToLower(filepath.Clean(protected))
	if target == protected {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(target, sep) {
		target += sep
	}
	return strings.HasPrefix(protected, target)
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
