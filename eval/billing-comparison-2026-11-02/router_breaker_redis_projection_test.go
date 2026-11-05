package comparison

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// These scripts are characterization prototypes only, not a production protocol.
var projectionApply = redis.NewScript(`
local previous=redis.call('HGET',KEYS[1],'version')
if not previous then return -1 end
if tonumber(ARGV[1]) <= tonumber(previous) then return 0 end
redis.call('HSET',KEYS[1],'version',ARGV[1],'state',ARGV[2])
return 1`)
var projectionPermit = redis.NewScript(`
local state=redis.call('HGET',KEYS[1],'state')
if not state then return -1 end
if state == 'closed' then return 1 end
return 0`)

func TestBreakerProjectionCommitGapAndMonotonicGuard(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	key := fmt.Sprintf("eval:breaker:projection:%d", time.Now().UnixNano())
	t.Cleanup(func() { r.Del(context.Background(), key) })
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	initial, err := projectionPermit.Run(ctx, r, []string{key}).Int()
	if err != nil || initial != 1 {
		t.Fatal(initial, err)
	}
	if applied, err := durableBreakerReport(ctx, db, "projection-event", false, ""); err != nil || !applied {
		t.Fatal(applied, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Version != 1 {
		t.Fatal(row, err)
	}
	// Explicitly pause publication after durable commit; no process/network fault injected.
	gap, err := projectionPermit.Run(ctx, r, []string{key}).Int()
	if err != nil || gap != 1 {
		t.Fatal(gap, err)
	}
	published, err := projectionApply.Run(ctx, r, []string{key}, row.Version, row.State).Int()
	if err != nil || published != 1 {
		t.Fatal(published, err)
	}
	stale, err := projectionApply.Run(ctx, r, []string{key}, 0, "closed").Int()
	if err != nil || stale != 0 {
		t.Fatal(stale, err)
	}
	denied, err := projectionPermit.Run(ctx, r, []string{key}).Int()
	if err != nil || denied != 0 {
		t.Fatal(denied, err)
	}
	if err := r.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	missing, err := projectionPermit.Run(ctx, r, []string{key}).Int()
	if err != nil || missing != -1 {
		t.Fatal(missing, err)
	}
	afterDelete, err := projectionApply.Run(ctx, r, []string{key}, 0, "closed").Int()
	if err != nil || afterDelete != -1 {
		t.Fatal(afterDelete, err)
	}
	t.Logf("mysql_after_commit=%s/v%d redis_gap_permit=%d stale_overwrite=%d after_publish_permit=%d missing_permit=%d", row.State, row.Version, gap, stale, denied, missing)
}

var projectionBlock = redis.NewScript(`
local version=redis.call('HGET',KEYS[1],'version')
local state=redis.call('HGET',KEYS[1],'state')
if not version or not state then return -1 end
if state == 'blocked' then return 0 end
if version ~= ARGV[1] then return -2 end
redis.call('HSET',KEYS[1],'state','blocked','owner',ARGV[2])
return 1`)
var projectionPublish = redis.NewScript(`
local version=redis.call('HGET',KEYS[1],'version')
local state=redis.call('HGET',KEYS[1],'state')
local owner=redis.call('HGET',KEYS[1],'owner')
if not version then return -1 end
if state ~= 'blocked' or owner ~= ARGV[1] then return 0 end
if tonumber(ARGV[2]) < tonumber(version) then return -2 end
if ARGV[3] ~= 'closed' and ARGV[3] ~= 'open' then return -3 end
redis.call('HSET',KEYS[1],'version',ARGV[2],'state',ARGV[3])
redis.call('HDEL',KEYS[1],'owner')
return 1`)

func TestBreakerProjectionBlocksBeforeCommit(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	key := fmt.Sprintf("eval:breaker:fence:%d", time.Now().UnixNano())
	t.Cleanup(func() { r.Del(context.Background(), key) })
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var owner string
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprintf("owner-%d", i)
			n, err := projectionBlock.Run(ctx, r, []string{key}, 0, token).Int()
			if err != nil {
				errs <- err
				return
			}
			if n == 1 {
				winners.Add(1)
				mu.Lock()
				owner = token
				mu.Unlock()
			} else if n != 0 {
				errs <- fmt.Errorf("block result=%d", n)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() || winners.Load() != 1 {
		t.Fatal(winners.Load())
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if applied, err := durableBreakerReport(ctx, db, "fenced-event", false, ""); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "stale-owner", 1, "closed").Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Version != 1 {
		t.Fatal(row, err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, owner, row.Version, row.State).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	// Characterize an old Redis snapshot being restored after the fence:
	// direct fixture overwrite simulates lost writes, not real Redis failover.
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	t.Logf("contenders=16 block_winners=%d blocked_before_and_after_commit=true stale_owner_rejected=true durable=%s/v%d restored_old_snapshot_permit=1", winners.Load(), row.State, row.Version)
}

// Characterizes why an operator cannot safely clear a blocked projection from
// an unfenced MySQL snapshot while its original writer may still run.
func TestBreakerProjectionUnsafeRecoveryAllowsLateCommit(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	key := fmt.Sprintf("eval:breaker:late:%d", time.Now().UnixNano())
	t.Cleanup(func() { r.Del(context.Background(), key) })
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := projectionBlock.Run(ctx, r, []string{key}, 0, "old-owner").Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var snapshot durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	// An unsafe recovery reuses the owner token to release from a stale snapshot.
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "old-owner", snapshot.Version, snapshot.State).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// The original writer was only delayed, not dead; this is sequential orchestration.
	if applied, err := durableBreakerReport(ctx, db, "late-owner-event", false, ""); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "old-owner", 1, "open").Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	allowed, err := projectionPermit.Run(ctx, r, []string{key}).Int()
	if err != nil || allowed != 1 {
		t.Fatal(allowed, err)
	}
	var final durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&final).Error; err != nil || final.State != "open" || final.Version != 1 {
		t.Fatal(final, err)
	}
	t.Logf("unsafe_recovery_snapshot=%s/v%d late_commit=%s/v%d late_publish_rejected=true redis_permit=%d", snapshot.State, snapshot.Version, final.State, final.Version, allowed)
}

func fencedBreakerReport(ctx context.Context, db *gorm.DB, epoch int64, id string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var f struct{ Epoch int64 }
		if err := tx.Table("eval_breaker_writer_fence").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&f).Error; err != nil {
			return err
		}
		if f.Epoch != epoch {
			return fmt.Errorf("stale writer epoch")
		}
		applied, err := durableBreakerReport(ctx, tx, id, false, "")
		if err != nil {
			return err
		}
		if !applied {
			return fmt.Errorf("unexpected duplicate")
		}
		return nil
	})
}
func TestBreakerProjectionFencedRecoveryRejectsLateWriter(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_breaker_writer_fence (id BIGINT PRIMARY KEY,epoch BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)", "DELETE FROM eval_breaker_writer_fence", "INSERT INTO eval_breaker_writer_fence VALUES(1,1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	key := fmt.Sprintf("eval:breaker:fenced:%d", time.Now().UnixNano())
	t.Cleanup(func() { r.Del(context.Background(), key) })
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := projectionBlock.Run(ctx, r, []string{key}, 0, "old-owner").Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// Controlled recovery obtains a durable new epoch before reading the snapshot.
	if err := db.Exec("UPDATE eval_breaker_writer_fence SET epoch=epoch+1 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	var before durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "old-owner", before.Version, before.State).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := fencedBreakerReport(ctx, db, 1, "late-fenced"); err == nil {
		t.Fatal("stale epoch committed")
	}
	var count int64
	if err := db.Table("eval_durable_breaker_event").Where("event_id=?", "late-fenced").Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if n, err := projectionBlock.Run(ctx, r, []string{key}, 0, "new-owner").Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := fencedBreakerReport(ctx, db, 2, "fresh-fenced"); err != nil {
		t.Fatal(err)
	}
	var after durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&after).Error; err != nil || after.State != "open" || after.Version != 1 {
		t.Fatal(after, err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "new-owner", after.Version, after.State).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	t.Log("old_epoch_rejected=true old_event=0 new_epoch_committed=true mysql=open redis_denied=true")
}

func TestBreakerProjectionRecoveryWaitsForActiveWriter(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_breaker_writer_fence (id BIGINT PRIMARY KEY,epoch BIGINT NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)", "DELETE FROM eval_breaker_writer_fence", "INSERT INTO eval_breaker_writer_fence VALUES(1,1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	key := fmt.Sprintf("eval:breaker:active:%d", time.Now().UnixNano())
	t.Cleanup(func() { r.Del(context.Background(), key) })
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := projectionBlock.Run(ctx, r, []string{key}, 0, "owner").Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var f struct{ Epoch int64 }
			if err := tx.Table("eval_breaker_writer_fence").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").Take(&f).Error; err != nil {
				return err
			}
			if f.Epoch != 1 {
				return fmt.Errorf("wrong epoch=%d", f.Epoch)
			}
			close(locked)
			<-release
			applied, err := durableBreakerReport(ctx, tx, "active-writer", false, "")
			if err != nil {
				return err
			}
			if !applied {
				return fmt.Errorf("not applied")
			}
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-writerDone:
		t.Fatalf("writer failed before locking: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not lock")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var recoveryID int64
	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&recoveryID); err != nil {
		t.Fatal(err)
	}
	recoveryDone := make(chan error, 1)
	go func() {
		_, err := conn.ExecContext(ctx, "UPDATE eval_breaker_writer_fence SET epoch=epoch+1 WHERE id=1")
		recoveryDone <- err
	}()
	waiting := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.innodb_trx WHERE trx_mysql_thread_id=? AND trx_state='LOCK WAIT'", recoveryID).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			waiting = true
			break
		}
		select {
		case err := <-recoveryDone:
			t.Fatalf("recovery advanced while writer holds fence: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("MySQL lock wait not observed")
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	unblock()
	for label, ch := range map[string]<-chan error{"writer": writerDone, "recovery": recoveryDone} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s timeout", label)
		}
	}
	var epoch int64
	if err := db.Raw("SELECT epoch FROM eval_breaker_writer_fence WHERE id=1").Scan(&epoch).Error; err != nil || epoch != 2 {
		t.Fatal(epoch, err)
	}
	var snapshot durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&snapshot).Error; err != nil || snapshot.State != "open" || snapshot.Version != 1 {
		t.Fatal(snapshot, err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "owner", snapshot.Version, snapshot.State).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	var count int64
	if err := db.Table("eval_durable_breaker_event").Where("event_id=?", "active-writer").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	t.Logf("mysql_lock_wait_observed=true active_writer_committed=true recovery_epoch=%d snapshot=%s/v%d events=%d redis_denied=true", epoch, snapshot.State, snapshot.Version, count)
}
