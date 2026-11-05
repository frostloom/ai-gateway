package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/server"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	gateway "github.com/frostloom/ai-gateway/internal/gateway/client"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm/logger"
)

func TestReplayProtocolRealGRPCAndStore(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("real-replay-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(controlplane.UnaryServer("synthetic-token")))
	pb.RegisterBillingServiceServer(grpcServer, server.NewService(s, slog.New(slog.DiscardHandler)))
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	client, err := gateway.NewBilling(listener.Addr().String(), "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	var first, duplicate, errors atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			resp, e := client.Reserve(c, req)
			if e != nil {
				t.Errorf("reserve: %v", e)
				errors.Add(1)
				return
			}
			switch resp.Code {
			case pb.ReserveCode_RESERVE_OK:
				first.Add(1)
			case pb.ReserveCode_RESERVE_DUPLICATE:
				duplicate.Add(1)
			default:
				t.Errorf("unexpected code %v", resp.Code)
				errors.Add(1)
			}
		}()
	}
	wg.Wait()
	if first.Load() != 1 || duplicate.Load() != 15 || errors.Load() != 0 {
		t.Fatalf("first=%d duplicate=%d errors=%d", first.Load(), duplicate.Load(), errors.Load())
	}
	changed := proto.Clone(req).(*pb.ReserveRequest)
	changed.EstPromptTokens++
	_, err = client.Reserve(ctx, changed)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("changed parameter: %v", err)
	}
	conflictCode := status.Code(err)
	var bill store.Bill
	if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&bill).Error; err != nil {
		t.Fatal(err)
	}
	balance, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || balance != ledger {
		t.Fatal(balance, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	t.Logf("first=%d duplicate=%d errors=%d conflict=%s bill=%d balance=%d ledger=%d reserve_events=%d", first.Load(), duplicate.Load(), errors.Load(), conflictCode, bill.ID, balance, ledger, events)
}
