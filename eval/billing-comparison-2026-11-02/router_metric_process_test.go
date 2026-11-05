package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

func TestRouterProcessPersistenceMetricOnMySQLOutage(t *testing.T) {
	if os.Getenv("TEST_ROUTER_MYSQL_FAILURE") != "1" {
		t.Skip("requires disposable MySQL")
	}
	name := os.Getenv("COMPARISON_MYSQL_CONTAINER")
	if !regexp.MustCompile(`^auth-cache-test-mysql-[0-9]+-[0-9]+$`).MatchString(name) {
		t.Fatal("refusing non-test container")
	}
	cfg, err := mysql.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Fatalf("invalid DSN: %v", err)
	}
	mapping, err := exec.Command("docker", "port", name, "3306/tcp").CombinedOutput()
	if err != nil || !strings.Contains(string(mapping), cfg.Addr) {
		t.Fatalf("DSN/container mismatch: %s %v", mapping, err)
	}
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Provider{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("process-metric-%d", time.Now().UnixNano())
	p := store.Provider{Name: tag, BaseURL: "http://127.0.0.1:1", Models: fmt.Sprintf(`["%s"]`, tag), Weight: 1}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "router")
	build := exec.Command("go", "build", "-o", binary, "./cmd/router")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build=%v %s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cmd := exec.Command(binary)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "MYSQL_DSN=" + os.Getenv("COMPARISON_MYSQL_DSN"), "CONTROL_PLANE_TOKEN=isolated-process-test-token", "LISTEN_HOST=127.0.0.1", fmt.Sprintf("ROUTER_PORT=%d", port), "LOG_LEVEL=error"}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("router process not reaped")
		}
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for i := 0; i < 80; i++ {
		select {
		case err := <-done:
			t.Fatalf("router exited during startup: %v", err)
		default:
		}
		resp, e := client.Get(base + "/healthz")
		if e == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("router did not start")
	}
	get := func(path string) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(controlplane.Header, "isolated-process-test-token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("GET %s status=%d err=%v", path, resp.StatusCode, err)
		}
		return string(b)
	}
	metric := func() bool {
		return strings.Contains(get("/metrics"), `router_persistence_errors_total{operation="increment"} 1`)
	}
	report := func() {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, base+"/report", strings.NewReader(fmt.Sprintf(`{"provider_id":%d,"ok":false}`, p.ID)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(controlplane.Header, "isolated-process-test-token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("report status=%d", resp.StatusCode)
		}
	}
	docker := func(action string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", action, name).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %s %v", action, out, err)
		}
	}
	docker("kill")
	restarted := false
	defer func() {
		if !restarted {
			_ = exec.Command("docker", "start", name).Run()
		}
	}()
	report()
	if !metric() {
		t.Fatal("missing increment error on actual router metrics endpoint")
	}
	docker("start")
	restarted = true
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = sqlDB.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	report()
	var got store.Provider
	if err := db.First(&got, p.ID).Error; err != nil || got.ConsecutiveFail != 1 {
		t.Fatalf("row=%+v err=%v", got, err)
	}
	var states []router.State
	if err := json.Unmarshal([]byte(get("/breakers")), &states); err != nil || len(states) != 1 || states[0].ConsecutiveFail != 2 {
		t.Fatalf("states=%+v err=%v", states, err)
	}
	if !metric() {
		t.Fatal("error counter changed unexpectedly after recovery")
	}
	t.Log("actual router process: outage report HTTP 200, exposed increment error=1; recovery report HTTP 200, DB count=1, memory count=2")
}
