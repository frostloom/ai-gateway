package taskbench

import (
	"fmt"
	"path/filepath"
)

func readWindowStoppedV5Files(root string) (map[string][]byte, error) {
	return readWindowStoppedV5FilesWithPass(nil, root)
}

func readWindowStoppedV5FilesWithPass(p *historyReadPass, root string) (map[string][]byte, error) {
	files := make(map[string][]byte, 24)
	for name, sha := range windowStoppedV5ArtifactPins() {
		raw, err := historyStoppedFile(p, filepath.Join(root, filepath.FromSlash(name)), sha, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("stopped v5 original top artifact changed: %s", name)
		}
		files[name] = raw
	}
	return files, nil
}

func readWindowStoppedV5Native(dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	return readWindowStoppedV5NativeWithPass(nil, dir, payload, ledger)
}

func readWindowStoppedV5NativeWithPass(p *historyReadPass, dir string, payload windowPayloadManifest, ledger []byte) (windowAccountingAudit, error) {
	return readWindowStoppedV4NativeWithPass(p, dir, payload, ledger)
}
