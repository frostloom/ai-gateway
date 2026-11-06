package taskbench

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// Streamed sources/Git/executable retain the existing individual byte caps and
// consume the same pass transcript. All other DATA uses the existing reader.
func readWindowStoppedV14Inventory(p *historyReadPass, inv windowStoppedV11Inventory) (map[string][]byte, error) {
	subset := inv
	subset.Files = nil
	subset.Bytes = 0
	seen := map[string]bool{}
	for _, f := range inv.Files {
		if !filepath.IsLocal(f.Path) || filepath.ToSlash(filepath.Clean(f.Path)) != f.Path || strings.Contains(f.Path, "\\") || seen[f.Path] || !comparisonDigest(f.SHA256) || f.Bytes < 0 {
			return nil, fmt.Errorf("stopped14 descriptor entry")
		}
		seen[f.Path] = true
		if strings.HasPrefix(f.Path, "regression/source/") || strings.HasPrefix(f.Path, "source-git/") || f.Path == "full-window-actual-v14" {
			continue
		}
		subset.Files = append(subset.Files, f)
		subset.Bytes += f.Bytes
	}
	subset.Count = len(subset.Files)
	return readWindowStoppedV11Inventory(p, inv.Root, subset)
}
func readWindowStoppedV14SourceInventory(p *historyReadPass, m windowActualManifest, inv windowStoppedV11Inventory) error {
	sources := map[string]string{}
	git := map[string]string{}
	for _, f := range inv.Files {
		if strings.HasPrefix(f.Path, "regression/source/") {
			sources[strings.TrimPrefix(f.Path, "regression/source/")] = f.SHA256
		}
		if strings.HasPrefix(f.Path, "source-git/") {
			git[strings.TrimPrefix(f.Path, "source-git/")] = f.SHA256
		}
	}
	want := map[string]string{}
	for k, v := range m.Sources {
		want[k] = v
	}
	want[".git"] = windowStoppedV14GitSHA
	if !reflect.DeepEqual(sources, want) || len(git) != 25 {
		return fmt.Errorf("stopped14 full source/Git descriptor")
	}
	if e := readWindowStoppedV14SourcesWithPass(p, m.SourceRoot, m.Sources, true); e != nil {
		return e
	}
	if e := readWindowStoppedV14SourcesWithPass(p, windowStoppedV14Root+"/source-git", git, false); e != nil {
		return e
	}
	// Compare descriptor identities to the same FD-bound observations, including
	// the executable streamed by companion validation. End reopens all entries.
	for _, f := range inv.Files {
		path := filepath.Join(inv.Root, filepath.FromSlash(f.Path))
		p.mu.Lock()
		v := p.paths[path]
		valid := v != nil && v.digest == f.SHA256 && stoppedV11IdentityMatches(v.identity, f.Identity)
		p.mu.Unlock()
		if !valid {
			return fmt.Errorf("stopped14 original identity %s", f.Path)
		}
	}
	return nil
}
