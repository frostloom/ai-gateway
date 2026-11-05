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

func TestOutboxLeaseProcessChild(t *testing.T) {
	if os.Getenv("OUTBOX_LEASE_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, err := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	row, err := claimOutboxGeneration(context.Background(), db, "same")
	if err != nil || row.ID != 1 || row.Generation != 1 {
		t.Fatalf("claim=%+v err=%v", row, err)
	}
	fmt.Println("OUTBOX_CLAIM_COMMITTED")
	time.Sleep(time.Minute)
}

func TestOutboxLeaseKilledHolder(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_outbox_lease_v2 (id BIGINT UNSIGNED PRIMARY KEY, generation BIGINT NOT NULL DEFAULT 0, owner VARBINARY(64), lease_until DATETIME(6), sent_at DATETIME(6)) ENGINE=InnoDB",
		"DELETE FROM eval_outbox_lease_v2", "INSERT INTO eval_outbox_lease_v2(id) VALUES(1)",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutboxLeaseProcessChild$")
	cmd.Env = append(os.Environ(), "OUTBOX_LEASE_CHILD=1")
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
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			if strings.TrimSpace(scan.Text()) == "OUTBOX_CLAIM_COMMITTED" {
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
			t.Fatal("child exited before committed marker")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child timed out")
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed child")
	} else if exit, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("wait: %v", err)
	} else if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("unexpected exit: %v", err)
	}
	ctx := context.Background()
	var stored leaseGenerationRow
	if err := db.Table("eval_outbox_lease_v2").Where("id=1").Take(&stored).Error; err != nil || stored.Generation != 1 || stored.Owner == nil || *stored.Owner != "same" || stored.SentAt != nil {
		t.Fatalf("after kill=%+v err=%v", stored, err)
	}
	if row, err := claimOutboxGeneration(ctx, db, "same"); err != nil || row.ID != 0 {
		t.Fatalf("before expiry=%+v err=%v", row, err)
	}
	if err := db.Exec("UPDATE eval_outbox_lease_v2 SET lease_until=DATE_SUB(NOW(6),INTERVAL 1 SECOND) WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	row, err := claimOutboxGeneration(ctx, db, "same")
	if err != nil || row.ID != 1 || row.Generation != 2 {
		t.Fatalf("replacement=%+v err=%v", row, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", 1); err != nil || ok {
		t.Fatalf("old=%v err=%v", ok, err)
	}
	if ok, err := markOutboxGeneration(ctx, db, 1, "same", 2); err != nil || !ok {
		t.Fatalf("new=%v err=%v", ok, err)
	}
	t.Log("child claimed generation 1, killed and reaped; manual expiry then generation 2 fenced old marker")
}
