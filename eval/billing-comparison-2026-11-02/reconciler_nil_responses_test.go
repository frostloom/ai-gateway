package comparison

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/reconciler"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc"
	"gorm.io/gorm/logger"
)

type nilReconcileBilling struct {
	pb.BillingServiceClient
	valid bool
}

func (b *nilReconcileBilling) Settle(context.Context, *pb.SettleRequest, ...grpc.CallOption) (*pb.SettleResponse, error) {
	if b.valid {
		return &pb.SettleResponse{Settled: true}, nil
	}
	return nil, nil
}
func (b *nilReconcileBilling) Reverse(context.Context, *pb.ReverseRequest, ...grpc.CallOption) (*pb.ReverseResponse, error) {
	if b.valid {
		return &pb.ReverseResponse{Reversed: true}, nil
	}
	return nil, nil
}
func (b *nilReconcileBilling) RebuildBalance(context.Context, *pb.RebuildBalanceRequest, ...grpc.CallOption) (*pb.RebuildBalanceResponse, error) {
	if b.valid {
		return &pb.RebuildBalanceResponse{}, nil
	}
	return nil, nil
}

func TestReconcilerNilResponsesCountAsErrors(t *testing.T) {
	db, _ := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	if err := db.AutoMigrate(&store.Tenant{}, &store.Bill{}); err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("nil-reconcile-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: tag}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&tenant)
	usage := time.Now().Add(-time.Hour)
	for _, v := range []struct {
		name  string
		known bool
	}{{"known", true}, {"unknown", false}} {
		bill := store.Bill{RequestID: tag + "-" + v.name, Phase: store.PhaseReserve, Status: store.StatusPending, TenantID: tenant.ID, APIKeyID: 1, Model: "synthetic", Funding: "balance", CreatedAt: usage}
		if v.known {
			bill.UsageReportedAt = &usage
		}
		if err := db.Create(&bill).Error; err != nil {
			t.Fatal(err)
		}
	}
	defer db.Where("tenant_id=?", tenant.ID).Delete(&store.Bill{})
	fake := &nilReconcileBilling{}
	runner := reconciler.New(db, fake, slog.New(slog.DiscardHandler), time.Minute, reconciler.WithAutoReverse())
	stats, err := runner.SweepOnce(context.Background())
	if err != nil || stats.Scanned != 2 || stats.SettleErrors != 1 || stats.ReverseErrors != 1 || stats.Settled != 0 || stats.Reversed != 0 {
		t.Fatalf("nil sweep=%+v err=%v", stats, err)
	}
	rebuilt, err := runner.RebuildAll(context.Background())
	if err != nil || rebuilt.RebuildErrors != 1 || rebuilt.Rebuilt != 0 {
		t.Fatalf("nil rebuild=%+v err=%v", rebuilt, err)
	}
	fake.valid = true
	stats, err = runner.SweepOnce(context.Background())
	if err != nil || stats.Settled != 1 || stats.Reversed != 1 || stats.SettleErrors != 0 || stats.ReverseErrors != 0 {
		t.Fatalf("valid sweep=%+v err=%v", stats, err)
	}
	rebuilt, err = runner.RebuildAll(context.Background())
	if err != nil || rebuilt.Rebuilt != 1 || rebuilt.RebuildErrors != 0 {
		t.Fatalf("valid rebuild=%+v err=%v", rebuilt, err)
	}
	t.Log("nil settle/reverse/rebuild recorded as errors; subsequent nonnil responses counted normally")
}
