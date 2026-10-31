package observability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

const durableBatchLimit = 1 << 20
const durableCapacityLimit int64 = 4 << 30

// DurableSpoolConfig is an explicit evaluation-only local receipt boundary.
// Root must not yet exist; stopped or crashed roots can never be reopened for writing.
type DurableSpoolConfig struct {
	Root     string
	RunID    string
	MaxBytes int64
}

// DurableSpoolStatus contains no span bodies, credentials, or raw filesystem paths.
// Persisted bytes are local data, never evidence of remote delivery.
type DurableSpoolStatus struct {
	RunID          string `json:"run_id"`
	RootSHA256     string `json:"root_sha256"`
	MaxBytes       int64  `json:"max_bytes"`
	Batches        uint64 `json:"batches"`
	Spans          uint64 `json:"spans"`
	Bytes          int64  `json:"bytes"`
	StoredBytes    int64  `json:"stored_bytes"`
	LastLinkSHA256 string `json:"last_link_sha256"`
	Closed         bool   `json:"closed"`
	Poisoned       bool   `json:"poisoned"`
	// Disk replay cannot establish whether a historical fsync/close returned nil,
	// or whether an acquisition owner completed normally. Neither is authority.
	DataOnly                 bool `json:"data_only"`
	DurabilityReturnUnproven bool `json:"durability_return_unproven"`
	OwnerCompletionUnproven  bool `json:"owner_completion_unproven"`
}

type DurableSpanIdentity struct {
	TraceID           string `json:"trace_id"`
	SpanID            string `json:"span_id"`
	ParentSpanID      string `json:"parent_span_id"`
	StartTimeUnixNano uint64 `json:"start_time_unix_nano"`
	EndTimeUnixNano   uint64 `json:"end_time_unix_nano"`
}

type spoolHeader struct {
	Version    string
	RunID      string
	RootSHA256 string
	MaxBytes   int64
}
type spoolCommit struct {
	Sequence   uint64
	Previous   string
	BodySHA256 string
	BodyBytes  int64
	Spans      []DurableSpanIdentity
}
type spoolClose struct {
	Version        string
	RunID          string
	RootSHA256     string
	Batches        uint64
	Spans          uint64
	Bytes          int64
	LastLinkSHA256 string
}
type spoolLeaf struct {
	info os.FileInfo
	hash string
	size int64
}
type spoolAncestor struct {
	path string
	info os.FileInfo
}

// The narrow OS boundary lets storage errors propagate uniformly, including a
// close error after successful writes. No failure path deletes original files.
type spoolIO struct {
	write func(*os.File, []byte) (int, error)
	sync  func(*os.File) error
	close func(*os.File) error
}

func defaultSpoolIO() spoolIO {
	return spoolIO{write: func(f *os.File, b []byte) (int, error) { return f.Write(b) }, sync: func(f *os.File) error { return f.Sync() }, close: func(f *os.File) error { return f.Close() }}
}
func spoolDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func validRunID(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func (c DurableSpoolConfig) validate() error {
	if !filepath.IsAbs(c.Root) || filepath.Clean(c.Root) != c.Root || c.Root == string(filepath.Separator) || !validRunID(c.RunID) || c.MaxBytes <= 0 || c.MaxBytes > durableCapacityLimit {
		return errors.New("invalid durable spool configuration")
	}
	return nil
}
func spoolAncestors(path string) ([]spoolAncestor, error) {
	var result []spoolAncestor
	for p := path; ; p = filepath.Dir(p) {
		fi, e := os.Lstat(p)
		if e != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("invalid durable spool directory chain")
		}
		result = append(result, spoolAncestor{p, fi})
		if filepath.Dir(p) == p {
			break
		}
	}
	return result, nil
}
func checkSpoolAncestors(chain []spoolAncestor) error {
	for _, a := range chain {
		fi, e := os.Lstat(a.path)
		if e != nil || !fi.IsDir() || fi.Mode() != a.info.Mode() || !os.SameFile(fi, a.info) {
			return errors.New("durable spool directory identity changed")
		}
	}
	return nil
}
func regularSpoolFile(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && fi.Mode().IsRegular() && fi.Mode().Perm() == 0600 && st.Nlink == 1
}
func sameSpoolFile(a, b os.FileInfo) bool {
	return regularSpoolFile(a) && regularSpoolFile(b) && os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime() == b.ModTime()
}
func openSpoolDir(path string) (*os.File, error) {
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openSpoolLeaf(dir *os.File, name string, flags int) (*os.File, error) {
	// An untrusted same-name FIFO must not wait for a peer before callers can
	// reject its descriptor type. This also covers the independent reader lock.
	if flags&unix.O_ACCMODE == unix.O_RDONLY {
		flags |= unix.O_NONBLOCK
	}
	fd, e := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), name), nil
}
func checkSpoolRoot(dir *os.File, chain []spoolAncestor) error {
	if e := checkSpoolAncestors(chain); e != nil {
		return e
	}
	fi, e := dir.Stat()
	if e != nil || !os.SameFile(fi, chain[0].info) || fi.Mode().Perm() != 0700 {
		return errors.New("durable spool root changed")
	}
	return nil
}
func readSpoolLeaf(ctx context.Context, dir *os.File, name string, limit int64, want *spoolLeaf) ([]byte, spoolLeaf, error) {
	fail := func() ([]byte, spoolLeaf, error) {
		return nil, spoolLeaf{}, errors.New("durable spool leaf changed or invalid")
	}
	if e := ctx.Err(); e != nil {
		return nil, spoolLeaf{}, e
	}
	f, e := openSpoolLeaf(dir, name, unix.O_RDONLY)
	if e != nil {
		return fail()
	}
	before, e := f.Stat()
	if e != nil || !regularSpoolFile(before) || before.Size() < 0 || before.Size() > limit {
		_ = f.Close()
		return fail()
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	after, se := f.Stat()
	ce := f.Close()
	var pathStat unix.Stat_t
	pe := unix.Fstatat(int(dir.Fd()), name, &pathStat, unix.AT_SYMLINK_NOFOLLOW)
	sys := before.Sys().(*syscall.Stat_t)
	if e != nil || se != nil || ce != nil || pe != nil || uint64(pathStat.Dev) != uint64(sys.Dev) || pathStat.Ino != sys.Ino || pathStat.Nlink != 1 || !sameSpoolFile(before, after) || int64(len(raw)) != before.Size() {
		return fail()
	}
	got := spoolLeaf{info: after, hash: spoolDigest(raw), size: int64(len(raw))}
	if want != nil && (!sameSpoolFile(want.info, after) || want.hash != got.hash || want.size != got.size) {
		return fail()
	}
	if e = ctx.Err(); e != nil {
		return nil, spoolLeaf{}, e
	}
	return raw, got, nil
}
func spoolNames(dir *os.File) ([]string, error) {
	// A separate open file description avoids mutating the owned directory offset.
	f, e := openSpoolLeaf(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if e != nil {
		return nil, e
	}
	names, e := f.Readdirnames(-1)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return names, e
}
func checkSpoolNames(dir *os.File, expected map[string]spoolLeaf) error {
	names, e := spoolNames(dir)
	if e != nil {
		return e
	}
	if len(names) != len(expected) {
		return errors.New("durable spool foreign or missing leaf")
	}
	for _, name := range names {
		if _, ok := expected[name]; !ok {
			return errors.New("durable spool foreign leaf")
		}
	}
	return nil
}
func spoolBodyName(index uint64) string   { return fmt.Sprintf("%020d.pb", index) }
func spoolCommitName(index uint64) string { return fmt.Sprintf("%020d.commit", index) }
func strictSpoolJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("trailing spool JSON")
	}
	canonical, e := json.Marshal(out)
	if e != nil || !bytes.Equal(raw, canonical) {
		return errors.New("noncanonical spool JSON")
	}
	return nil
}
func spoolSpanInventory(raw []byte) ([]DurableSpanIdentity, error) {
	if len(raw) == 0 || len(raw) > durableBatchLimit {
		return nil, errors.New("durable batch size limit")
	}
	var request collector.ExportTraceServiceRequest
	if e := proto.Unmarshal(raw, &request); e != nil {
		return nil, errors.New("malformed durable protobuf")
	}
	canonical, e := proto.MarshalOptions{Deterministic: true}.Marshal(&request)
	if e != nil || !bytes.Equal(canonical, raw) {
		return nil, errors.New("noncanonical durable protobuf")
	}
	var out []DurableSpanIdentity
	seen := map[string]bool{}
	for _, r := range request.ResourceSpans {
		if r == nil {
			return nil, errors.New("nil durable resource")
		}
		for _, scope := range r.ScopeSpans {
			if scope == nil {
				return nil, errors.New("nil durable scope")
			}
			for _, s := range scope.Spans {
				if s == nil || len(s.TraceId) != 16 || len(s.SpanId) != 8 || bytes.Equal(s.TraceId, make([]byte, 16)) || bytes.Equal(s.SpanId, make([]byte, 8)) || (len(s.ParentSpanId) != 0 && (len(s.ParentSpanId) != 8 || bytes.Equal(s.ParentSpanId, make([]byte, 8)))) || s.StartTimeUnixNano == 0 || s.EndTimeUnixNano < s.StartTimeUnixNano {
					return nil, errors.New("invalid durable span identity")
				}
				identity := DurableSpanIdentity{hex.EncodeToString(s.TraceId), hex.EncodeToString(s.SpanId), hex.EncodeToString(s.ParentSpanId), s.StartTimeUnixNano, s.EndTimeUnixNano}
				key := identity.TraceID + identity.SpanID
				if seen[key] {
					return nil, errors.New("duplicate durable span identity")
				}
				seen[key] = true
				out = append(out, identity)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty durable batch")
	}
	return out, nil
}
func spoolSafeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("durable spool operation failed")
}
func validSpoolName(name string) bool {
	return !strings.ContainsAny(name, "/\\") && name != "." && name != ".."
}
