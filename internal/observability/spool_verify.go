package observability

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// VerifiedDurableSpool is a process-local, sealed DATA proof. Only a full
// independent closed-root replay creates one. JSON, Summary, and a live writer
// status cannot create it. It grants no inference, shipment, or score authority.
type VerifiedDurableSpool struct {
	sealed    bool
	cfg       DurableSpoolConfig
	ancestors []spoolAncestor
	leaves    map[string]spoolLeaf
	summary   DurableSpoolStatus
	spans     []DurableSpanIdentity
}

func (v *VerifiedDurableSpool) Summary() DurableSpoolStatus {
	if v == nil || !v.sealed {
		return DurableSpoolStatus{DataOnly: true, DurabilityReturnUnproven: true, OwnerCompletionUnproven: true}
	}
	return v.summary
}
func (v *VerifiedDurableSpool) SpanIdentities() []DurableSpanIdentity {
	if v == nil || !v.sealed {
		return nil
	}
	return append([]DurableSpanIdentity(nil), v.spans...)
}
func openClosedSpool(ctx context.Context, c DurableSpoolConfig, chain []spoolAncestor) (*os.File, *os.File, error) {
	fail := func() (*os.File, *os.File, error) {
		return nil, nil, errors.New("closed durable spool identity or lock invalid")
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if checkSpoolAncestors(chain) != nil {
		return fail()
	}
	root, e := openSpoolDir(c.Root)
	if e != nil {
		return fail()
	}
	if checkSpoolRoot(root, chain) != nil {
		_ = root.Close()
		return fail()
	}
	lock, e := openSpoolLeaf(root, "lock", unix.O_RDONLY)
	if e != nil {
		_ = root.Close()
		return fail()
	}
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_SH|unix.LOCK_NB); e != nil {
		_ = lock.Close()
		_ = root.Close()
		return fail()
	}
	return root, lock, nil
}
func closeSpoolReader(root, lock *os.File) error {
	a := lock.Close()
	b := root.Close()
	if a != nil || b != nil {
		return errors.New("closed durable spool descriptor close failed")
	}
	return nil
}
func (v *VerifiedDurableSpool) ReadBatch(ctx context.Context, index uint64) (raw []byte, err error) {
	if v == nil || !v.sealed || index == 0 || index > v.summary.Batches {
		return nil, errors.New("invalid sealed durable spool proof or batch index")
	}
	root, lock, e := openClosedSpool(ctx, v.cfg, v.ancestors)
	if e != nil {
		return nil, e
	}
	defer func() {
		if e := closeSpoolReader(root, lock); err == nil && e != nil {
			raw = nil
			err = e
		}
	}()
	if e = checkSpoolNames(root, v.leaves); e != nil {
		return nil, e
	}
	for _, name := range []string{"lock", "header.json", "closed.json", spoolCommitName(index)} {
		want := v.leaves[name]
		if _, _, e = readSpoolLeaf(ctx, root, name, 16*durableBatchLimit, &want); e != nil {
			return nil, e
		}
	}
	want := v.leaves[spoolBodyName(index)]
	raw, _, err = readSpoolLeaf(ctx, root, spoolBodyName(index), durableBatchLimit, &want)
	if err == nil {
		err = checkSpoolRoot(root, v.ancestors)
	}
	if err == nil {
		err = checkSpoolNames(root, v.leaves)
	}
	if err != nil {
		return nil, err
	}
	return raw, ctx.Err()
}

// VerifyClosedDurableSpool can run in a different process after the writer
// releases its lock. A fully consistent candidate close marker cannot prove a
// historical fsync/close succeeded: callers separately require genuine normal
// owner termination before shipment. Crash/open/orphan/foreign data is rejected.
func VerifyClosedDurableSpool(ctx context.Context, c DurableSpoolConfig) (proof *VerifiedDurableSpool, err error) {
	if e := c.validate(); e != nil {
		return nil, e
	}
	chain, e := spoolAncestors(c.Root)
	if e != nil {
		return nil, e
	}
	root, lock, e := openClosedSpool(ctx, c, chain)
	if e != nil {
		return nil, e
	}
	defer func() {
		if e := closeSpoolReader(root, lock); err == nil && e != nil {
			proof = nil
			err = e
		}
	}()
	v := &VerifiedDurableSpool{cfg: c, ancestors: chain, leaves: map[string]spoolLeaf{}, summary: DurableSpoolStatus{RunID: c.RunID, RootSHA256: spoolDigest([]byte(c.Root)), MaxBytes: c.MaxBytes, Closed: true, DataOnly: true, DurabilityReturnUnproven: true, OwnerCompletionUnproven: true}}
	read := func(name string, limit int64) ([]byte, error) {
		raw, leaf, e := readSpoolLeaf(ctx, root, name, limit, nil)
		if e != nil {
			return nil, e
		}
		if leaf.size > c.MaxBytes-v.summary.StoredBytes {
			return nil, errors.New("closed spool exceeds capacity")
		}
		v.summary.StoredBytes += leaf.size
		v.leaves[name] = leaf
		return raw, nil
	}
	raw, e := read("lock", 64)
	if e != nil || string(raw) != c.RunID {
		return nil, errors.New("closed spool lock binding invalid")
	}
	lockInfo, e := lock.Stat()
	if e != nil || !sameSpoolFile(lockInfo, v.leaves["lock"].info) {
		return nil, errors.New("closed spool lock replaced")
	}
	raw, e = read("header.json", 4096)
	if e != nil {
		return nil, e
	}
	var header spoolHeader
	if strictSpoolJSON(raw, &header) != nil || header != (spoolHeader{"durable-otel.v1", c.RunID, v.summary.RootSHA256, c.MaxBytes}) {
		return nil, errors.New("closed spool header mismatch")
	}
	raw, e = read("closed.json", 4096)
	if e != nil {
		return nil, e
	}
	var closed spoolClose
	if strictSpoolJSON(raw, &closed) != nil || closed.Version != "durable-otel.closed.v1" || closed.RunID != c.RunID || closed.RootSHA256 != v.summary.RootSHA256 {
		return nil, errors.New("closed spool marker mismatch")
	}
	names, e := spoolNames(root)
	if e != nil {
		return nil, e
	}
	if closed.Batches > uint64(c.MaxBytes) || closed.Batches > uint64(len(names)) || uint64(len(names)) != 3+2*closed.Batches {
		return nil, errors.New("closed spool incomplete or foreign inventory")
	}
	seenIDs := map[string]bool{}
	seenBodies := map[string]bool{}
	for i := uint64(1); i <= closed.Batches; i++ {
		record, e := read(spoolCommitName(i), 16*durableBatchLimit)
		if e != nil {
			return nil, e
		}
		var commit spoolCommit
		if strictSpoolJSON(record, &commit) != nil || commit.Sequence != i || commit.Previous != v.summary.LastLinkSHA256 || commit.BodyBytes <= 0 || commit.BodyBytes > durableBatchLimit {
			return nil, errors.New("closed spool commit linkage invalid")
		}
		body, e := read(spoolBodyName(i), durableBatchLimit)
		if e != nil {
			return nil, e
		}
		if int64(len(body)) != commit.BodyBytes || spoolDigest(body) != commit.BodySHA256 || seenBodies[commit.BodySHA256] {
			return nil, errors.New("closed spool body mismatch")
		}
		ids, e := spoolSpanInventory(body)
		if e != nil {
			return nil, e
		}
		expected, _ := json.Marshal(ids)
		actual, _ := json.Marshal(commit.Spans)
		if string(expected) != string(actual) {
			return nil, errors.New("closed spool span inventory mismatch")
		}
		for _, id := range ids {
			key := id.TraceID + id.SpanID
			if seenIDs[key] {
				return nil, errors.New("closed spool conflicting span identity")
			}
			seenIDs[key] = true
		}
		seenBodies[commit.BodySHA256] = true
		v.spans = append(v.spans, ids...)
		v.summary.Batches = i
		v.summary.Spans += uint64(len(ids))
		v.summary.Bytes += int64(len(body))
		v.summary.LastLinkSHA256 = spoolDigest(record)
	}
	if v.summary.Batches != closed.Batches || v.summary.Spans != closed.Spans || v.summary.Bytes != closed.Bytes || v.summary.LastLinkSHA256 != closed.LastLinkSHA256 {
		return nil, errors.New("closed spool totals mismatch")
	}
	// A second physical/content pass rejects changes during the independent read.
	for name, want := range v.leaves {
		if _, _, e = readSpoolLeaf(ctx, root, name, 16*durableBatchLimit, &want); e != nil {
			return nil, e
		}
	}
	if e = checkSpoolNames(root, v.leaves); e != nil {
		return nil, e
	}
	if e = checkSpoolRoot(root, chain); e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	v.sealed = true
	return v, nil
}
