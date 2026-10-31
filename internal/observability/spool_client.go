package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type durableSpoolClient struct {
	mu               sync.Mutex
	cfg              DurableSpoolConfig
	io               spoolIO
	root             *os.File
	lock             *os.File
	ancestors        []spoolAncestor
	leaves           map[string]spoolLeaf
	bodies           map[string]uint64
	identities       map[string]bool
	counts           DurableSpoolStatus
	storedBytes      int64
	started, stopped bool
	poison           error
}

var _ otlptrace.Client = (*durableSpoolClient)(nil)

func newDurableSpoolClient(c DurableSpoolConfig) *durableSpoolClient {
	return &durableSpoolClient{cfg: c, io: defaultSpoolIO(), leaves: map[string]spoolLeaf{}, bodies: map[string]uint64{}, identities: map[string]bool{}, counts: DurableSpoolStatus{RunID: c.RunID, RootSHA256: spoolDigest([]byte(c.Root)), MaxBytes: c.MaxBytes}}
}
func (s *durableSpoolClient) fail(err error) error {
	if s.poison == nil {
		s.poison = spoolSafeError(err)
	}
	s.counts.Poisoned = true
	return s.poison
}
func (s *durableSpoolClient) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped || s.poison != nil {
		return errors.New("durable spool already started or failed")
	}
	if e := s.cfg.validate(); e != nil {
		return s.fail(e)
	}
	if int64(len(s.cfg.RunID)) > s.cfg.MaxBytes {
		return s.fail(errors.New("durable spool control capacity exceeded"))
	}
	if e := ctx.Err(); e != nil {
		return s.fail(e)
	}
	parent := filepath.Dir(s.cfg.Root)
	chain, e := spoolAncestors(parent)
	if e != nil {
		return s.fail(e)
	}
	// Verify the parent before and after mkdir. Openat roots all subsequent writes
	// at the owned directory even if a path is maliciously replaced concurrently.
	if e = os.Mkdir(s.cfg.Root, 0700); e != nil {
		return s.fail(e)
	}
	if e = checkSpoolAncestors(chain); e != nil {
		return s.fail(e)
	}
	s.ancestors, e = spoolAncestors(s.cfg.Root)
	if e != nil {
		return s.fail(e)
	}
	s.root, e = openSpoolDir(s.cfg.Root)
	if e != nil {
		return s.fail(e)
	}
	cleanup := func(err error) error {
		if s.lock != nil {
			_ = s.lock.Close()
			s.lock = nil
		}
		_ = s.root.Close()
		s.root = nil
		return s.fail(err)
	}
	if e = checkSpoolRoot(s.root, s.ancestors); e != nil {
		return cleanup(e)
	}
	s.lock, e = openSpoolLeaf(s.root, "lock", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
	if e != nil {
		return cleanup(e)
	}
	if e = unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return cleanup(e)
	}
	// Keep the lock descriptor alive for the whole writer lifetime. The leaf is
	// immutable and never removed or repurposed as an authorization token.
	lockRaw := []byte(s.cfg.RunID)
	if n, we := s.io.write(s.lock, lockRaw); we != nil || n != len(lockRaw) {
		return cleanup(io.ErrShortWrite)
	}
	if e = s.io.sync(s.lock); e != nil {
		return cleanup(e)
	}
	_, leaf, e := readSpoolLeaf(ctx, s.root, "lock", 64, nil)
	if e != nil {
		return cleanup(e)
	}
	s.leaves["lock"] = leaf
	s.storedBytes += leaf.size
	header, _ := json.Marshal(spoolHeader{"durable-otel.v1", s.cfg.RunID, s.counts.RootSHA256, s.cfg.MaxBytes})
	if e = s.writeLeaf(ctx, "header.json", header); e != nil {
		return cleanup(e)
	}
	p, e := openSpoolDir(parent)
	if e != nil {
		return cleanup(e)
	}
	se := s.io.sync(p)
	ce := s.io.close(p)
	if se != nil {
		return cleanup(se)
	}
	if ce != nil {
		return cleanup(ce)
	}
	if e = s.check(ctx, false); e != nil {
		return cleanup(e)
	}
	s.started = true
	return nil
}
func (s *durableSpoolClient) writeLeaf(ctx context.Context, name string, raw []byte) error {
	if !validSpoolName(name) || int64(len(raw)) > s.cfg.MaxBytes-s.storedBytes {
		return errors.New("durable spool capacity exceeded")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	f, e := openSpoolLeaf(s.root, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if e != nil {
		return e
	}
	n, we := s.io.write(f, raw)
	if we == nil && n != len(raw) {
		we = io.ErrShortWrite
	}
	if we == nil {
		we = s.io.sync(f)
	}
	ce := s.io.close(f)
	if we != nil {
		return we
	}
	if ce != nil {
		return ce
	}
	if e = s.io.sync(s.root); e != nil {
		return e
	}
	got, leaf, e := readSpoolLeaf(ctx, s.root, name, int64(len(raw)), nil)
	if e != nil {
		return e
	}
	if !bytes.Equal(got, raw) {
		return errors.New("durable spool write verification failed")
	}
	if e = checkSpoolRoot(s.root, s.ancestors); e != nil {
		return e
	}
	s.leaves[name] = leaf
	s.storedBytes += leaf.size
	return ctx.Err()
}
func (s *durableSpoolClient) check(ctx context.Context, last bool) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.root == nil || s.lock == nil {
		return errors.New("durable spool not owned")
	}
	if e := checkSpoolRoot(s.root, s.ancestors); e != nil {
		return e
	}
	if e := checkSpoolNames(s.root, s.leaves); e != nil {
		return e
	}
	for _, name := range []string{"lock", "header.json"} {
		want, ok := s.leaves[name]
		if !ok {
			return errors.New("durable spool missing binding")
		}
		if _, _, e := readSpoolLeaf(ctx, s.root, name, 4096, &want); e != nil {
			return e
		}
	}
	fi, e := s.lock.Stat()
	if e != nil || !sameSpoolFile(fi, s.leaves["lock"].info) {
		return errors.New("durable spool lock changed")
	}
	if last && s.counts.Batches > 0 {
		for _, name := range []string{spoolBodyName(s.counts.Batches), spoolCommitName(s.counts.Batches)} {
			want := s.leaves[name]
			if _, _, e := readSpoolLeaf(ctx, s.root, name, 4*durableBatchLimit, &want); e != nil {
				return e
			}
		}
	}
	return ctx.Err()
}
func (s *durableSpoolClient) UploadTraces(ctx context.Context, resources []*tracepb.ResourceSpans) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison != nil {
		return s.poison
	}
	if !s.started || s.stopped {
		return errors.New("durable spool not open")
	}
	if e := s.check(ctx, true); e != nil {
		return s.fail(e)
	}
	request := &collector.ExportTraceServiceRequest{ResourceSpans: resources}
	if proto.Size(request) > durableBatchLimit {
		return s.fail(errors.New("durable batch size limit"))
	}
	raw, e := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if e != nil {
		return s.fail(e)
	}
	ids, e := spoolSpanInventory(raw)
	if e != nil {
		return s.fail(e)
	}
	digest := spoolDigest(raw)
	if index, ok := s.bodies[digest]; ok {
		name := spoolBodyName(index)
		want := s.leaves[name]
		original, _, e := readSpoolLeaf(ctx, s.root, name, durableBatchLimit, &want)
		if e != nil {
			return s.fail(e)
		}
		if !bytes.Equal(original, raw) {
			return s.fail(errors.New("durable duplicate mismatch"))
		}
		// The referenced original can be older than the latest-entry check.
		commitName := spoolCommitName(index)
		commitWant := s.leaves[commitName]
		if _, _, e = readSpoolLeaf(ctx, s.root, commitName, 16*durableBatchLimit, &commitWant); e != nil {
			return s.fail(e)
		}
		if e = s.check(ctx, true); e != nil {
			return s.fail(e)
		}
		return nil
	}
	for _, id := range ids {
		if s.identities[id.TraceID+id.SpanID] {
			return s.fail(errors.New("conflicting durable span identity"))
		}
	}
	index := s.counts.Batches + 1
	commit := spoolCommit{index, s.counts.LastLinkSHA256, digest, int64(len(raw)), ids}
	record, _ := json.Marshal(commit)
	// Reserve the close record before touching a new batch. This avoids a full
	// spool that cannot even record an orderly close within its declared cap.
	if int64(len(raw)+len(record))+1024 > s.cfg.MaxBytes-s.storedBytes {
		return s.fail(errors.New("durable spool capacity exceeded"))
	}
	if e = s.writeLeaf(ctx, spoolBodyName(index), raw); e != nil {
		return s.fail(e)
	}
	if e = s.writeLeaf(ctx, spoolCommitName(index), record); e != nil {
		return s.fail(e)
	}
	for _, name := range []string{spoolBodyName(index), spoolCommitName(index)} {
		want := s.leaves[name]
		if _, _, e = readSpoolLeaf(ctx, s.root, name, 16*durableBatchLimit, &want); e != nil {
			return s.fail(e)
		}
	}
	if e = s.check(ctx, false); e != nil {
		return s.fail(e)
	}
	s.counts.Batches = index
	s.counts.Spans += uint64(len(ids))
	s.counts.Bytes += int64(len(raw))
	s.counts.LastLinkSHA256 = spoolDigest(record)
	s.bodies[digest] = index
	for _, id := range ids {
		s.identities[id.TraceID+id.SpanID] = true
	}
	return nil
}
func (s *durableSpoolClient) status() (DurableSpoolStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison == nil && !s.stopped {
		if e := s.check(context.Background(), true); e != nil {
			s.fail(e)
		}
	}
	out := s.counts
	out.StoredBytes = s.storedBytes
	if !s.started && s.poison == nil {
		return out, errors.New("durable spool not started")
	}
	return out, s.poison
}
func (s *durableSpoolClient) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return s.poison
	}
	s.stopped = true
	if !s.started {
		if s.poison != nil {
			return s.poison
		}
		return s.fail(errors.New("durable spool not started"))
	}
	if s.poison == nil {
		if e := s.check(ctx, true); e != nil {
			s.fail(e)
		}
	}
	if s.poison == nil {
		closeRaw, _ := json.Marshal(spoolClose{"durable-otel.closed.v1", s.cfg.RunID, s.counts.RootSHA256, s.counts.Batches, s.counts.Spans, s.counts.Bytes, s.counts.LastLinkSHA256})
		if e := s.writeLeaf(ctx, "closed.json", closeRaw); e != nil {
			s.fail(e)
		}
		if s.poison == nil {
			if e := s.check(ctx, true); e != nil {
				s.fail(e)
			}
		}
	}
	// Check every close result, even after an earlier fault. A readable candidate
	// marker is data only and cannot certify these calls returned successfully.
	if s.lock != nil {
		if e := s.io.close(s.lock); e != nil {
			s.fail(e)
		}
		s.lock = nil
	}
	if s.root != nil {
		if e := s.io.close(s.root); e != nil {
			s.fail(e)
		}
		s.root = nil
	}
	if e := ctx.Err(); e != nil {
		s.fail(e)
	}
	if s.poison == nil {
		s.counts.Closed = true
	}
	return s.poison
}

func (o *Observer) DurableStatus() (DurableSpoolStatus, error) {
	if o == nil || o.durable == nil || o.processor == nil || o.processor.durable != o.durable || o.processor.exporter != o.durableExporter {
		return DurableSpoolStatus{}, errors.New("observer has no genuine durable exporter")
	}
	return o.durable.status()
}
func (o *Observer) CheckDurableReceipt(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	status, e := o.DurableStatus()
	if e != nil {
		return e
	}
	if status.Closed || status.Poisoned {
		return errors.New("durable observer is not live")
	}
	counts := o.Stats()
	if counts.Accepted != counts.Persisted || counts.Exported != 0 || counts.Failed != 0 || counts.Dropped != 0 || counts.QueueDepth != 0 || counts.Persisted != status.Spans {
		return errors.New("incomplete durable observation receipt")
	}
	return ctx.Err()
}
