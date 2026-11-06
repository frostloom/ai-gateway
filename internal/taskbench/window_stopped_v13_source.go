package taskbench

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const windowStoppedV13GitSHA = "c4a404ed2a5aef99d15bb2d8adb8278409da8ca5ee8b7520398c9a87a1e93430"

// New Root4 source streams are uncached and close every directory against the
// authenticated source map. The one intentional gitdir file is separately pinned.
func readWindowStoppedV13SourcesWithPass(p *historyReadPass, root string, sources map[string]string, gitdir bool) error {
	if p == nil || len(sources) == 0 {
		return fmt.Errorf("Root4 source pass/map absent")
	}
	if e := historyStoppedRoot(p, root); e != nil {
		return e
	}
	dirs := map[string]map[string]bool{".": {}}
	names := make([]string, 0, len(sources))
	refs := map[string]string{}
	for n, h := range sources {
		refs[n] = h
	}
	if gitdir {
		if root != windowStoppedV13SourceRoot {
			return fmt.Errorf("Root4 gitdir root")
		}
		refs[".git"] = windowStoppedV13GitSHA
	}
	for n, h := range refs {
		if !filepath.IsLocal(n) || filepath.ToSlash(filepath.Clean(n)) != n || strings.Contains(n, "\\") || !comparisonDigest(h) {
			return fmt.Errorf("Root4 source identity")
		}
		names = append(names, n)
		parts := strings.Split(n, "/")
		for i, part := range parts {
			dir := "."
			if i > 0 {
				dir = strings.Join(parts[:i], "/")
			}
			if dirs[dir] == nil {
				dirs[dir] = map[string]bool{}
			}
			isDir := i < len(parts)-1
			if old, ok := dirs[dir][part]; ok && old != isDir {
				return fmt.Errorf("Root4 source namespace conflict")
			}
			dirs[dir][part] = isDir
		}
	}
	sort.Strings(names)
	for offset := 0; offset < len(names); offset += 4 {
		batch := names[offset:min(offset+4, len(names))]
		if e := runWindowStoppedV11Four(len(batch), func(i int) error {
			n := batch[i]
			_, e := p.Stream(historyReadInventoryStream, filepath.Join(root, filepath.FromSlash(n)), refs[n], windowActualSourceMaxBytes)
			return e
		}); e != nil {
			return e
		}
	}
	names = names[:0]
	for n := range dirs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		path := filepath.Join(root, filepath.FromSlash(n))
		if e := historyStoppedDir(p, path, dirs[n]); e != nil {
			return e
		}
	}
	return nil
}
