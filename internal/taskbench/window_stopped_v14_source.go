package taskbench

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const windowStoppedV14GitSHA = "f8c779242b97870db6bbaea7c5be4702a74446dcec7af3cda756598bb875b7cf"

// New Root14 source streams are uncached and close every directory against the
// authenticated source map. The one intentional gitdir file is separately pinned.
func readWindowStoppedV14SourcesWithPass(p *historyReadPass, root string, sources map[string]string, gitdir bool) error {
	if p == nil || len(sources) == 0 {
		return fmt.Errorf("Root14 source pass/map absent")
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
		if root != windowStoppedV14SourceRoot {
			return fmt.Errorf("Root14 gitdir root")
		}
		refs[".git"] = windowStoppedV14GitSHA
	}
	for n, h := range refs {
		if !filepath.IsLocal(n) || filepath.ToSlash(filepath.Clean(n)) != n || strings.Contains(n, "\\") || !comparisonDigest(h) {
			return fmt.Errorf("Root14 source identity")
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
				return fmt.Errorf("Root14 source namespace conflict")
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
	// The frozen Git metadata graph includes original empty directories. They
	// are part of the closed namespace even though no file entry names them.
	if root == windowStoppedV14Root+"/source-git" {
		dirs["."]["refs"] = true
		dirs["refs"] = map[string]bool{"heads": true, "tags": true}
		dirs["refs/heads"] = map[string]bool{}
		dirs["refs/tags"] = map[string]bool{}
		dirs["objects"]["info"] = true
		dirs["objects/info"] = map[string]bool{}
		names = names[:0]
		for n := range dirs {
			names = append(names, n)
		}
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
