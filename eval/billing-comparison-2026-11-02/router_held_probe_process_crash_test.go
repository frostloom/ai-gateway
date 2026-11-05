package comparison

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHeldProbeBeforeCommitChild(t *testing.T) {
	if os.Getenv("HELD_PROBE_CRASH_CHILD") != "1" {
		t.Skip("child helper")
	}
	db, err := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// The generation event INSERT already ran in this transaction; stop before state UPDATE.
	if err := db.Callback().Update().Before("gorm:update").Register("hold-before-state-update", func(tx *gorm.DB) { fmt.Println("HELD_EVENT_INSERTED_BEFORE_STATE_UPDATE"); time.Sleep(time.Minute) }); err != nil {
		t.Fatal(err)
	}
	_, err = applyHeldWithCooldown(context.Background(), db, "first")
	t.Fatalf("child unexpectedly returned: %v", err)
}

func TestHeldProbeTransactionCrashBeforeStateUpdate(t *testing.T) {
	db := setupHeldFailure(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHeldProbeBeforeCommitChild$")
	cmd.Env = append(os.Environ(), "HELD_PROBE_CRASH_CHILD=1")
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
			if strings.TrimSpace(s.Text()) == "HELD_EVENT_INSERTED_BEFORE_STATE_UPDATE" {
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
			t.Fatal("child exited before transaction marker")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child marker timeout")
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	err = cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("wait=%v", err)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("wait=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var events, outbox int64
	var row generationRow
	if err := db.WithContext(ctx).Table("eval_probe_generation_event").Count(&events).Error; err != nil || events != 0 {
		t.Fatalf("before retry events=%d err=%v", events, err)
	}
	if err := db.WithContext(ctx).Table("eval_bound_probe_outbox").Count(&outbox).Error; err != nil || outbox != 1 {
		t.Fatalf("outbox=%d err=%v", outbox, err)
	}
	if err := db.WithContext(ctx).Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "half_open" || row.Generation != 1 {
		t.Fatalf("before retry state=%+v err=%v", row, err)
	}
	if applied, err := applyHeldWithCooldown(ctx, db, "first"); err != nil || !applied {
		t.Fatalf("retry=%v err=%v", applied, err)
	}
	if applied, err := applyHeldWithCooldown(ctx, db, "first"); err != nil || applied {
		t.Fatalf("duplicate=%v err=%v", applied, err)
	}
	if gen, err := allocateProbeWithCooldown(ctx, db, "next", "token-2"); err != nil || gen != 0 {
		t.Fatalf("cooldown generation=%d err=%v", gen, err)
	}
	if err := db.WithContext(ctx).Table("eval_probe_generation_event").Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("after retry events=%d err=%v", events, err)
	}
	if err := db.WithContext(ctx).Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Expires == nil || row.Generation != 1 {
		t.Fatalf("after retry state=%+v err=%v", row, err)
	}
	t.Log("SIGKILL before state update: event rolled back, outbox retained; replay applied once and cooldown held")
}
