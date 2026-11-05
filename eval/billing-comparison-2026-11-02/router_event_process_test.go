package comparison

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRouterEventProcessHelper(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENT_CHILD") != "1" {
		t.Skip("child only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	provider, err := strconv.ParseUint(os.Getenv("TEST_ROUTER_EVENT_PROVIDER"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEST_ROUTER_EVENT_CUT") == "before_commit" {
		if err := db.Callback().Update().After("gorm:update").Register("eval:before_commit", func(tx *gorm.DB) {
			if tx.Statement.Table == "providers" {
				fmt.Println("UPDATE_DONE_BEFORE_COMMIT")
				time.Sleep(time.Minute)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := testRouterApply(context.Background(), db, os.Getenv("TEST_ROUTER_EVENT_ID"), provider, false, false); err != nil {
		t.Fatal(err)
	}
	fmt.Println("COMMIT_ACKNOWLEDGED_BY_CHILD")
	time.Sleep(time.Minute)
	t.Fatal("parent did not terminate child")
}

func TestRouterEventRetryAfterProcessKill(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENTS") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("router-child-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRouterEventProcessHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_ROUTER_EVENT_CHILD=1", "TEST_ROUTER_EVENT_PROVIDER="+strconv.FormatUint(p.ID, 10), "TEST_ROUTER_EVENT_ID="+id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	got := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "COMMIT_ACKNOWLEDGED_BY_CHILD" {
				got <- true
				return
			}
		}
		got <- false
	}()
	select {
	case ok := <-got:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("child did not commit: %s", stderr.String())
		}
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(ctx.Err())
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child unexpectedly exited successfully")
	}
	if err := testRouterApply(ctx, db, id, p.ID, false, false); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&routerTestEvent{}).Where("id=?", id).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("events=%d err=%v", n, err)
	}
	var gotProvider store.Provider
	if err := db.First(&gotProvider, p.ID).Error; err != nil || gotProvider.ConsecutiveFail != 1 || gotProvider.Status != 0 {
		t.Fatalf("provider=%+v err=%v", gotProvider, err)
	}
	t.Logf("child_commit_then_killed=true retried=true event_rows=%d failures=%d status=%d", n, gotProvider.ConsecutiveFail, gotProvider.Status)
}

func TestRouterEventRetryAfterKillBeforeCommit(t *testing.T) {
	if os.Getenv("TEST_ROUTER_EVENTS") != "1" {
		t.Skip("isolated only")
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("router-uncommitted-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRouterEventProcessHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_ROUTER_EVENT_CHILD=1", "TEST_ROUTER_EVENT_CUT=before_commit", "TEST_ROUTER_EVENT_PROVIDER="+strconv.FormatUint(p.ID, 10), "TEST_ROUTER_EVENT_ID="+id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	got := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "UPDATE_DONE_BEFORE_COMMIT" {
				got <- true
				return
			}
		}
		got <- false
	}()
	select {
	case ok := <-got:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("child did not reach uncommitted update")
		}
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(ctx.Err())
	}
	if err := cmd.Process.Kill(); err != nil {
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child unexpectedly exited successfully")
	}
	// The retry must wait for any server-side rollback before it can succeed.
	if err := testRouterApply(ctx, db, id, p.ID, false, false); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&routerTestEvent{}).Where("id=?", id).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("events=%d err=%v", n, err)
	}
	var gotProvider store.Provider
	if err := db.First(&gotProvider, p.ID).Error; err != nil || gotProvider.ConsecutiveFail != 1 || gotProvider.Status != 0 {
		t.Fatalf("provider=%+v err=%v", gotProvider, err)
	}
	t.Logf("child_killed_after_update_before_commit=true retry_events=%d failures=%d status=%d", n, gotProvider.ConsecutiveFail, gotProvider.Status)
}
