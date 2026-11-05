package comparison

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCandidateCorrectness(t *testing.T) {
	db, _ := isolatedDB(t)
	c := candidate{db: db}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.seed(101, 1000); err != nil {
		t.Fatal(err)
	}
	if err := c.seed(102, 1000); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	successes := make(chan bool, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tenant := uint64(101 + i%2)
			r, e := c.reserve(ctx, &billingv1.ReserveRequest{RequestId: fmt.Sprintf("cand-%d", i), TenantId: int64(tenant), ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84})
			if e != nil {
				t.Errorf("reserve: %v", e)
				return
			}
			successes <- r.Code == billingv1.ReserveCode_RESERVE_OK
		}(i)
	}
	wg.Wait()
	close(successes)
	n := 0
	for x := range successes {
		if x {
			n++
		}
	}
	if n != 20 {
		t.Fatalf("successes %d", n)
	}
	for _, id := range []uint64{101, 102} {
		b, e := c.balance(ctx, id)
		if e != nil || b != 0 {
			t.Fatalf("balance %d=%d %v", id, b, e)
		}
	}
	// Duplicate requests are checked independently of current available balance.
	req := &billingv1.ReserveRequest{RequestId: "dup", TenantId: 101, ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84}
	if err := c.seed(103, 1000); err != nil {
		t.Fatal(err)
	}
	req.TenantId = 103
	first, e := c.reserve(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	second, e := c.reserve(ctx, req)
	if e != nil || first.BillId != second.BillId {
		t.Fatalf("replay %+v %v", second, e)
	}
	req.EstPromptTokens++
	if _, e = c.reserve(ctx, req); status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("conflict %v", e)
	}
	req.EstPromptTokens--
	if e = c.usage(ctx, &billingv1.ReportUsageRequest{RequestId: "dup", PromptTokens: 50}); e != nil {
		t.Fatal(e)
	}
	if e = c.usage(ctx, &billingv1.ReportUsageRequest{RequestId: "dup", PromptTokens: 50}); e != nil {
		t.Fatal(e)
	}
	if e = c.usage(ctx, &billingv1.ReportUsageRequest{RequestId: "dup", PromptTokens: 51}); status.Code(e) != codes.FailedPrecondition {
		t.Fatalf("usage conflict %v", e)
	}
	for i := 0; i < 2; i++ {
		r, e := c.settle(ctx, "dup")
		if e != nil || !r {
			t.Fatalf("settle %v %v", r, e)
		}
	}
	if r, e := c.reverse(ctx, "dup"); e != nil || r {
		t.Fatalf("reverse settled %v %v", r, e)
	}
	b, e := c.balance(ctx, 103)
	if e != nil || b != 950 {
		t.Fatalf("settled balance %d %v", b, e)
	}
	req.RequestId = "reverse"
	if _, e = c.reserve(ctx, req); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e := c.reverse(ctx, "reverse")
		if e != nil || !r {
			t.Fatalf("reverse %v %v", r, e)
		}
	}
	b, e = c.balance(ctx, 103)
	if e != nil || b != 950 {
		t.Fatalf("reversed balance %d %v", b, e)
	}

	// Rollback after balance, bill and outbox writes simulates transaction abort.
	req.RequestId = "rollback-reserve"
	bad := c
	bad.fail = true
	if _, err := bad.reserve(ctx, req); err == nil {
		t.Fatal("expected injected reserve failure")
	}
	var rows int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", req.RequestId).Count(&rows).Error; err != nil || rows != 0 {
		t.Fatalf("rollback bills=%d err=%v", rows, err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?", req.RequestId+":%").Count(&rows).Error; err != nil || rows != 0 {
		t.Fatalf("rollback outbox=%d err=%v", rows, err)
	}
	if b, e := c.balance(ctx, 103); e != nil || b != 950 {
		t.Fatalf("rollback balance=%d err=%v", b, e)
	}
	req.RequestId = "terminal-race"
	if _, err := c.reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := c.usage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50}); err != nil {
		t.Fatal(err)
	}
	if _, err := bad.settle(ctx, req.RequestId); err == nil {
		t.Fatal("expected terminal rollback")
	}
	if b, e := c.balance(ctx, 103); e != nil || b != 850 {
		t.Fatalf("terminal rollback balance=%d err=%v", b, e)
	}
	var won [2]bool
	var errs [2]error
	var race sync.WaitGroup
	race.Add(2)
	go func() { defer race.Done(); won[0], errs[0] = c.settle(ctx, req.RequestId) }()
	go func() { defer race.Done(); won[1], errs[1] = c.reverse(ctx, req.RequestId) }()
	race.Wait()
	if errs[0] != nil || errs[1] != nil || won[0] == won[1] {
		t.Fatalf("terminal race wins=%v errors=%v", won, errs)
	}
	b, e = c.balance(ctx, 103)
	if e != nil || (b != 900 && b != 950) {
		t.Fatalf("terminal race balance %d %v", b, e)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{req.RequestId + ":settle", req.RequestId + ":reverse"}).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("terminal outbox=%d err=%v", rows, err)
	}

	// Two accounts racing for one request ID: unique key must roll back the loser.
	if err := c.seed(104, 1000); err != nil {
		t.Fatal(err)
	}
	if err := c.seed(105, 1000); err != nil {
		t.Fatal(err)
	}
	var cross [2]error
	var crossRes [2]*billingv1.ReserveResponse
	var crossWG sync.WaitGroup
	crossWG.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer crossWG.Done()
			crossRes[i], cross[i] = c.reserve(ctx, &billingv1.ReserveRequest{RequestId: "cross-tenant-race", TenantId: int64(104 + i), ApiKeyId: 1, Model: "comparison-mock", EstPromptTokens: 84})
		}(i)
	}
	crossWG.Wait()
	good := 0
	for i := 0; i < 2; i++ {
		if cross[i] == nil && crossRes[i].Code == billingv1.ReserveCode_RESERVE_OK {
			good++
		}
	}
	if good != 1 {
		t.Fatalf("cross tenant successful reservations=%d errors=%v", good, cross)
	}
	b1, e1 := c.balance(ctx, 104)
	b2, e2 := c.balance(ctx, 105)
	if e1 != nil || e2 != nil || b1+b2 != 1900 {
		t.Fatalf("cross balances=%d,%d errors=%v,%v", b1, b2, e1, e2)
	}
	t.Logf("cross-tenant race error outcomes: %v", cross)
	if err := c.audit(ctx); err != nil {
		t.Fatal(err)
	}
}
