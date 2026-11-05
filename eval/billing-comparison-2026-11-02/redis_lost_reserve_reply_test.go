package comparison

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/logger"
)

// Runs the real Redis command, then discards its successful result inside the
// client hook. It models an ambiguous response, not a real network partition.
type lostReserveReply struct {
	guard string
	hits  atomic.Int32
}

func (h *lostReserveReply) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *lostReserveReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *lostReserveReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && (cmd.Name() == "eval" || cmd.Name() == "evalsha") {
			for _, arg := range cmd.Args() {
				if key, ok := arg.(string); ok && key == h.guard && h.hits.CompareAndSwap(0, 1) {
					return errors.New("synthetic lost reserve reply")
				}
			}
		}
		return err
	}
}
func TestReserveRedisAppliedReplyLostThenRetry(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("redis-lost-reply-%d", time.Now().UnixNano())
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
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	injector := &lostReserveReply{guard: store.ReqGuardKey(id)}
	r.AddHook(injector)
	_, err := s.ReserveWithReplaySignal(ctx, req)
	if status.Code(err) != codes.Unavailable || injector.hits.Load() != 1 {
		t.Fatalf("error=%v injected=%d", err, injector.hits.Load())
	}
	var count int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 900 {
		t.Fatal(live, err)
	}
	if exists, err := r.Exists(ctx, store.ReqGuardKey(id)).Result(); err != nil || exists != 1 {
		t.Fatal(exists, err)
	}
	first, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	again, err := s.ReserveWithReplaySignal(ctx, req)
	if err != nil || again.Code != pb.ReserveCode_RESERVE_DUPLICATE || again.BillId != first.BillId {
		t.Fatal(again, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 900 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	var events int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil || events != 1 {
		t.Fatal(events, err)
	}
	t.Logf("injected=%d bills_before=0 balance_after_lost=900 retry=%s replay=%s balance=%d ledger=%d events=%d", injector.hits.Load(), first.Code, again.Code, live, ledger, events)
}

func TestOtherReservationBeforeLostReplyRetry(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("redis-lost-interleave-%d", time.Now().UnixNano())
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
	old := &pb.ReserveRequest{RequestId: id + "-old", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	other := &pb.ReserveRequest{RequestId: id + "-other", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	injector := &lostReserveReply{guard: store.ReqGuardKey(old.RequestId)}
	r.AddHook(injector)
	if _, err := s.ReserveWithReplaySignal(ctx, old); status.Code(err) != codes.Unavailable || injector.hits.Load() != 1 {
		t.Fatal(err, injector.hits.Load())
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 900 {
		t.Fatal(live, err)
	}
	next, err := s.ReserveWithReplaySignal(ctx, other)
	if err != nil || next.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(next, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil || live != 900 {
		t.Fatal(live, err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || ledger != 900 {
		t.Fatal(ledger, err)
	}
	prior, err := s.ReserveWithReplaySignal(ctx, old)
	if err != nil || prior.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(prior, err)
	}
	live, err = r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 800 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id IN ? AND phase=?", []string{old.RequestId, other.RequestId}, store.PhaseReserve).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{old.RequestId + ":reserve", other.RequestId + ":reserve"}).Count(&events).Error; err != nil || bills != 2 || events != 2 {
		t.Fatal(bills, events, err)
	}
	t.Logf("lost_balance=900 other_then_balance=900 old_retry_balance=%d ledger=%d bills=%d events=%d", live, ledger, bills, events)
}

func TestConcurrentReservationsAfterLostRedisReply(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("redis-lost-concurrent-%d", time.Now().UnixNano())
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
	old := &pb.ReserveRequest{RequestId: id + "-old", TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	injector := &lostReserveReply{guard: store.ReqGuardKey(old.RequestId)}
	r.AddHook(injector)
	if _, err := s.ReserveWithReplaySignal(ctx, old); status.Code(err) != codes.Unavailable || injector.hits.Load() != 1 {
		t.Fatal(err, injector.hits.Load())
	}
	ids := []string{old.RequestId}
	for i := 0; i < 4; i++ {
		ids = append(ids, fmt.Sprintf("%s-other-%d", id, i))
	}
	var oldOK, oldDuplicate, othersOK atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rid := old.RequestId
			if i >= 4 {
				rid = ids[i-3]
			}
			req := &pb.ReserveRequest{RequestId: rid, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
			result, err := s.ReserveWithReplaySignal(ctx, req)
			if err != nil {
				errs <- err
				return
			}
			if i < 4 {
				switch result.Code {
				case pb.ReserveCode_RESERVE_OK:
					oldOK.Add(1)
				case pb.ReserveCode_RESERVE_DUPLICATE:
					oldDuplicate.Add(1)
				default:
					errs <- fmt.Errorf("old code %v", result.Code)
				}
			} else if result.Code == pb.ReserveCode_RESERVE_OK {
				othersOK.Add(1)
			} else {
				errs <- fmt.Errorf("other code %v", result.Code)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if oldOK.Load() != 1 || oldDuplicate.Load() != 3 || othersOK.Load() != 4 {
		t.Fatalf("old_ok=%d old_duplicate=%d others_ok=%d", oldOK.Load(), oldDuplicate.Load(), othersOK.Load())
	}
	live, err := r.Get(ctx, store.BalanceKey(tenant.ID)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || live != 500 || ledger != live {
		t.Fatal(live, ledger, err)
	}
	keys := make([]string, 0, len(ids))
	for _, rid := range ids {
		keys = append(keys, rid+":reserve")
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id IN ? AND phase=?", ids, store.PhaseReserve).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", keys).Count(&events).Error; err != nil || bills != 5 || events != 5 {
		t.Fatal(bills, events, err)
	}
	t.Logf("old_ok=%d old_duplicate=%d others_ok=%d balance=%d ledger=%d bills=%d events=%d", oldOK.Load(), oldDuplicate.Load(), othersOK.Load(), live, ledger, bills, events)
}

func TestInsufficientMultiTenantAfterLostRedisReply(t *testing.T) {
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	s := store.NewStore(db, r)
	ctx := context.Background()
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("redis-lost-limited-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: "mock", InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 2)
	ids := make([][]string, 2)
	for j := range tenants {
		tenants[j] = store.Tenant{Name: fmt.Sprintf("%s-t%d", id, j), InitialQuota: 250}
		if err := db.Create(&tenants[j]).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.Set(ctx, store.BalanceKey(tenants[j].ID), 250, 0).Err(); err != nil {
			t.Fatal(err)
		}
		for k := 0; k < 5; k++ {
			ids[j] = append(ids[j], fmt.Sprintf("%s-t%d-r%d", id, j, k))
		}
		hook := &lostReserveReply{guard: store.ReqGuardKey(ids[j][0])}
		r.AddHook(hook)
		req := &pb.ReserveRequest{RequestId: ids[j][0], TenantId: int64(tenants[j].ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
		if _, err := s.ReserveWithReplaySignal(ctx, req); status.Code(err) != codes.Unavailable || hook.hits.Load() != 1 {
			t.Fatal(err, hook.hits.Load())
		}
	}
	type result struct {
		tenant int
		rid    string
		code   pb.ReserveCode
		err    error
	}
	results := make(chan result, 16)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for j := 0; j < 2; j++ {
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(j, i int) {
				defer wg.Done()
				<-start
				index := 0
				if i >= 4 {
					index = i - 3
				}
				req := &pb.ReserveRequest{RequestId: ids[j][index], TenantId: int64(tenants[j].ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
				resp, err := s.ReserveWithReplaySignal(ctx, req)
				if err != nil {
					results <- result{tenant: j, rid: req.RequestId, err: err}
					return
				}
				results <- result{tenant: j, rid: req.RequestId, code: resp.Code}
			}(j, i)
		}
	}
	close(start)
	wg.Wait()
	close(results)
	oks := [2]int{}
	duplicates := [2]int{}
	insufficient := [2]int{}
	unique := [2]map[string]bool{make(map[string]bool), make(map[string]bool)}
	for res := range results {
		if res.err != nil {
			t.Error(res.err)
			continue
		}
		switch res.code {
		case pb.ReserveCode_RESERVE_OK:
			oks[res.tenant]++
			if unique[res.tenant][res.rid] {
				t.Errorf("duplicate OK %s", res.rid)
			}
			unique[res.tenant][res.rid] = true
		case pb.ReserveCode_RESERVE_DUPLICATE:
			duplicates[res.tenant]++
			if res.rid != ids[res.tenant][0] {
				t.Errorf("unexpected duplicate %s", res.rid)
			}
		case pb.ReserveCode_RESERVE_INSUFFICIENT:
			insufficient[res.tenant]++
		default:
			t.Errorf("unexpected %v", res.code)
		}
	}
	for j := range tenants {
		if oks[j] != 2 || oks[j]+duplicates[j]+insufficient[j] != 8 || duplicates[j] > 3 || (duplicates[j] > 0 && !unique[j][ids[j][0]]) {
			t.Fatalf("tenant %d ok=%d duplicates=%d insufficient=%d", j, oks[j], duplicates[j], insufficient[j])
		}
		live, err := r.Get(ctx, store.BalanceKey(tenants[j].ID)).Int64()
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LedgerBalance(ctx, db, tenants[j].ID)
		if err != nil || live != 50 || ledger != live {
			t.Fatal(j, live, ledger, err)
		}
		var bills, events int64
		if err := db.Model(&store.Bill{}).Where("request_id IN ? AND phase=?", ids[j], store.PhaseReserve).Count(&bills).Error; err != nil {
			t.Fatal(err)
		}
		keys := []string{}
		for _, rid := range ids[j] {
			keys = append(keys, rid+":reserve")
		}
		if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", keys).Count(&events).Error; err != nil || bills != 2 || events != 2 {
			t.Fatal(j, bills, events, err)
		}
		t.Logf("tenant=%d ok=%d duplicates=%d insufficient=%d balance=%d ledger=%d bills=%d events=%d", j, oks[j], duplicates[j], insufficient[j], live, ledger, bills, events)
	}
}
