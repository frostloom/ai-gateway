package comparison

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthPoolMySQLRestartFailsClosedAndRecovers(t *testing.T) {
	name := os.Getenv("COMPARISON_MYSQL_CONTAINER")
	if !regexp.MustCompile(`^auth-cache-test-mysql-[0-9]+-[0-9]+$`).MatchString(name) {
		t.Skip("requires runner-owned isolated MySQL container")
	}
	docker := func(timeout time.Duration, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		b, e := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		return strings.TrimSpace(string(b)), e
	}
	cfg, err := mysqldriver.ParseDSN(os.Getenv("COMPARISON_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Fatalf("invalid isolated DSN: %v", err)
	}
	portBefore, err := docker(10*time.Second, "port", name, "3306/tcp")
	if err != nil || !strings.Contains(portBefore, cfg.Addr) {
		t.Fatalf("container/DSN mismatch: %s %v", portBefore, err)
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	primary := store.NewStore(db, r)
	if err := primary.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	authDB, err := gorm.Open(mysql.Open(os.Getenv("COMPARISON_MYSQL_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	authSQL, err := authDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer authSQL.Close()
	id := fmt.Sprintf("pool-restart-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(id))
	key := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("%x", hash), Name: "ordinary"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("restart-test-token")))
	pb.RegisterBillingServiceServer(rpc, authPoolCandidate(primary, store.NewStore(authDB, r)))
	go func() { _ = rpc.Serve(listener) }()
	t.Cleanup(rpc.Stop)
	client, err := gateway.NewBilling(listener.Addr().String(), "restart-test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(middleware.APIKey(client))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+id)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	if got := request(); got != http.StatusNoContent {
		t.Fatalf("initial HTTP %d", got)
	}
	if out, err := docker(15*time.Second, "kill", name); err != nil {
		t.Fatalf("kill owned MySQL: %s %v", out, err)
	}
	stopped := true
	defer func() {
		if stopped {
			_, _ = docker(20*time.Second, "start", name)
		}
	}()
	if got := request(); got != http.StatusServiceUnavailable {
		t.Fatalf("stopped database should fail closed, HTTP %d", got)
	}
	if out, err := docker(20*time.Second, "start", name); err != nil {
		t.Fatalf("restart owned MySQL: %s %v", out, err)
	}
	stopped = false
	if portAfter, err := docker(10*time.Second, "port", name, "3306/tcp"); err != nil || portAfter != portBefore {
		t.Fatalf("mapped port changed: %q -> %q %v", portBefore, portAfter, err)
	}
	start := time.Now()
	deadline := start.Add(90 * time.Second)
	for {
		_, err = docker(3*time.Second, "exec", name, "mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-pisolated", "--silent")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned MySQL readiness timeout")
		}
		time.Sleep(500 * time.Millisecond)
	}
	authDeadline := time.Now().Add(20 * time.Second)
	var attempts int
	var got int
	for {
		attempts++
		got = request()
		if got == http.StatusNoContent {
			break
		}
		if time.Now().After(authDeadline) {
			t.Fatalf("auth did not recover: HTTP %d attempts=%d", got, attempts)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := db.Model(&store.APIKey{}).Where("id=?", key.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if got := request(); got != http.StatusUnauthorized {
		t.Fatalf("disabled key after restart HTTP %d", got)
	}
	t.Logf("stopped=503 recovered=204 disabled=401 elapsed=%s attempts=%d", time.Since(start), attempts)
}
