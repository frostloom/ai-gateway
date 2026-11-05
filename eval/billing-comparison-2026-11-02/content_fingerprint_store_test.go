package comparison

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

func TestContentFingerprintConcurrentStore(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("content-concurrent-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 1000}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, store.BalanceKey(tenant.ID), 1000, 0).Err(); err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	ctxA := controlplane.WithReserveContent(ctx, strings.Repeat("a", 64))
	ctxB := controlplane.WithReserveContent(ctx, strings.Repeat("b", 64))
	first, err := s.ReserveWithReplaySignal(ctxA, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := ctxA
			if i%2 == 1 {
				c = ctxB
			}
			resp, err := s.ReserveWithReplaySignal(c, req)
			if i%2 == 0 {
				if err != nil || resp.Code != pb.ReserveCode_RESERVE_DUPLICATE {
					errs <- fmt.Errorf("equal: %v %v", resp, err)
				}
			} else if status.Code(err) != codes.FailedPrecondition {
				errs <- fmt.Errorf("different: %v %v", resp, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if _, err := s.ReserveWithReplaySignal(ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("v1 replay of v2=%v", err)
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != live {
		t.Fatal(live, ledger, err)
	}
	t.Logf("equal_duplicates=10 different_conflicts=10 v1_rejected=true reserve=1 balance=%d ledger=%d", live, ledger)
}
