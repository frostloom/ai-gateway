package taskbench

import (
	"fmt"
	"path/filepath"
)

func readWindowStoppedV5Files(root string) (map[string][]byte, error) {
	files := make(map[string][]byte, 24)
	for name, sha := range windowStoppedV5ArtifactPins() {
		raw, err := readWindowActualFile(filepath.Join(root, filepath.FromSlash(name)), sha, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("stopped v5 original top artifact changed: %s", name)
		}
		files[name] = raw
	}
	return files, nil
}

func readWindowStoppedV5Native(dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	return readWindowStoppedV4Native(dir, payload, ledger)
}
