package comparison

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestReserveMissingLookupDoesNotLogError(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("lookup-log-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	db.Logger = logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Error})
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	first, err := s.Reserve(ctx, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatalf("first %+v %v", first, err)
	}
	again, err := s.Reserve(ctx, req)
	if err != nil || again.BillId != first.BillId {
		t.Fatalf("replay %+v %v", again, err)
	}
	if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id + "-missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing settle: %v", err)
	}
	if strings.Contains(output.String(), "record not found") {
		t.Errorf("expected absent reservation logged as error: %s", output.String())
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 900 || ledger != 900 {
		t.Fatalf("balances %d %d %v", live, ledger, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(ctx, &pb.SettleRequest{RequestId: id}); err == nil {
		t.Fatal("closed database error was ignored")
	}
	if !strings.Contains(output.String(), "database is closed") {
		t.Errorf("database failure not logged: %s", output.String())
	}
}
