package comparison

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

func TestBreakerMultiprocessChild(t *testing.T) {
	if os.Getenv("BREAKER_MULTI_CHILD") != "1" {
		t.Skip("helper")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	fmt.Println("READY")
	var token [1]byte
	if _, err := io.ReadFull(os.Stdin, token[:]); err != nil {
		t.Fatal(err)
	}
	applied, err := durableBreakerReport(context.Background(), db, os.Getenv("BREAKER_MULTI_EVENT"), os.Getenv("BREAKER_MULTI_SUCCESS") == "1", "")
	switch {
	case errors.Is(err, errBreakerConflict):
		fmt.Println("RESULT conflict")
	case err != nil:
		t.Fatal(err)
	case applied:
		fmt.Println("RESULT applied")
	default:
		fmt.Println("RESULT duplicate")
	}
}

func TestBreakerMultiprocessDuplicateAndConflict(t *testing.T) {
	if os.Getenv("TEST_BREAKER_MULTIPROCESS") != "1" {
		t.Skip("isolated test only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker (id BIGINT PRIMARY KEY,state VARCHAR(16) NOT NULL, failures INT NOT NULL, open_until DATETIME(6) NULL, probe_token VARBINARY(64) NULL, probe_until DATETIME(6) NULL,version BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS eval_durable_breaker_event (event_id VARBINARY(128) PRIMARY KEY, success BOOLEAN NOT NULL, probe_token VARBINARY(64) NOT NULL) ENGINE=InnoDB",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"duplicate", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			for _, q := range []string{"DELETE FROM eval_durable_breaker_event", "DELETE FROM eval_durable_breaker", "INSERT INTO eval_durable_breaker(id,state,failures,version) VALUES(1,'closed',0,0)"} {
				if err := db.Exec(q).Error; err != nil {
					t.Fatal(err)
				}
			}
			id := fmt.Sprintf("multi-%s-%d", mode, time.Now().UnixNano())
			type child struct {
				cmd *exec.Cmd
				in  io.WriteCloser
				out *bufio.Scanner
			}
			var children []child
			childCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			defer func() {
				for _, c := range children {
					if c.cmd.ProcessState == nil {
						_ = c.cmd.Process.Kill()
						_ = c.cmd.Wait()
					}
				}
			}()
			for i := 0; i < 6; i++ {
				cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestBreakerMultiprocessChild$", "-test.v")
				success := "0"
				if mode == "conflict" && i%2 == 1 {
					success = "1"
				}
				cmd.Env = append(os.Environ(), "BREAKER_MULTI_CHILD=1", "BREAKER_MULTI_EVENT="+id, "BREAKER_MULTI_SUCCESS="+success)
				in, err := cmd.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				out, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				cmd.Stderr = os.Stderr
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				children = append(children, child{cmd: cmd, in: in, out: bufio.NewScanner(out)})
			}
			for _, c := range children {
				ready := false
				for c.out.Scan() {
					if strings.TrimSpace(c.out.Text()) == "READY" {
						ready = true
						break
					}
				}
				if !ready {
					t.Fatal("child did not become ready")
				}
			}
			for _, c := range children {
				if _, err := c.in.Write([]byte{1}); err != nil {
					t.Fatal(err)
				}
				_ = c.in.Close()
			}
			result := map[string]int{}
			for _, c := range children {
				for c.out.Scan() {
					line := strings.TrimSpace(c.out.Text())
					if strings.HasPrefix(line, "RESULT ") {
						result[strings.TrimPrefix(line, "RESULT ")]++
					}
				}
				if err := c.out.Err(); err != nil {
					t.Fatal(err)
				}
				if err := c.cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			if result["applied"] != 1 || (mode == "duplicate" && (result["duplicate"] != 5 || result["conflict"] != 0)) || (mode == "conflict" && (result["duplicate"] != 2 || result["conflict"] != 3)) {
				t.Fatal(result)
			}
			var row durableBreakerRow
			if err := db.Table("eval_durable_breaker").Where("id=1").Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Table("eval_durable_breaker_event").Count(&count).Error; err != nil || count != 1 || row.Version != 1 {
				t.Fatal(row, count, err)
			}
			var event durableBreakerEvent
			if err := db.Table("eval_durable_breaker_event").Where("event_id=?", id).Take(&event).Error; err != nil {
				t.Fatal(err)
			}
			if (!event.Success && (row.State != "open" || row.Failures != 1)) || (event.Success && (row.State != "closed" || row.Failures != 0)) {
				t.Fatal(row, event)
			}
			t.Logf("mode=%s processes=6 results=%v state=%s version=%d events=%d", mode, result, row.State, row.Version, count)
		})
	}
}
