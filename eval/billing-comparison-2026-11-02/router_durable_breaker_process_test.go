package comparison

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDurableBreakerCommitChild(t *testing.T) {
	if os.Getenv("DURABLE_BREAKER_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, err := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := durableBreakerReport(context.Background(), db, os.Getenv("DURABLE_BREAKER_EVENT"), false, "")
	if err != nil || !applied {
		t.Fatal(applied, err)
	}
	fmt.Println("BREAKER_COMMITTED_BEFORE_REPLY")
	time.Sleep(time.Minute)
}
func TestDurableBreakerKilledAfterCommit(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
		"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	id := fmt.Sprintf("breaker-kill-%d", time.Now().UnixNano())
	cmd := exec.Command(os.Args[0], "-test.run=^TestDurableBreakerCommitChild$")
	cmd.Env = append(os.Environ(), "DURABLE_BREAKER_CHILD=1", "DURABLE_BREAKER_EVENT="+id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if strings.TrimSpace(s.Text()) == "BREAKER_COMMITTED_BEFORE_REPLY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("child exited before commit")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child timeout")
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx := context.Background()
	applied, err := durableBreakerReport(ctx, db, id, false, "")
	if err != nil || applied {
		t.Fatal(applied, err)
	}
	permit, err := durableBreakerPermit(ctx, db, "after-restart")
	if err != nil || permit {
		t.Fatal(permit, err)
	}
	var row durableBreakerRow
	if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Failures != 1 || row.Version != 1 {
		t.Fatal(row, err)
	}
	var events int64
	if err := db.Table("eval_durable_breaker_event").Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	t.Logf("child_killed_after_commit=true duplicate_applied=%v permit=%v state=%s failures=%d version=%d events=%d", applied, permit, row.State, row.Failures, row.Version, events)
}
