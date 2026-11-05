package comparison

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Child simulates discovery and concurrent redelivery, not a production relay.
func TestHeldProbeDispatchChild(t *testing.T) {
	if os.Getenv("HELD_DISPATCH_CHILD") != "1" {
		t.Skip("child only")
	}
	db, err := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ ID string }
	q := db.Raw("SELECT o.id FROM eval_bound_probe_outbox o LEFT JOIN eval_probe_generation_event e ON e.event_id=o.id WHERE e.event_id IS NULL ORDER BY o.id LIMIT 1").Scan(&rows)
	if q.Error != nil || len(rows) != 1 {
		t.Fatalf("pending=%+v err=%v", rows, q.Error)
	}
	fmt.Printf("DISCOVERED %s\n", rows[0].ID)
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	applied, err := applyHeldWithCooldown(context.Background(), db, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("APPLIED %t\n", applied)
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
}

func TestHeldProbeTwoProcessOutboxDiscovery(t *testing.T) {
	db := setupHeldFailure(t)
	type child struct {
		cmd   *exec.Cmd
		in    io.WriteCloser
		lines chan string
		done  chan error
	}
	children := make([]child, 0, 2)
	defer func() {
		for _, c := range children {
			_ = c.in.Close()
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			select {
			case <-c.done:
			case <-time.After(5 * time.Second):
				t.Error("child not reaped")
			}
		}
	}()
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHeldProbeDispatchChild$")
		cmd.Env = append(os.Environ(), "HELD_DISPATCH_CHILD=1")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		lines := make(chan string, 8)
		done := make(chan error, 1)
		children = append(children, child{cmd, stdin, lines, done})
		go func() {
			s := bufio.NewScanner(stdout)
			for s.Scan() {
				lines <- strings.TrimSpace(s.Text())
			}
			close(lines)
		}()
		go func() { done <- cmd.Wait() }()
	}
	for _, c := range children {
		select {
		case line := <-c.lines:
			if line != "DISCOVERED first" {
				t.Fatalf("discovery=%q", line)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("discovery timeout")
		}
	}
	for _, c := range children {
		if _, err := io.WriteString(c.in, "continue\n"); err != nil {
			t.Fatal(err)
		}
	}
	applied := 0
	for _, c := range children {
		select {
		case line := <-c.lines:
			switch line {
			case "APPLIED true":
				applied++
			case "APPLIED false":
			default:
				t.Fatalf("result=%q", line)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("apply timeout")
		}
	}
	for _, c := range children {
		if _, err := io.WriteString(c.in, "finish\n"); err != nil {
			t.Fatal(err)
		}
	}
	for i := range children {
		select {
		case err := <-children[i].done:
			if err != nil {
				t.Fatalf("child exit=%v", err)
			}
			children[i].cmd.Process = nil
			children[i].done <- nil // satisfy deferred reaping check
		case <-time.After(30 * time.Second):
			t.Fatal("child exit timeout")
		}
	}
	if applied != 1 {
		t.Fatalf("applied=%d", applied)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var row generationRow
	if err := db.WithContext(ctx).Table("eval_probe_generation").Where("id=1").Take(&row).Error; err != nil || row.State != "open" || row.Generation != 1 || row.Expires == nil {
		t.Fatalf("state=%+v err=%v", row, err)
	}
	var events, pending int64
	if err := db.WithContext(ctx).Table("eval_probe_generation_event").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Raw("SELECT COUNT(*) FROM eval_bound_probe_outbox o LEFT JOIN eval_probe_generation_event e ON e.event_id=o.id WHERE e.event_id IS NULL").Scan(&pending).Error; err != nil || pending != 0 || events != 1 {
		t.Fatalf("events=%d pending=%d err=%v", events, pending, err)
	}
	if gen, err := allocateProbeWithCooldown(ctx, db, "next", "next-token"); err != nil || gen != 0 {
		t.Fatalf("cooldown gen=%d err=%v", gen, err)
	}
	t.Logf("two processes discovered same pending outbox; applied=%d events=%d pending=%d; cooldown held", applied, events, pending)
}
