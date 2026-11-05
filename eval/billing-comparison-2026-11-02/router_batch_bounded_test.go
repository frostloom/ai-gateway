package comparison

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

var errBatchFull = errors.New("report queue full")
var errBatchClosed = errors.New("report queue closed")

type boundedBatch struct {
	mu     sync.Mutex
	closed bool
	inner  *batchReport
}

func newBoundedBatch(size int) *boundedBatch {
	return &boundedBatch{inner: &batchReport{jobs: make(chan reportJob, size)}}
}

// Test-only nonblocking admission. A successful submission remains owned by
// the worker even when the caller stops waiting; retry is not automatically safe.
func (b *boundedBatch) try(success bool) (<-chan error, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errBatchClosed
	}
	done := make(chan error, 1)
	select {
	case b.inner.jobs <- reportJob{success: success, done: done}:
		return done, nil
	default:
		return nil, errBatchFull
	}
}
func (b *boundedBatch) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.inner.jobs)
	}
}
func awaitBatch(ctx context.Context, done <-chan error) (error, bool) {
	select {
	case e := <-done:
		return e, false
	case <-ctx.Done():
		return ctx.Err(), true
	}
}

func TestBoundedBatchAdmissionAndClose(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.Exec("CREATE TABLE IF NOT EXISTS eval_router_batch (id BIGINT PRIMARY KEY,status INT NOT NULL,consecutive_fail INT NOT NULL) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM eval_router_batch").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO eval_router_batch VALUES(1,0,0)").Error; e != nil {
		t.Fatal(e)
	}
	b := newBoundedBatch(2)
	a, e := b.try(true)
	if e != nil || a == nil {
		t.Fatalf("first=%v %v", a, e)
	}
	c, e := b.try(true)
	if e != nil || c == nil {
		t.Fatalf("second=%v %v", c, e)
	}
	if _, e := b.try(true); !errors.Is(e, errBatchFull) {
		t.Fatalf("full queue error=%v", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if e, unknown := awaitBatch(ctx, a); !errors.Is(e, context.DeadlineExceeded) || !unknown {
		t.Fatalf("accepted timeout=%v unknown=%v", e, unknown)
	}
	b.close()
	b.close()
	if _, e := b.try(true); !errors.Is(e, errBatchClosed) {
		t.Fatalf("closed queue error=%v", e)
	}
	done := make(chan struct{})
	go func() { defer close(done); b.inner.run(db) }()
	for _, ch := range []<-chan error{a, c} {
		select {
		case e := <-ch:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("accepted work did not drain")
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestBoundedBatchConcurrentClose(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.Exec("CREATE TABLE IF NOT EXISTS eval_router_batch (id BIGINT PRIMARY KEY,status INT NOT NULL,consecutive_fail INT NOT NULL) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM eval_router_batch").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO eval_router_batch VALUES(1,0,0)").Error; e != nil {
		t.Fatal(e)
	}
	b := newBoundedBatch(128)
	first, e := b.try(true)
	if e != nil {
		t.Fatal(e)
	}
	receipts := make(chan (<-chan error), 64)
	errs := make(chan error, 64)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			receipt, e := b.try(true)
			if e == nil {
				receipts <- receipt
			} else if !errors.Is(e, errBatchClosed) {
				errs <- e
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); <-start; b.close() }()
	close(start)
	wg.Wait()
	close(receipts)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	b.close()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); b.inner.run(db) }()
	all := []<-chan error{first}
	for receipt := range receipts {
		all = append(all, receipt)
	}
	for _, ch := range all {
		select {
		case e := <-ch:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("accepted report lost")
		}
	}
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
	t.Logf("accepted=%d; all accepted reports completed", len(all))
}
