package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

type sweepProbe struct {
	pb.BillingServiceClient
	reverse  atomic.Int32
	rebuild  atomic.Int32
	notified chan struct{}
}

func (p *sweepProbe) Reverse(context.Context, *pb.ReverseRequest, ...grpc.CallOption) (*pb.ReverseResponse, error) {
	p.reverse.Add(1)
	return &pb.ReverseResponse{Reversed: true}, nil
}
func (p *sweepProbe) RebuildBalance(context.Context, *pb.RebuildBalanceRequest, ...grpc.CallOption) (*pb.RebuildBalanceResponse, error) {
	p.rebuild.Add(1)
	select {
	case p.notified <- struct{}{}:
	default:
	}
	return &pb.RebuildBalanceResponse{}, nil
}

func TestReconcilerRunHonorsNoSweep(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("no-sweep-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(context.Background(), store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	bill, err := s.Reserve(context.Background(), &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(bill, err)
	}
	if err := db.Model(&store.Bill{}).Where("id=?", bill.BillId).UpdateColumn("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	for _, noSweep := range []bool{true, false} {
		probe := &sweepProbe{notified: make(chan struct{}, 1)}
		runner := reconciler.New(db, probe, slog.New(slog.DiscardHandler), 10*time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); runner.Run(ctx, time.Hour, noSweep, false) }()
		select {
		case <-probe.notified:
		case <-time.After(5 * time.Second):
			cancel()
			<-done
			t.Fatal("rebuild timeout")
		}
		cancel()
		<-done
		if (noSweep && probe.reverse.Load() != 0) || (!noSweep && probe.reverse.Load() < 1) || probe.rebuild.Load() < 1 {
			t.Fatalf("noSweep=%v reverse=%d rebuild=%d", noSweep, probe.reverse.Load(), probe.rebuild.Load())
		}
	}
	t.Log("no-sweep skipped Reverse; default attempted Reverse; rebuild ran in both cases")
}
