package comparison

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type reportJob struct {
	success bool
	done    chan error
}
type batchReport struct {
	jobs    chan reportJob
	updates atomic.Int64
}

func newBatchReport() *batchReport { return &batchReport{jobs: make(chan reportJob, 128)} }
func (b *batchReport) enqueue(success bool) <-chan error {
	done := make(chan error, 1)
	b.jobs <- reportJob{success, done}
	return done
}

// Test-only single-provider worker. It acknowledges only after SQL completes.
func (b *batchReport) run(db *gorm.DB) {
	var carry *reportJob
	closed := false
	for {
		var first reportJob
		if carry != nil {
			first = *carry
			carry = nil
		} else {
			var ok bool
			first, ok = <-b.jobs
			if !ok {
				return
			}
		}
		group := []reportJob{first}
		if first.success {
		collect:
			for {
				select {
				case next, ok := <-b.jobs:
					if !ok {
						closed = true
						break collect
					}
					if !next.success {
						carry = &next
						break collect
					}
					group = append(group, next)
				default:
					break collect
				}
			}
		}
		query := db.Table("eval_router_batch").Where("id=? AND status=?", 1, 0)
		var err error
		if first.success {
			err = query.UpdateColumn("consecutive_fail", 0).Error
		} else {
			err = query.UpdateColumn("consecutive_fail", gorm.Expr("consecutive_fail + 1")).Error
		}
		b.updates.Add(1)
		for _, job := range group {
			job.done <- err
		}
		if closed && carry == nil {
			return
		}
	}
}

func TestBatchedReportOrderingAndCommit(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.Exec("CREATE TABLE IF NOT EXISTS eval_router_batch (id BIGINT PRIMARY KEY,status INT NOT NULL,consecutive_fail INT NOT NULL) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM eval_router_batch").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO eval_router_batch VALUES(1,0,7)").Error; e != nil {
		t.Fatal(e)
	}
	b := newBatchReport()
	defer close(b.jobs)
	var first []<-chan error
	for i := 0; i < 32; i++ {
		first = append(first, b.enqueue(true))
	}
	failure := b.enqueue(false)
	last := b.enqueue(true)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	if e := db.Callback().Update().Before("gorm:update").Register("test:batch-barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "eval_router_batch" && once.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer db.Callback().Update().Remove("test:batch-barrier")
	go b.run(db)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("batch did not enter database")
	}
	select {
	case e := <-first[0]:
		close(resume)
		t.Fatalf("success returned before DB completion: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case e := <-failure:
		close(resume)
		t.Fatalf("failure overtook blocked success: %v", e)
	default:
	}
	close(resume)
	for _, done := range append(first, failure, last) {
		select {
		case e := <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("report did not complete")
		}
	}
	var count int
	if e := db.WithContext(context.Background()).Raw("SELECT consecutive_fail FROM eval_router_batch WHERE id=1").Scan(&count).Error; e != nil || count != 0 {
		t.Fatalf("final failure count=%d err=%v", count, e)
	}
	if n := b.updates.Load(); n != 3 {
		t.Fatalf("want 3 ordered SQL updates for success group, failure, success; got %d", n)
	}
}

func TestBatchedReportDatabaseFailure(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if e := db.Exec("DROP TABLE IF EXISTS eval_router_batch").Error; e != nil {
		t.Fatal(e)
	}
	b := newBatchReport()
	defer close(b.jobs)
	var done []<-chan error
	for i := 0; i < 16; i++ {
		done = append(done, b.enqueue(true))
	}
	go b.run(db)
	for _, ch := range done {
		select {
		case e := <-ch:
			if e == nil {
				t.Fatal("failed write acknowledged as success")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("failed write left waiter stuck")
		}
	}
	if n := b.updates.Load(); n != 1 {
		t.Fatalf("expected one failed batch, got %d", n)
	}
}

func TestBatchedReportFullQueueBlocksUntilDrained(t *testing.T) {
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
	b := newBatchReport()
	var first []<-chan error
	for i := 0; i < cap(b.jobs); i++ {
		first = append(first, b.enqueue(true))
	}
	entered := make(chan struct{})
	extra := make(chan (<-chan error), 1)
	go func() { close(entered); extra <- b.enqueue(true) }()
	<-entered
	select {
	case <-extra:
		t.Fatal("full queue accepted request before drain")
	case <-time.After(100 * time.Millisecond):
	}
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); b.run(db) }()
	var extraDone <-chan error
	select {
	case extraDone = <-extra:
	case <-time.After(5 * time.Second):
		t.Fatal("queue did not unblock")
	}
	for _, c := range append(first, extraDone) {
		select {
		case e := <-c:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("report did not finish")
		}
	}
	close(b.jobs)
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
}
