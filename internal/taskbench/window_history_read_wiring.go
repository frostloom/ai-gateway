package taskbench

import (
	"context"
	"fmt"
)

// The sole top-level lifecycle; nested historical constructors receive p.
func runHistoryReadPass[T any](ctx context.Context, load func(*historyReadPass) (T, error)) (T, error) {
	p, err := newHistoryReadPass(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	defer p.Abort()
	value, err := load(p)
	if err == nil {
		err = p.Finish()
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

func historyStoppedFile(p *historyReadPass, path, sha string, max int64) ([]byte, error) {
	if p == nil {
		return readWindowActualFile(path, sha, max)
	}
	return p.Read(historyReadSpec{historyReadFile, path, sha, max})
}
func historyStoppedBatch(p *historyReadPass, specs []windowStoppedReadSpec) ([][]byte, error) {
	if p == nil {
		return readWindowStoppedBatch4(specs)
	}
	if len(specs) < 1 || len(specs) > 4 {
		return nil, p.fail(fmt.Errorf("historical batch size"))
	}
	converted := make([]historyReadSpec, len(specs))
	for i, s := range specs {
		kind := historyReadFile
		if s.kind == windowStoppedReadBytes {
			kind = historyReadBytes
		} else if s.kind != windowStoppedReadFile {
			return nil, p.fail(fmt.Errorf("unknown historical batch reader"))
		}
		converted[i] = historyReadSpec{kind, s.path, s.sha256, s.maxBytes}
	}
	return p.Batch(converted)
}

func historyStoppedBytes(p *historyReadPass, path, sha string, max int64) ([]byte, error) {
	if p == nil {
		return readWindowActualBytes(path, sha, max)
	}
	return p.Read(historyReadSpec{historyReadBytes, path, sha, max})
}
func historyStoppedV7File(p *historyReadPass, path, sha string, max int64) ([]byte, error) {
	if p == nil {
		return readWindowStoppedV7File(path, sha, max)
	}
	return p.Read(historyReadSpec{historyReadV7File, path, sha, max})
}
func historyStoppedRoot(p *historyReadPass, path string) error {
	if p == nil {
		return nil
	}
	return p.MarkImmutableRoot(path)
}
func historyStoppedDir(p *historyReadPass, path string, names map[string]bool) error {
	if p == nil {
		return validateWindowStoppedV2Dir(path, names)
	}
	return p.CheckNamespace(path, names, func() error { return validateWindowStoppedV2Dir(path, names) })
}
func historyStoppedLock(p *historyReadPass, epoch int, path string, check func() error) error {
	if p == nil {
		return withWindowStoppedLock(path, check)
	}
	return p.WithStoppedLock(epoch, path, check)
}
func historyStoppedBinaryAttestation(p *historyReadPass, raw []byte, m windowActualManifest, sha, path string) error {
	return validateWindowActualBinaryAttestationWithPass(p, raw, m, sha, path)
}
