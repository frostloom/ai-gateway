package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Replaced in the isolated candidate overlay; baseline uses the existing service.
func authPoolCandidate(primary, auth *store.Store) *server.Service {
	return server.NewService(primary, slog.New(slog.DiscardHandler))
}

func TestAuthPoolIsolationRetainsAuthority(t *testing.T) {
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
	authSQL.SetMaxOpenConns(1)
	defer authSQL.Close()
	id := fmt.Sprintf("auth-pool-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	key := store.APIKey{TenantID: tenant.ID, KeyHash: id, Name: "test"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	mainSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	mainSQL.SetMaxOpenConns(1)
	conn, err := mainSQL.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	svc := authPoolCandidate(primary, store.NewStore(authDB, r))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	resp, err := svc.ValidateAPIKey(ctx, &pb.ValidateAPIKeyRequest{ApiKeyHash: id})
	cancel()
	if err != nil || resp == nil || !resp.Valid {
		conn.Close()
		t.Fatalf("independent auth should succeed while primary pool is occupied: resp=%+v err=%v", resp, err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.APIKey{}).Where("id=?", key.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	resp, err = svc.ValidateAPIKey(context.Background(), &pb.ValidateAPIKeyRequest{ApiKeyHash: id})
	if err != nil || resp.Valid {
		t.Fatalf("disabled key accepted: %+v %v", resp, err)
	}
	if err := db.Model(&store.APIKey{}).Where("id=?", key.ID).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.Tenant{}).Where("id=?", tenant.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	resp, err = svc.ValidateAPIKey(context.Background(), &pb.ValidateAPIKeyRequest{ApiKeyHash: id})
	if err != nil || resp.Valid {
		t.Fatalf("disabled tenant accepted: %+v %v", resp, err)
	}
	if err := authSQL.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = svc.ValidateAPIKey(context.Background(), &pb.ValidateAPIKeyRequest{ApiKeyHash: id})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("unavailable auth pool must fail closed: %v", err)
	}
}
