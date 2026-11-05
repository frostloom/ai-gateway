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

func TestAttemptProcessChild(t *testing.T) {
	if os.Getenv("ATTEMPT_CHILD") != "1" {
		t.Skip("helper only")
	}
	db, e := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	id := os.Getenv("ATTEMPT_ID")
	ctx := context.Background()
	if ok, e := beginAttempt(ctx, db, id); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if os.Getenv("ATTEMPT_STAGE") == "completed" {
		if ok, e := completeAttempt(ctx, db, id, false); e != nil || !ok {
			t.Fatal(ok, e)
		}
	}
	fmt.Println("ATTEMPT_DURABLE_READY")
	time.Sleep(time.Minute)
}
func TestAttemptKilledWorker(t *testing.T) {
	db := setupAttemptDB(t)
	ctx := context.Background()
	base := fmt.Sprintf("attempt-kill-%d", time.Now().UnixNano())
	for _, stage := range []string{"sending", "completed"} {
		t.Run(stage, func(t *testing.T) {
			a, e := prepareAttempt(ctx, db, 1, base+stage, 0, 7)
			if e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestAttemptProcessChild$")
			cmd.Env = append(os.Environ(), "ATTEMPT_CHILD=1", "ATTEMPT_ID="+a.AttemptID, "ATTEMPT_STAGE="+stage)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			cmd.Stderr = os.Stderr
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			ready := make(chan bool, 1)
			go func() {
				scan := bufio.NewScanner(stdout)
				for scan.Scan() {
					if strings.TrimSpace(scan.Text()) == "ATTEMPT_DURABLE_READY" {
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
					t.Fatal("child ended before ready")
				}
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("child timeout")
			}
			if e := cmd.Process.Kill(); e != nil {
				_ = cmd.Wait()
				t.Fatal(e)
			}
			_ = cmd.Wait()
			var row attemptRow
			if e := db.Table("eval_route_attempt").Where("attempt_id=?", a.AttemptID).Take(&row).Error; e != nil {
				t.Fatal(e)
			}
			if row.State != stage {
				t.Fatalf("state=%s want=%s", row.State, stage)
			}
			if ok, e := beginAttempt(ctx, db, a.AttemptID); e != nil || ok {
				t.Fatalf("retry send=%v err=%v", ok, e)
			}
			var count int64
			if e := db.Table("eval_attempt_outbox").Where("attempt_id=?", a.AttemptID).Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			if stage == "sending" {
				if count != 0 || row.Outcome != nil {
					t.Fatalf("unknown attempt has result: count=%d row=%+v", count, row)
				}
			} else {
				if count != 1 || row.Outcome == nil || *row.Outcome {
					t.Fatalf("completed row=%+v outbox=%d", row, count)
				}
				if ok, e := completeAttempt(ctx, db, a.AttemptID, false); e != nil || ok {
					t.Fatalf("replayed completion=%v err=%v", ok, e)
				}
			}
		})
	}
}
