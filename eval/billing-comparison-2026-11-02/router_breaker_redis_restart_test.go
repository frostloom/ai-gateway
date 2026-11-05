package comparison

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

// This isolated experiment intentionally restores an older persisted RDB image.
func TestBreakerProjectionRedisRestartRestoresOldSnapshot(t *testing.T) {
	container := os.Getenv("COMPARISON_OWNED_REDIS_CONTAINER")
	if !strings.HasPrefix(container, "auth-cache-test-redis-") || strings.ContainsAny(container, "/ ") {
		t.Fatal("owned isolated Redis container required")
	}
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
	readRunID := func() string {
		info, e := r.Info(ctx, "server").Result()
		if e != nil {
			t.Fatal(e)
		}
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "run_id:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "run_id:"))
			}
		}
		t.Fatal("Redis run_id missing")
		return ""
	}
	runBefore := readRunID()
	// Hold the same go-redis Conn object across the restart, and bind a
	// one-time identity check to its initial physical connection.
	pinned := r.Conn()
	defer pinned.Close()
	pinnedInfo, e := pinned.Info(ctx, "server").Result()
	if e != nil || !strings.Contains(pinnedInfo, "run_id:"+runBefore) {
		t.Fatalf("initial pinned identity mismatch: %v", e)
	}
	config, err := r.ConfigGet(ctx, "appendonly").Result()
	if err != nil || config["appendonly"] != "no" {
		t.Fatalf("expected RDB-only isolated Redis: %v %v", config, err)
	}
	key := fmt.Sprintf("eval:breaker:rdb:%d", time.Now().UnixNano())
	t.Cleanup(func() { r.Del(context.Background(), key) })
	if err := r.HSet(ctx, key, "version", 0, "state", "closed").Err(); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := projectionBlock.Run(ctx, r, []string{key}, 0, "owner").Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if applied, err := durableBreakerReport(ctx, db, "rdb-failure", false, ""); err != nil || !applied {
		t.Fatal(applied, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Version != 1 {
		t.Fatal(row, err)
	}
	if n, err := projectionPublish.Run(ctx, r, []string{key}, "owner", row.Version, row.State).Int(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := projectionPermit.Run(ctx, r, []string{key}).Int(); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	port, err := exec.Command("docker", "port", container, "6379/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("docker", "kill", container).CombinedOutput(); err != nil {
		t.Fatalf("kill: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "start", container).Run() })
	if out, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
		t.Fatalf("start: %v %s", err, out)
	}
	ready := false
	for i := 0; i < 30; i++ {
		if exec.Command("docker", "exec", container, "redis-cli", "ping").Run() == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("restart timeout")
	}
	portAfter, err := exec.Command("docker", "port", container, "6379/tcp").Output()
	if err != nil || strings.TrimSpace(string(port)) != strings.TrimSpace(string(portAfter)) {
		t.Fatal(string(port), string(portAfter), err)
	}
	runAfter := readRunID()
	if runBefore == "" || runBefore == runAfter {
		t.Fatalf("run_id did not change: %q %q", runBefore, runAfter)
	}
	// No second identity check is made on the pinned object. A transport
	// reconnect can otherwise invalidate the earlier one-time check.
	pinnedPermit := -99
	var pinnedErr error
	for i := 0; i < 3; i++ {
		pinnedPermit, pinnedErr = projectionPermit.Run(ctx, pinned, []string{key}).Int()
		if pinnedErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pinnedErr == nil {
		t.Fatalf("expected dedicated Conn failure after restart, permit=%d", pinnedPermit)
	}
	values, err := r.HGetAll(ctx, key).Result()
	if err != nil || values["state"] != "closed" || values["version"] != "0" {
		t.Fatal(values, err)
	}
	permit, err := projectionPermit.Run(ctx, r, []string{key}).Int()
	if err != nil || permit != 1 {
		t.Fatal(permit, err)
	}
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Version != 1 {
		t.Fatal(row, err)
	}
	t.Logf("appendonly=no old_rdb_saved=true redis_killed_restarted=true run_id_changed=true pinned_conn_error=true pooled_without_revalidation_permit=1 restored=%s/v%s mysql=%s/v%d redis_permit=%d", values["state"], values["version"], row.State, row.Version, permit)
}
