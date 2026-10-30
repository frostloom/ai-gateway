package taskbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// The new protocol records the identity of the FD that supplied the bytes.
// The existing no-pass readers are deliberately unchanged.
type historyFileIdentity struct {
	Dev, Ino           uint64
	Mode               os.FileMode
	UID, GID           uint32
	Links              uint64
	Size, Mtime, Ctime int64
}

func historyIdentity(info os.FileInfo) (historyFileIdentity, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return historyFileIdentity{}, fmt.Errorf("history identity unsupported")
	}
	ct, err := historyCTimeNS(info)
	if err != nil {
		return historyFileIdentity{}, err
	}
	return historyFileIdentity{uint64(s.Dev), uint64(s.Ino), info.Mode(), s.Uid, s.Gid, uint64(s.Nlink), info.Size(), info.ModTime().UnixNano(), ct}, nil
}
func historyPathIdentity(path string, dir bool) (historyFileIdentity, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return historyFileIdentity{}, fmt.Errorf("history canonical path required")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return historyFileIdentity{}, fmt.Errorf("history path alias")
	}
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() != dir || !dir && !info.Mode().IsRegular() {
		return historyFileIdentity{}, fmt.Errorf("history path type")
	}
	return historyIdentity(info)
}
func historyDirectoryEqual(a, b historyFileIdentity, strict bool) bool {
	if a.Dev != b.Dev || a.Ino != b.Ino || a.Mode != b.Mode || a.UID != b.UID || a.GID != b.GID {
		return false
	}
	return !strict || a == b
}

type historyObservedFile struct {
	raw      []byte
	digest   string
	identity historyFileIdentity
}

func (s historyReadSpec) validate() error {
	limit := int64(1 << 30) // Additional bounded admission for the new mode.
	if s.kind == historyReadV7File || s.kind == historyReadInventoryStream {
		limit = 256 << 20 // Preserve the original V7 file/inventory bounds.
	}
	if s.kind > historyReadExecutableStream || s.max < 1 || s.max > limit || len(s.path) > 4096 || !filepath.IsAbs(s.path) || filepath.Clean(s.path) != s.path || s.kind != historyReadBytes && !comparisonDigest(s.sha) || s.sha != "" && !comparisonDigest(s.sha) {
		return fmt.Errorf("history leaf specification")
	}
	return nil
}

func observeHistoryFile(ctx context.Context, s historyReadSpec, retain bool) (out historyObservedFile, err error) {
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if err = s.validate(); err != nil {
		return out, err
	}
	before, err := historyPathIdentity(s.path, false)
	if err != nil || before.Links != 1 || before.Size > s.max || s.kind == historyReadExecutableStream && before.Size < 1 {
		return out, fmt.Errorf("history file metadata")
	}
	fd, err := syscall.Open(s.path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return out, fmt.Errorf("history file open")
	}
	f := os.NewFile(uintptr(fd), s.path)
	defer func() { err = errors.Join(err, f.Close()) }()
	fi, err := f.Stat()
	if err != nil {
		return out, fmt.Errorf("history opened file")
	}
	opened, err := historyIdentity(fi)
	if err != nil || before != opened {
		return out, fmt.Errorf("history opened identity")
	}
	h := sha256.New()
	var n int64
	if retain {
		out.raw, err = io.ReadAll(io.TeeReader(io.LimitReader(f, s.max+1), h))
		n = int64(len(out.raw))
	} else {
		n, err = io.Copy(h, io.LimitReader(f, s.max+1))
	}
	if err != nil {
		return out, fmt.Errorf("history file read")
	}
	finalInfo, err := f.Stat()
	if err != nil {
		return out, fmt.Errorf("history final FD")
	}
	final, err := historyIdentity(finalInfo)
	if err != nil {
		return out, err
	}
	after, err := historyPathIdentity(s.path, false)
	if err != nil || before != final || before != after || n != opened.Size || n > s.max {
		return out, fmt.Errorf("history bytes or identity changed")
	}
	out.digest = hex.EncodeToString(h.Sum(nil))
	out.identity = opened
	if s.sha != "" && out.digest != s.sha {
		return out, fmt.Errorf("history file digest changed")
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, nil
}

type historyLockLease struct {
	file     *os.File
	identity historyFileIdentity
	path     string
}

func openHistoryLock(path string) (lease *historyLockLease, err error) {
	before, err := historyPathIdentity(path, false)
	if err != nil || before.Size != 0 || before.Links != 1 {
		return nil, fmt.Errorf("history lock metadata")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("history lock open")
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close())
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	opened, err := historyIdentity(info)
	if err != nil || opened != before {
		return nil, fmt.Errorf("history lock identity")
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("history lock occupied")
	}
	return &historyLockLease{f, opened, path}, nil
}
func (l *historyLockLease) check() error {
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	fdID, err := historyIdentity(info)
	if err != nil {
		return err
	}
	pathID, err := historyPathIdentity(l.path, false)
	if err != nil || fdID != l.identity || pathID != l.identity {
		return fmt.Errorf("history lock changed")
	}
	return nil
}
func (l *historyLockLease) close() error {
	return errors.Join(l.check(), syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN), l.file.Close())
}
