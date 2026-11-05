package comparison

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	mysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm/logger"
)

func TestRouterEventRetryAfterMySQLCrash(t *testing.T) {
	name := os.Getenv("COMPARISON_MYSQL_CONTAINER")
	if os.Getenv("TEST_ROUTER_EVENT_CRASH") != "1" || !regexp.MustCompile(`^auth-cache-test-mysql-[0-9]+-[0-9]+$`).MatchString(name) {
		t.Skip("requires runner-owned isolated container")
	}
	docker := func(timeout time.Duration, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	cfg, err := mysql.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Fatalf("invalid isolated DSN: %v", err)
	}
	port, err := docker(10*time.Second, "port", name, "3306/tcp")
	if err != nil || !strings.Contains(port, cfg.Addr) {
		t.Fatalf("DSN/container mismatch %q %v", port, err)
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}, &routerTestEvent{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("router-event-crash-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: "[]", Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	if out, err := docker(20*time.Second, "kill", name); err != nil {
		t.Fatalf("kill: %v %s", err, out)
	}
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_, _ = docker(20*time.Second, "start", name)
		}
	})
	attemptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	attemptErr := testRouterApply(attemptCtx, db, id, p.ID, false, false)
	cancel()
	if attemptErr == nil {
		t.Fatal("event accepted while database stopped")
	}
	if out, err := docker(20*time.Second, "start", name); err != nil {
		t.Fatalf("start: %v %s", err, out)
	}
	restarted = true
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = sqlDB.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mysql not ready: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	ctx := context.Background()
	var n int64
	var got store.Provider
	if err := db.Model(&routerTestEvent{}).Where("id=?", id).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("before retry events=%d err=%v", n, err)
	}
	if err := db.First(&got, p.ID).Error; err != nil || got.ConsecutiveFail != 0 {
		t.Fatalf("before retry count=%d err=%v", got.ConsecutiveFail, err)
	}
	for i := 0; i < 2; i++ {
		if err := testRouterApply(ctx, db, id, p.ID, false, false); err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
	}
	if err := testRouterApply(ctx, db, id, p.ID, true, false); !errors.Is(err, errRouterConflict) {
		t.Fatalf("different content=%v", err)
	}
	if err := db.Model(&routerTestEvent{}).Where("id=?", id).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("after retry events=%d err=%v", n, err)
	}
	if err := db.First(&got, p.ID).Error; err != nil || got.ConsecutiveFail != 1 {
		t.Fatalf("after retry count=%d err=%v", got.ConsecutiveFail, err)
	}
	t.Logf("while down: %v; after restart retry twice events=%d failures=%d conflicting replay rejected", attemptErr, n, got.ConsecutiveFail)
}
