package taskbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	historyPassCacheLimit      uint64 = 128 << 20
	historyPassEntryLimit      int64  = 1 << 20
	historyPassTranscriptLimit uint64 = 32 << 20
	historyPassRecordsLimit    uint64 = 65536
	historyPassLogicalLimit    uint64 = 1048576
	historyPassActiveLimit            = 64
)

type historyReadKind uint8

const (
	historyReadFile historyReadKind = iota
	historyReadBytes
	historyReadV7File
	historyReadInventoryStream
	historyReadExecutableStream
)

type historyReadSpec struct {
	kind      historyReadKind
	path, sha string
	max       int64
}

func (s historyReadSpec) isStream() bool {
	return s.kind == historyReadInventoryStream || s.kind == historyReadExecutableStream
}
func (s historyReadSpec) key() string {
	return strconv.Itoa(int(s.kind)) + "\x00" + s.path + "\x00" + s.sha + "\x00" + strconv.FormatInt(s.max, 10)
}

type historyReadStats struct{ Logical, FirstPhysical, EndPhysical, CacheHits, CacheBytes, TranscriptBytes, Records, LocksClosed uint64 }
type historyPassState uint8

const (
	historyPassOpen historyPassState = iota
	historyPassDraining
	historyPassSweeping
	historyPassClosed
	historyPassFailed
)

type historyReadEntry struct {
	spec     historyReadSpec
	ready    chan struct{}
	raw      []byte
	digest   string
	identity historyFileIdentity
	cached   bool
}
type historyDirEntry struct {
	identity historyFileIdentity
	strict   bool
}
type historyNamespace struct{ names map[string]bool }
type historyLockEntry struct {
	epoch    int
	identity historyFileIdentity
}

// No pass or raw cache is stored in a baseline, ready object or campaign.
// Only explicitly threaded historical leaf operations may use this primitive.
type historyReadPass struct {
	finishing  bool
	finishDone chan struct{}
	ctx        context.Context
	mu         sync.Mutex
	cond       *sync.Cond
	state      historyPassState
	active     int
	leaves     chan struct{}
	failed     chan struct{}
	failure    error
	entries    map[string]*historyReadEntry
	paths      map[string]*historyReadEntry
	dirs       map[string]historyDirEntry
	roots      map[string]bool
	namespaces map[string]historyNamespace
	locks      map[string]historyLockEntry
	stats      historyReadStats
}

func newHistoryReadPass(ctx context.Context) (*historyReadPass, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, fmt.Errorf("history context rejected")
	}
	p := &historyReadPass{ctx: ctx, leaves: make(chan struct{}, 4), failed: make(chan struct{}), entries: map[string]*historyReadEntry{}, paths: map[string]*historyReadEntry{}, dirs: map[string]historyDirEntry{}, roots: map[string]bool{}, namespaces: map[string]historyNamespace{}, locks: map[string]historyLockEntry{}}
	p.finishDone = make(chan struct{})
	p.cond = sync.NewCond(&p.mu)
	return p, nil
}
func historyBudgetFits(used, add, limit uint64) bool { return used <= limit && add <= limit-used }
func (p *historyReadPass) failLocked(err error) error {
	if p.state != historyPassFailed {
		p.state = historyPassFailed
		p.failure = err
		close(p.failed)
	}
	if p.failure == nil {
		p.failure = fmt.Errorf("history pass failed")
	}
	return p.failure
}
func (p *historyReadPass) fail(err error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failLocked(err)
}
func (p *historyReadPass) begin(logical bool) error {
	if err := p.ctx.Err(); err != nil {
		return p.fail(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != historyPassOpen {
		if p.state == historyPassDraining || p.state == historyPassSweeping {
			return p.failLocked(fmt.Errorf("history read during closure"))
		}
		return fmt.Errorf("history pass unavailable")
	}
	if p.active >= historyPassActiveLimit || logical && p.stats.Logical >= historyPassLogicalLimit {
		return p.failLocked(fmt.Errorf("history logical admission limit"))
	}
	if logical {
		p.stats.Logical++
	}
	p.active++
	return nil
}
func (p *historyReadPass) done() { p.mu.Lock(); p.active--; p.cond.Broadcast(); p.mu.Unlock() }
func (p *historyReadPass) leaf(fn func() error) error {
	select {
	case p.leaves <- struct{}{}:
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.failed:
		return fmt.Errorf("history pass failed")
	}
	defer func() { <-p.leaves }()
	select {
	case <-p.failed:
		return fmt.Errorf("history pass failed")
	default:
	}
	return fn()
}
func (p *historyReadPass) physical(s historyReadSpec, retain, end bool) (historyObservedFile, error) {
	select {
	case windowStoppedReadSlots <- struct{}{}:
	case <-p.ctx.Done():
		return historyObservedFile{}, p.ctx.Err()
	case <-p.failed:
		return historyObservedFile{}, fmt.Errorf("history pass failed")
	}
	defer func() { <-windowStoppedReadSlots }()
	select {
	case <-p.failed:
		return historyObservedFile{}, fmt.Errorf("history pass failed")
	default:
	}
	p.mu.Lock()
	if end {
		p.stats.EndPhysical++
	} else {
		p.stats.FirstPhysical++
	}
	p.mu.Unlock()
	return observeHistoryFile(p.ctx, s, retain)
}
func (p *historyReadPass) chargeLocked(key, path, sha string, extra uint64) error {
	charge := uint64(512 + len(key) + len(path) + len(sha))
	if !historyBudgetFits(charge, extra, historyPassTranscriptLimit) || !historyBudgetFits(p.stats.TranscriptBytes, charge+extra, historyPassTranscriptLimit) || p.stats.Records >= historyPassRecordsLimit {
		return p.failLocked(fmt.Errorf("history transcript limit"))
	}
	p.stats.TranscriptBytes += charge + extra
	p.stats.Records++
	return nil
}
func historyInside(path, root string) bool {
	return path == root || root == string(filepath.Separator) && filepath.IsAbs(path) || strings.HasPrefix(path, root+string(filepath.Separator))
}
func (p *historyReadPass) strictLocked(path string) bool {
	for root := range p.roots {
		if historyInside(path, root) {
			return true
		}
	}
	return false
}
func (p *historyReadPass) observeDirs(path string) error {
	var chain []string
	for current := path; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		if filepath.Dir(current) == current {
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		dir := chain[i]
		id, err := historyPathIdentity(dir, true)
		if err != nil {
			return err
		}
		p.mu.Lock()
		strict := p.strictLocked(dir)
		old, ok := p.dirs[dir]
		if ok {
			if !historyDirectoryEqual(old.identity, id, old.strict || strict) {
				err = p.failLocked(fmt.Errorf("history ancestor changed"))
			}
		} else if err = p.chargeLocked("dir\x00"+dir, dir, "", 0); err == nil {
			p.dirs[dir] = historyDirEntry{id, strict}
		}
		p.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func (p *historyReadPass) Read(s historyReadSpec) (raw []byte, err error) {
	if s.isStream() {
		return nil, p.fail(fmt.Errorf("history stream requires Stream"))
	}
	if err = s.validate(); err != nil {
		return nil, p.fail(err)
	}
	if err = p.begin(true); err != nil {
		return nil, err
	}
	defer p.done()
	err = p.leaf(func() error { var e error; raw, _, e = p.readLeaf(s); return e })
	if err != nil {
		return nil, p.fail(err)
	}
	return raw, nil
}
func (p *historyReadPass) Stream(kind historyReadKind, path, sha string, max int64) (digest string, err error) {
	if kind != historyReadInventoryStream && kind != historyReadExecutableStream {
		return "", p.fail(fmt.Errorf("history stream kind"))
	}
	spec := historyReadSpec{kind, path, sha, max}
	if err = spec.validate(); err != nil {
		return "", p.fail(err)
	}
	if err = p.begin(true); err != nil {
		return "", err
	}
	defer p.done()
	err = p.leaf(func() error {
		var e error
		_, digest, e = p.readLeaf(spec)
		return e
	})
	if err != nil {
		return "", p.fail(err)
	}
	return digest, nil
}
func (p *historyReadPass) readLeaf(s historyReadSpec) (raw []byte, digest string, err error) {
	if err = s.validate(); err != nil {
		return nil, "", err
	}
	key := s.key()
	p.mu.Lock()
	entry, found := p.entries[key]
	if !found {
		if err = p.chargeLocked(key, s.path, s.sha, 0); err == nil {
			entry = &historyReadEntry{spec: s, ready: make(chan struct{})}
			p.entries[key] = entry
		}
	}
	p.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	if found {
		select {
		case <-entry.ready:
		case <-p.ctx.Done():
			return nil, "", p.ctx.Err()
		case <-p.failed:
			return nil, "", fmt.Errorf("history pass failed")
		}
		p.mu.Lock()
		bad := p.state == historyPassFailed
		cached := entry.cached
		stored := entry.raw
		digest = entry.digest
		p.mu.Unlock()
		if bad {
			return nil, "", fmt.Errorf("history pass failed")
		}
		if cached {
			raw = bytes.Clone(stored)
			p.mu.Lock()
			p.stats.CacheHits++
			p.mu.Unlock()
			if p.ctx.Err() != nil {
				return nil, "", p.ctx.Err()
			}
			return raw, digest, nil
		}
	} else {
		defer func() {
			if err != nil {
				p.fail(err)
			}
			close(entry.ready)
		}()
	}
	if err = p.observeDirs(filepath.Dir(s.path)); err != nil {
		return nil, "", err
	}
	observed, err := p.physical(s, !s.isStream(), false)
	if err != nil {
		return nil, "", err
	}
	if err = p.observeDirs(filepath.Dir(s.path)); err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	if previous := p.paths[s.path]; previous != nil && (previous.identity != observed.identity || previous.digest != observed.digest) {
		err = p.failLocked(fmt.Errorf("history dependency changed between references"))
	}
	if p.state == historyPassFailed {
		err = fmt.Errorf("history pass failed")
	}
	cache := !s.isStream() && observed.identity.Size <= historyPassEntryLimit
	if !found && err == nil {
		if cache && !historyBudgetFits(p.stats.CacheBytes, uint64(len(observed.raw)), historyPassCacheLimit) {
			err = p.failLocked(fmt.Errorf("history retained byte limit"))
		}
		if err == nil {
			entry.identity, entry.digest = observed.identity, observed.digest
			p.paths[s.path] = entry
			if cache {
				p.stats.CacheBytes += uint64(len(observed.raw))
			}
		}
	}
	p.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	if !found && cache {
		stored := bytes.Clone(observed.raw)
		p.mu.Lock()
		entry.raw, entry.cached = stored, true
		p.mu.Unlock()
		raw = bytes.Clone(stored)
	} else {
		raw = observed.raw
	}
	if p.ctx.Err() != nil {
		return nil, "", p.ctx.Err()
	}
	return raw, observed.digest, nil
}

// Batch never owns the legacy outer physical slots. Each Read owns its own
// logical leaf and acquires the process-wide physical slot only for actual IO.
func (p *historyReadPass) Batch(specs []historyReadSpec) ([][]byte, error) {
	if len(specs) < 1 || len(specs) > 4 {
		return nil, p.fail(fmt.Errorf("history batch size"))
	}
	if err := p.begin(false); err != nil {
		return nil, err
	}
	defer p.done()
	out := make([][]byte, len(specs))
	errs := make([]error, len(specs))
	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); out[i], errs[i] = p.Read(specs[i]) }(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (p *historyReadPass) MarkImmutableRoot(root string) (err error) {
	if err = p.begin(true); err != nil {
		return err
	}
	defer p.done()
	err = p.leaf(func() error {
		p.mu.Lock()
		registered := p.roots[root]
		p.mu.Unlock()
		if registered {
			// Classification is idempotent. Keep the original marker; original
			// namespace callbacks and the final fresh sweep still run separately.
			return nil
		}
		if e := p.observeDirs(root); e != nil {
			return e
		}
		p.mu.Lock()
		p.roots[root] = true
		dirs := map[string]historyDirEntry{}
		for path, d := range p.dirs {
			if historyInside(path, root) {
				dirs[path] = d
			}
		}
		p.mu.Unlock()
		for path, d := range dirs {
			id, e := historyPathIdentity(path, true)
			if e != nil || !historyDirectoryEqual(d.identity, id, true) {
				return fmt.Errorf("history immutable root upgrade changed")
			}
			p.mu.Lock()
			old := p.dirs[path]
			old.strict = true
			p.dirs[path] = old
			p.mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return p.fail(err)
	}
	return nil
}

// A recovered panic (or Goexit) must not convert an unfinished semantic check
// into usable evidence. This defer seals failure without recovering the panic.
func (p *historyReadPass) semanticCheck(check func() error) (err error) {
	returned := false
	defer func() {
		if !returned {
			p.fail(fmt.Errorf("history semantic callback incomplete"))
		}
	}()
	err = check()
	returned = true
	return err
}

func (p *historyReadPass) CheckNamespace(path string, names map[string]bool, check func() error) (err error) {
	if check == nil {
		return p.fail(fmt.Errorf("history namespace callback"))
	}
	if err = p.begin(true); err != nil {
		return err
	}
	defer p.done()
	if err = p.leaf(func() error { return p.observeDirs(path) }); err != nil {
		return p.fail(err)
	}
	// Semantic callbacks may themselves read leaves and must not own a leaf slot.
	if err = p.semanticCheck(check); err != nil {
		return p.fail(err)
	}
	err = p.leaf(func() error {
		if e := validateWindowStoppedV2Dir(path, names); e != nil {
			return e
		}
		if e := p.observeDirs(path); e != nil {
			return e
		}
		cloned := map[string]bool{}
		extra := uint64(0)
		for name, dir := range names {
			if name == "" || name == "." || filepath.Base(name) != name {
				return fmt.Errorf("history namespace name")
			}
			if !historyBudgetFits(extra, uint64(24+len(name)), historyPassTranscriptLimit) {
				return fmt.Errorf("history namespace charge")
			}
			extra += uint64(24 + len(name))
			cloned[name] = dir
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if old, ok := p.namespaces[path]; ok {
			if !reflect.DeepEqual(old.names, cloned) {
				return fmt.Errorf("history namespace expectation changed")
			}
			return nil
		}
		if e := p.chargeLocked("namespace\x00"+path, path, "", extra); e != nil {
			return e
		}
		p.namespaces[path] = historyNamespace{cloned}
		return nil
	})
	if err != nil {
		return p.fail(err)
	}
	return nil
}
func (p *historyReadPass) WithStoppedLock(epoch int, path string, check func() error) (err error) {
	if check == nil || epoch < 1 || epoch > 8 {
		return p.fail(fmt.Errorf("history lock specification"))
	}
	if err = p.begin(true); err != nil {
		return err
	}
	defer p.done()
	var lease *historyLockLease
	err = p.leaf(func() error {
		if e := p.observeDirs(filepath.Dir(path)); e != nil {
			return e
		}
		p.mu.Lock()
		for name, old := range p.locks {
			if old.epoch == epoch && name != path {
				p.mu.Unlock()
				return fmt.Errorf("history lock epoch collision")
			}
		}
		old, exists := p.locks[path]
		if exists && old.epoch != epoch {
			p.mu.Unlock()
			return fmt.Errorf("history lock order changed")
		}
		if !exists {
			if e := p.chargeLocked("lock\x00"+path, path, "", 0); e != nil {
				p.mu.Unlock()
				return e
			}
			p.locks[path] = historyLockEntry{epoch: epoch}
		}
		p.mu.Unlock()
		select {
		case windowStoppedReadSlots <- struct{}{}:
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-p.failed:
			return fmt.Errorf("history pass failed")
		}
		var e error
		lease, e = openHistoryLock(path)
		<-windowStoppedReadSlots
		if e != nil {
			return e
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if exists && old.identity != lease.identity {
			return fmt.Errorf("history lock changed")
		}
		p.locks[path] = historyLockEntry{epoch, lease.identity}
		return nil
	})
	if lease != nil {
		defer func() {
			err = errors.Join(err, lease.close())
			if err != nil {
				err = p.fail(err)
			}
		}()
	}
	if err != nil {
		return p.fail(err)
	}
	if err = p.semanticCheck(check); err != nil {
		return p.fail(err)
	}
	return nil
}
func (p *historyReadPass) Stats() historyReadStats { p.mu.Lock(); defer p.mu.Unlock(); return p.stats }
func (p *historyReadPass) clearLocked() {
	for _, e := range p.entries {
		e.raw = nil
	}
	p.entries = nil
	p.paths = nil
	p.dirs = nil
	p.roots = nil
	p.namespaces = nil
	p.locks = nil
}
func (p *historyReadPass) Abort() error {
	p.mu.Lock()
	if p.finishing {
		p.failLocked(fmt.Errorf("history closure aborted"))
		done := p.finishDone
		p.mu.Unlock()
		<-done
		return nil
	}
	if p.state == historyPassClosed {
		p.mu.Unlock()
		return fmt.Errorf("history pass already closed")
	}
	p.failLocked(fmt.Errorf("history pass aborted"))
	for p.active > 0 {
		p.cond.Wait()
	}
	p.clearLocked()
	p.mu.Unlock()
	return nil
}
func (p *historyReadPass) Finish() (err error) {
	p.mu.Lock()
	if p.finishing {
		p.failLocked(fmt.Errorf("history closure already active"))
		done := p.finishDone
		p.mu.Unlock()
		<-done
		return fmt.Errorf("history repeated finish")
	}
	if p.state != historyPassOpen {
		if p.state == historyPassFailed {
			for p.active > 0 {
				p.cond.Wait()
			}
			p.clearLocked()
		}
		p.mu.Unlock()
		return fmt.Errorf("history pass cannot finish")
	}
	p.finishing = true
	p.state = historyPassDraining
	p.mu.Unlock()
	var leases []*historyLockLease
	verified := false
	defer func() {
		cleanupCompleted := false
		// This finalizer also runs if the body or final context check panics.
		// Only a normally verified body AND normally finished cleanup may close.
		defer func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if !verified || !cleanupCompleted || err != nil || p.state == historyPassFailed {
				err = p.failLocked(fmt.Errorf("history closure failed"))
			} else {
				p.state = historyPassClosed
			}
			p.clearLocked()
			p.finishing = false
			close(p.finishDone)
			p.cond.Broadcast()
		}()
		for i := len(leases) - 1; i >= 0; i-- {
			err = errors.Join(err, leases[i].close())
			p.mu.Lock()
			p.stats.LocksClosed++
			p.mu.Unlock()
		}
		err = errors.Join(err, p.ctx.Err())
		cleanupCompleted = true
	}()
	p.mu.Lock()
	for p.active > 0 {
		p.cond.Wait()
	}
	failed := p.state == historyPassFailed
	if !failed {
		p.state = historyPassSweeping
	}
	p.mu.Unlock()
	if failed {
		return fmt.Errorf("history pass failed")
	}
	if p.ctx.Err() != nil {
		return p.ctx.Err()
	}
	paths := make([]string, 0, len(p.locks))
	for path := range p.locks {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return p.locks[paths[i]].epoch < p.locks[paths[j]].epoch })
	for _, path := range paths {
		select {
		case windowStoppedReadSlots <- struct{}{}:
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-p.failed:
			return fmt.Errorf("history pass failed")
		}
		lease, e := openHistoryLock(path)
		<-windowStoppedReadSlots
		if e != nil {
			return e
		}
		leases = append(leases, lease)
		if lease.identity != p.locks[path].identity {
			return fmt.Errorf("history End lock identity")
		}
	}
	checkDirs := func() error {
		dirPaths := make([]string, 0, len(p.dirs))
		for path := range p.dirs {
			dirPaths = append(dirPaths, path)
		}
		sort.Strings(dirPaths)
		for _, path := range dirPaths {
			d := p.dirs[path]
			id, e := historyPathIdentity(path, true)
			if e != nil || !historyDirectoryEqual(d.identity, id, d.strict) {
				return fmt.Errorf("history End ancestor changed")
			}
		}
		namespacePaths := make([]string, 0, len(p.namespaces))
		for path := range p.namespaces {
			namespacePaths = append(namespacePaths, path)
		}
		sort.Strings(namespacePaths)
		for _, path := range namespacePaths {
			n := p.namespaces[path]
			if e := validateWindowStoppedV2Dir(path, n.names); e != nil {
				return e
			}
		}
		return nil
	}
	if err = checkDirs(); err != nil {
		return err
	}
	keys := make([]string, 0, len(p.entries))
	for key := range p.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// All original per-reference physical checks remain fresh. Workers hold no
	// outer read slot: physical owns the existing process-wide four-slot bound.
	// Join every worker before inspecting errors or releasing historical locks.
	endResults := make([]historyEndResult, len(keys))
	var endWorkers sync.WaitGroup
	for worker := 0; worker < 4 && worker < len(keys); worker++ {
		endWorkers.Add(1)
		go func(first int) {
			current, returned := first, false
			defer func() {
				value := recover()
				if !returned {
					endResults[current].panicked = true
					endResults[current].panicValue = value
					endResults[current].goexit = value == nil
				}
				endWorkers.Done()
			}()
			for i := first; i < len(keys); i += 4 {
				current = i
				entry := p.entries[keys[i]]
				observed, e := p.physical(entry.spec, false, true)
				if e != nil || observed.identity != entry.identity || observed.digest != entry.digest {
					endResults[i].err = fmt.Errorf("history End dependency changed")
					returned = true
					return
				}
			}
			returned = true
		}(worker)
	}
	endWorkers.Wait()
	if e := checkHistoryEndResults(endResults); e != nil {
		return e
	}

	for _, key := range keys {
		entry := p.entries[key]
		id, e := historyPathIdentity(entry.spec.path, false)
		if e != nil || id != entry.identity {
			return fmt.Errorf("history final dependency metadata changed")
		}
	}
	if err = checkDirs(); err != nil {
		return err
	}
	for _, lease := range leases {
		if e := lease.check(); e != nil {
			return e
		}
	}
	verified = true
	return nil
}

type historyEndResult struct {
	err              error
	panicValue       any
	panicked, goexit bool
}

func checkHistoryEndResults(results []historyEndResult) error {
	for _, result := range results {
		if result.goexit {
			runtime.Goexit()
		}
		if result.panicked {
			panic(result.panicValue)
		}
	}
	// Already executed worker panics/Goexit must not be masked by a lower-key
	// ordinary file error after all workers have been joined.
	for _, result := range results {
		if result.err != nil {
			return result.err
		}
	}
	return nil
}
