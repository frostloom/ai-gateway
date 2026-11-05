package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	"github.com/frostloom/ai-gateway/internal/router"
	mysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm/logger"
)

// PASS characterizes an unsafe acknowledgement, not correctness.
func TestRouterReportMySQLCrashAndRecovery(t *testing.T) {
	if os.Getenv("TEST_ROUTER_MYSQL_FAILURE") != "1" {
		t.Skip("requires isolated MySQL container")
	}
	name := os.Getenv("COMPARISON_MYSQL_CONTAINER")
	if !regexp.MustCompile(`^auth-cache-test-mysql-[0-9]+-[0-9]+$`).MatchString(name) {
		t.Fatal("refusing non-test container")
	}
	cfg, err := mysql.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Fatalf("invalid isolated DSN: %v", err)
	}
	port, err := exec.Command("docker", "port", name, "3306/tcp").CombinedOutput()
	if err != nil || !strings.Contains(string(port), cfg.Addr) {
		t.Fatalf("DSN/container mismatch: %s %v", port, err)
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("router-outage-%d", time.Now().UnixNano())
	p := store.Provider{Name: id, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, id), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reg := router.New(db, slog.New(slog.DiscardHandler))
	if got, err := reg.Route(id); err != nil || got == nil {
		t.Fatalf("warm route=%v %v", got, err)
	}
	docker := func(args ...string) {
		t.Helper()
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(c, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
	}
	docker("kill", name)
	restarted := false
	t.Cleanup(func() {
		if !restarted {
			_ = exec.Command("docker", "start", name).Run()
		}
	})
	report := func() int {
		req := httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(fmt.Sprintf(`{"provider_id":%d,"ok":false}`, p.ID)))
		req.Header.Set(controlplane.Header, "test-token")
		w := httptest.NewRecorder()
		router.NewHTTPHandler(reg, "test-token", slog.New(slog.DiscardHandler)).ServeHTTP(w, req)
		return w.Code
	}
	beforeErrors := routerMetric(t, "increment")
	first := report()
	if first != 200 {
		t.Fatalf("unexpected failed report status=%d", first)
	}
	if delta := routerMetric(t, "increment") - beforeErrors; delta != 1 {
		t.Fatalf("persistence error metric delta=%v, want 1", delta)
	}
	states := reg.States()
	if len(states) != 1 || states[0].ConsecutiveFail != 1 {
		t.Fatalf("during outage states=%v", states)
	}
	docker("start", name)
	restarted = true
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = sqlDB.PingContext(pingCtx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mysql not ready: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	var row store.Provider
	if err := db.First(&row, p.ID).Error; err != nil || row.ConsecutiveFail != 0 {
		t.Fatalf("after restart db=%d err=%v", row.ConsecutiveFail, err)
	}
	second := report()
	if second != 200 {
		t.Fatalf("second=%d", second)
	}
	if err := db.First(&row, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	states = reg.States()
	if len(states) != 1 || states[0].ConsecutiveFail != 2 || row.ConsecutiveFail != 1 {
		t.Fatalf("after recovery db=%d states=%v", row.ConsecutiveFail, states)
	}
	t.Logf("mysql killed: first status=%d, persistence error metric delta=1; restart then second status=%d, memory failures=%d db failures=%d", first, second, states[0].ConsecutiveFail, row.ConsecutiveFail)
}
