package taskbench

import (
	"fmt"
	"path"
	"strings"
)

type windowExecutorEquivalenceV15 struct {
	Schema                string                             `json:"schema"`
	OriginalSourcesSHA256 string                             `json:"original_sources_sha256"`
	SourcesSHA256         string                             `json:"sources_sha256"`
	OriginalSources       map[string]string                  `json:"original_sources"`
	ChangedFiles          map[string]windowExecutorChangeV10 `json:"changed_files"`
}

// These pins must originate in the independent complete production/config
// review, never in the manifest or the untrusted equivalence artifact itself.
// ChangesSHA256 binds the entire exact before/after map, including additions
// and deletions. There is intentionally no path-prefix permission rule.
type windowExecutorEquivalencePinsV15 struct {
	OriginalSourcesSHA256 string
	SourcesSHA256         string
	ChangesSHA256         string
}

func expectedWindowV15ExecutorEquivalencePins() windowExecutorEquivalencePinsV15 {
	// Current-map and complete change-map pins require an independent external
	// freeze binding after archive/review; embedding self-source hashes here
	// would be circular. Empty pins fail closed until that runtime integration.
	return windowExecutorEquivalencePinsV15{OriginalSourcesSHA256: "47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00"}
}

func validateWindowV15ExecutorEquivalenceData(m windowActualManifest, e windowExecutorEquivalenceV15) error {
	if m.Schema != "full-window-actual-ready.v15" {
		return fmt.Errorf("v15 actual manifest required")
	}
	return validateWindowV15ExecutorEquivalencePinnedData(m.Sources, e, expectedWindowV15ExecutorEquivalencePins())
}

func validateWindowV15ExecutorEquivalencePinnedData(current map[string]string, e windowExecutorEquivalenceV15, pins windowExecutorEquivalencePinsV15) error {
	if !comparisonDigest(pins.OriginalSourcesSHA256) || !comparisonDigest(pins.SourcesSHA256) || !comparisonDigest(pins.ChangesSHA256) {
		return fmt.Errorf("v15 independently reviewed exact source pins absent")
	}
	if e.Schema != "full-window-executor-equivalence.v15" || e.OriginalSourcesSHA256 != pins.OriginalSourcesSHA256 || e.SourcesSHA256 != pins.SourcesSHA256 || windowV8DeclarationSHA(e.OriginalSources) != pins.OriginalSourcesSHA256 || windowV8DeclarationSHA(current) != pins.SourcesSHA256 || windowV8DeclarationSHA(e.ChangedFiles) != pins.ChangesSHA256 {
		return fmt.Errorf("v15 reviewed source/diff binding mismatch")
	}
	changes := make(map[string]windowExecutorChangeV10)
	for name, before := range e.OriginalSources {
		if !windowV15CanonicalSourcePath(name) || !comparisonDigest(before) {
			return fmt.Errorf("v15 invalid original source pin")
		}
		after, exists := current[name]
		if !exists || after != before {
			changes[name] = windowExecutorChangeV10{Before: before, After: after}
		}
	}
	for name, after := range current {
		if !windowV15CanonicalSourcePath(name) || !comparisonDigest(after) {
			return fmt.Errorf("v15 invalid current source pin")
		}
		if _, exists := e.OriginalSources[name]; !exists {
			changes[name] = windowExecutorChangeV10{After: after}
		}
	}
	if len(changes) != len(e.ChangedFiles) {
		return fmt.Errorf("v15 surplus or missing exact source changes")
	}
	for name, change := range changes {
		if e.ChangedFiles[name] != change {
			return fmt.Errorf("v15 unreviewed exact source change: %s", name)
		}
	}
	return nil
}

func windowV15CanonicalSourcePath(name string) bool {
	return name != "" && name != "." && name != ".." && path.Clean(name) == name && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") && !strings.Contains(name, "\\")
}
