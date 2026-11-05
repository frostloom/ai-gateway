package comparison

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/gateway/middleware"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthPoolGRPCConcurrentUnderBillingSaturation(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	mainStore := store.NewStore(db, r)
	if err := mainStore.AutoMigrate(); err != nil {
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
	authSQL.SetMaxOpenConns(16)
	defer authSQL.Close()
	id := fmt.Sprintf("pool-grpc-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(id))
	key := store.APIKey{TenantID: tenant.ID, KeyHash: fmt.Sprintf("%x", sum), Name: "ordinary"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("pool-test-token")))
	pb.RegisterBillingServiceServer(rpc, authPoolCandidate(mainStore, store.NewStore(authDB, r)))
	go func() { _ = rpc.Serve(listener) }()
	t.Cleanup(rpc.Stop)
	client, err := gateway.NewBilling(listener.Addr().String(), "pool-test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(middleware.APIKey(client))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	mainSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	mainLimit := 100
	if os.Getenv("TEST_AUTH_POOL") == "1" {
		mainLimit = 84
	}
	mainSQL.SetMaxOpenConns(mainLimit)
	occupied := make([]*sql.Conn, 0, mainLimit)
	defer func() {
		for _, conn := range occupied {
			_ = conn.Close()
		}
	}()
	for i := 0; i < mainLimit; i++ {
		conn, err := mainSQL.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		occupied = append(occupied, conn)
	}
	const count = 32
	results := make(chan int, count)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+id)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results <- w.Code
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[http.StatusNoContent] != count {
		t.Fatalf("pool saturation should not prevent authentication: statuses=%v", counts)
	}
	for _, conn := range occupied {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	occupied = nil
	if err := db.Model(&store.APIKey{}).Where("id=?", key.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+id)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled key HTTP %d", w.Code)
	}
	if err := authSQL.Close(); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed auth pool HTTP %d", w.Code)
	}
	t.Logf("main_pool_held=%d auth_pool_limit=16 concurrent=%d success=%d disabled=401 closed_pool=503", mainLimit, count, counts[http.StatusNoContent])
}
