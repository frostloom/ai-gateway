package comparison

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"errors"
	"github.com/frostloom/ai-gateway/internal/billing/store"
	pb "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	mysqldriver "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMySQLCandidateReserveIntentAtomic(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("requires test overlay")
	}
	db, r := isolatedDB(t)
	db.Logger = db.Logger.LogMode(logger.Silent)
	_ = r
	c := candidate{db: db}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("mysql-intent-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&candidateAccount{ID: tenant.ID, Initial: 500, Balance: 500}).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("intent-owner"))
	c.intent = &candidateIntent{ownerHash: hex.EncodeToString(h[:]), lease: 30 * time.Second}
	req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
	failed := c
	failed.fail = true
	if _, err := failed.reserve(ctx, req); err == nil {
		t.Fatal("expected rollback")
	}
	var n, e int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&e).Error; err != nil {
		t.Fatal(err)
	}
	balance, err := c.balance(ctx, tenant.ID)
	if err != nil || n != 0 || e != 0 || balance != 500 {
		t.Fatalf("rollback bills=%d events=%d balance=%d err=%v", n, e, balance, err)
	}
	first, err := c.reserve(ctx, req)
	if err != nil || first.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(first, err)
	}
	replay, err := c.reserve(ctx, req)
	if err != nil || replay.Code != pb.ReserveCode_RESERVE_DUPLICATE || replay.BillId != first.BillId {
		t.Fatal(replay, err)
	}
	changed := *req
	changed.EstPromptTokens++
	if _, err := c.reserve(ctx, &changed); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflict=%v", err)
	}
	var state struct {
		State string
		Owner string
		Live  bool
	}
	if err := db.Raw("SELECT execution_state AS state,execution_owner_hash AS owner,execution_lease_until>CURRENT_TIMESTAMP(6) AS live FROM bills WHERE id=?", first.BillId).Scan(&state).Error; err != nil || state.State != "reserved" || state.Owner != c.intent.ownerHash || !state.Live {
		t.Fatal(state, err)
	}
	gate := &capabilityServer{db: db, recoverySecret: "synthetic-recovery"}
	bad, err := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|wrong"))
	if err != nil || bad.Value != 0 {
		t.Fatal(bad, err)
	}
	good, err := gate.Transition(ctx, wrapperspb.String(id+"|forwarding|intent-owner"))
	if err != nil || good.Value != 1 {
		t.Fatal(good, err)
	}
	if err := c.usage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	done, err := c.settle(ctx, id)
	if err != nil || !done {
		t.Fatal(done, err)
	}
	balance, err = c.balance(ctx, tenant.ID)
	ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || e2 != nil || balance != 460 || ledger != balance {
		t.Fatal(balance, ledger, err, e2)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key IN ?", []string{id + ":reserve", id + ":usage", id + ":settle", id + ":reverse"}).Count(&e).Error; err != nil || e != 3 {
		t.Fatal(e, err)
	}
	// An intent alone must not authorize refund after forwarding.
	secondReq := *req
	secondReq.RequestId = id + "-forwarding"
	second, err := c.reserve(ctx, &secondReq)
	if err != nil || second.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(second, err)
	}
	permit, err := gate.Transition(ctx, wrapperspb.String(secondReq.RequestId+"|forwarding|intent-owner"))
	if err != nil || permit.Value != 1 {
		t.Fatal(permit, err)
	}
	// A strict service must not let a caller omit an intent to bypass guards.
	strictService := candidate{db: db, strict: true}
	if _, err := strictService.reverse(ctx, secondReq.RequestId); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("strict service missing intent bypassed reverse guard: %v", err)
	}
	legacyReq := *req
	legacyReq.RequestId = id + "-legacy"
	if _, err := strictService.reserve(ctx, &legacyReq); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("strict reserve without intent=%v", err)
	}
	var legacyBills, legacyEvents int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", legacyReq.RequestId).Count(&legacyBills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", legacyReq.RequestId+":reserve").Count(&legacyEvents).Error; err != nil || legacyBills != 0 || legacyEvents != 0 {
		t.Fatal(legacyBills, legacyEvents, err)
	}
	reversed, err := c.reverse(ctx, secondReq.RequestId)
	if status.Code(err) != codes.FailedPrecondition || reversed {
		t.Fatalf("expected forwarding reverse rejected: %v %v", reversed, err)
	}
	if err := c.usage(ctx, &pb.ReportUsageRequest{RequestId: secondReq.RequestId, PromptTokens: 40}); err != nil {
		t.Fatal(err)
	}
	settled, err := c.settle(ctx, secondReq.RequestId)
	if err != nil || !settled {
		t.Fatal(settled, err)
	}
	var row store.Bill
	if err := db.Where("request_id=? AND phase=?", secondReq.RequestId, store.PhaseReserve).First(&row).Error; err != nil || row.Status != store.StatusSettled {
		t.Fatal(row.Status, err)
	}
	balance, err = c.balance(ctx, tenant.ID)
	ledger, e2 = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || e2 != nil || balance != 420 || ledger != 420 {
		t.Fatal(balance, ledger, err, e2)
	}
	var reverseEvents int64
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", secondReq.RequestId+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 0 {
		t.Fatal(reverseEvents, err)
	}
	thirdReq := *req
	thirdReq.RequestId = id + "-reversing"
	third, err := c.reserve(ctx, &thirdReq)
	if err != nil || third.Code != pb.ReserveCode_RESERVE_OK {
		t.Fatal(third, err)
	}
	if err := db.Exec("UPDATE bills SET execution_lease_until=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id=?", third.BillId).Error; err != nil {
		t.Fatal(err)
	}
	refundPermit, err := gate.Transition(ctx, wrapperspb.String(thirdReq.RequestId+"|reversing|synthetic-recovery"))
	if err != nil || refundPermit.Value != 1 {
		t.Fatal(refundPermit, err)
	}
	if err := c.usage(ctx, &pb.ReportUsageRequest{RequestId: thirdReq.RequestId, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("usage after reversing=%v", err)
	}
	for i := 0; i < 2; i++ {
		ok, err := c.reverse(ctx, thirdReq.RequestId)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", thirdReq.RequestId+":reverse").Count(&reverseEvents).Error; err != nil || reverseEvents != 1 {
		t.Fatal(reverseEvents, err)
	}
	balance, err = c.balance(ctx, tenant.ID)
	ledger, e2 = store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || e2 != nil || balance != 420 || ledger != 420 {
		t.Fatal(balance, ledger, err, e2)
	}
	t.Logf("rollback=true replay=duplicate conflict=rejected wrong_owner=0 owner=1 forwarding_reverse=rejected late_usage=settled balance=%d ledger=%d reverse_events=%d", balance, ledger, reverseEvents)
}

func TestMySQLIntentOrphanOutboxDuplicateFailsClosed(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("requires test overlay")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db, strict: true}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("orphan-outbox-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&candidateAccount{ID: tenant.ID, Initial: 500, Balance: 500}).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&store.OutboxEvent{Topic: "test", EventKey: id + ":reserve", Payload: "{}", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("owner"))
	c.intent = &candidateIntent{ownerHash: hex.EncodeToString(h[:]), lease: time.Minute}
	out, err := c.reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
	var mysqlErr *mysqldriver.MySQLError
	if out != nil || !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		t.Fatalf("want raw outbox conflict, got out=%+v err=%v", out, err)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=?", id).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, err := c.balance(ctx, tenant.ID)
	ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || e2 != nil || bills != 0 || events != 1 || live != 500 || ledger != 500 {
		t.Fatal(bills, events, live, ledger, err, e2)
	}
	t.Logf("outbox_duplicate=error bills=%d preexisting_events=%d balance=%d ledger=%d", bills, events, live, ledger)
}

func TestMySQLIntentCrossTenantConflict(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("requires test overlay")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db, strict: true}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("cross-intent-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	tenants := make([]store.Tenant, 2)
	for i := range tenants {
		tenants[i] = store.Tenant{Name: fmt.Sprintf("%s-%d", id, i), InitialQuota: 500}
		if err := db.Create(&tenants[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&candidateAccount{ID: tenants[i].ID, Initial: 500, Balance: 500}).Error; err != nil {
			t.Fatal(err)
		}
	}
	h := sha256.Sum256([]byte("owner"))
	c.intent = &candidateIntent{ownerHash: hex.EncodeToString(h[:]), lease: 30 * time.Second}
	var arrived atomic.Int32
	ready := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	if err := db.Callback().Create().Before("gorm:create").Register("test:cross-intent-barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "bills" {
			if arrived.Add(1) == 2 {
				once.Do(func() { close(ready) })
			}
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("test:cross-intent-barrier")
	type result struct {
		response *pb.ReserveResponse
		err      error
	}
	results := make(chan result, 2)
	for _, tenant := range tenants {
		go func(tenant store.Tenant) {
			out, err := c.reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84})
			results <- result{out, err}
		}(tenant)
	}
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("both transactions did not reach insert")
	}
	close(release)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		res := <-results
		if res.err == nil && res.response.Code == pb.ReserveCode_RESERVE_OK {
			success++
		} else if status.Code(res.err) == codes.FailedPrecondition {
			conflict++
		} else {
			t.Errorf("unexpected result: %+v err=%v", res.response, res.err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Errorf("success=%d conflict=%d", success, conflict)
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, tenant := range tenants {
		live, err := c.balance(ctx, tenant.ID)
		ledger, e := store.LedgerBalance(ctx, db, tenant.ID)
		if err != nil || e != nil || live != ledger {
			t.Fatal(live, ledger, err, e)
		}
		total += live
	}
	if bills != 1 || events != 1 || total != 900 {
		t.Fatalf("bills=%d events=%d balance_sum=%d", bills, events, total)
	}
	t.Logf("success=%d conflict=%d bills=%d events=%d balance_sum=%d", success, conflict, bills, events, total)
}

func TestMySQLIntentCommitAckLoss(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("requires test overlay")
	}
	for _, injected := range []error{errCommitAckLost, driver.ErrBadConn} {
		t.Run(injected.Error(), func(t *testing.T) {
			db, r := isolatedDB(t)
			_ = r
			db.Logger = db.Logger.LogMode(logger.Silent)
			stable := candidate{db: db}
			if err := stable.migrate(); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			id := fmt.Sprintf("mysql-intent-ack-%d", time.Now().UnixNano())
			tenant := store.Tenant{Name: id, InitialQuota: 500}
			if err := db.Create(&tenant).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&candidateAccount{ID: tenant.ID, Initial: 500, Balance: 500}).Error; err != nil {
				t.Fatal(err)
			}
			model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
			if err := db.Create(&model).Error; err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256([]byte("ack-owner"))
			stable.intent = &candidateIntent{ownerHash: hex.EncodeToString(h[:]), lease: 30 * time.Second}
			broken := candidate{db: recoveryDBWithCommitError(t, injected), intent: stable.intent}
			req := &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
			if _, err := broken.reserve(ctx, req); err == nil {
				t.Fatal("expected simulated lost acknowledgement")
			}
			var row store.Bill
			if err := db.Where("request_id=? AND phase=?", id, store.PhaseReserve).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			replay, err := stable.reserve(ctx, req)
			if err != nil || replay.Code != pb.ReserveCode_RESERVE_DUPLICATE || replay.BillId != int64(row.ID) {
				t.Fatal(replay, err)
			}
			var state struct{ State, Owner string }
			if err := db.Raw("SELECT execution_state AS state, execution_owner_hash AS owner FROM bills WHERE id=?", row.ID).Scan(&state).Error; err != nil || state.State != "reserved" || state.Owner != stable.intent.ownerHash {
				t.Fatal(state, err)
			}
			var bills, events int64
			if err := db.Model(&store.Bill{}).Where("request_id=? AND phase=?", id, store.PhaseReserve).Count(&bills).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":reserve").Count(&events).Error; err != nil {
				t.Fatal(err)
			}
			live, err := stable.balance(ctx, tenant.ID)
			ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
			if err != nil || e2 != nil || bills != 1 || events != 1 || live != 400 || ledger != 400 {
				t.Fatal(bills, events, live, ledger, err, e2)
			}
			t.Logf("injected=%v commit_visible=true retry=duplicate state=reserved owner_preserved=true bills=%d events=%d balance=%d ledger=%d", injected, bills, events, live, ledger)
		})
	}
}

func TestMySQLIntentInvalidRejectedBeforeReserve(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("requires test overlay")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db, strict: true}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("invalid-intent-%d", time.Now().UnixNano())
	tenant := store.Tenant{Name: id, InitialQuota: 500}
	if err := db.Create(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&candidateAccount{ID: tenant.ID, Initial: 500, Balance: 500}).Error; err != nil {
		t.Fatal(err)
	}
	model := store.Model{Vendor: "test", ModelID: id, Name: id, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("owner"))
	valid := hex.EncodeToString(h[:])
	for i, intent := range []candidateIntent{{ownerHash: "bad", lease: time.Second}, {ownerHash: valid, lease: 0}, {ownerHash: valid, lease: time.Hour + time.Second}} {
		c.intent = &intent
		req := &pb.ReserveRequest{RequestId: fmt.Sprintf("%s-%d", id, i), TenantId: int64(tenant.ID), ApiKeyId: 1, Model: id, EstPromptTokens: 84}
		if _, err := c.reserve(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("invalid %d accepted or wrong error: %v", i, err)
		}
	}
	var bills, events int64
	if err := db.Model(&store.Bill{}).Where("tenant_id=?", tenant.ID).Count(&bills).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&store.OutboxEvent{}).Where("event_key LIKE ?", id+"-%").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	live, err := c.balance(ctx, tenant.ID)
	ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
	if err != nil || e2 != nil || bills != 0 || events != 0 || live != 500 || ledger != 500 {
		t.Fatalf("bills=%d events=%d live=%d ledger=%d err=%v/%v", bills, events, live, ledger, err, e2)
	}
	t.Log("invalid_intents=3 bills=0 events=0 balance=ledger=500")
}

// Competes synthetic state transitions; this is not production authorization.
func TestMySQLCandidateForwardReverseRace(t *testing.T) {
	if os.Getenv("TEST_EXECUTION_STRICT") != "1" {
		t.Skip("requires test overlay")
	}
	db, r := isolatedDB(t)
	_ = r
	db.Logger = db.Logger.LogMode(logger.Silent)
	c := candidate{db: db}
	if err := c.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := fmt.Sprintf("mysql-race-%d", time.Now().UnixNano())
	model := store.Model{Vendor: "test", ModelID: base, Name: base, InputPriceCent: 1000000, OutputPriceCent: 1000000, Tags: "[]"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("race-owner"))
	c.intent = &candidateIntent{ownerHash: hex.EncodeToString(h[:]), lease: 30 * time.Second}
	gate := &executionServer{db: db}
	wins := map[string]int{}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("%s-%d", base, i)
		tenant := store.Tenant{Name: id, InitialQuota: 500}
		if err := db.Create(&tenant).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&candidateAccount{ID: tenant.ID, Initial: 500, Balance: 500}).Error; err != nil {
			t.Fatal(err)
		}
		bill, err := c.reserve(ctx, &pb.ReserveRequest{RequestId: id, TenantId: int64(tenant.ID), ApiKeyId: 1, Model: base, EstPromptTokens: 84})
		if err != nil || bill.Code != pb.ReserveCode_RESERVE_OK {
			t.Fatal(bill, err)
		}
		type result struct {
			op  string
			n   int64
			err error
		}
		ch := make(chan result, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, op := range []string{"forwarding", "reversing"} {
			wg.Add(1)
			go func(op string) {
				defer wg.Done()
				<-start
				out, e := gate.Transition(ctx, wrapperspb.String(id+"|"+op))
				var n int64
				if out != nil {
					n = out.Value
				}
				ch <- result{op, n, e}
			}(op)
		}
		close(start)
		wg.Wait()
		close(ch)
		winner := ""
		for res := range ch {
			if res.err != nil || (res.n != 0 && res.n != 1) {
				t.Fatal(res)
			}
			if res.n == 1 {
				if winner != "" {
					t.Fatal("two winners")
				}
				winner = res.op
			}
		}
		if winner == "" {
			t.Fatal("no winner")
		}
		wins[winner]++
		if winner == "forwarding" {
			if _, err := c.reverse(ctx, id); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("forwarding reverse=%v", err)
			}
			if err := c.usage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); err != nil {
				t.Fatal(err)
			}
			ok, err := c.settle(ctx, id)
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
		} else {
			if err := c.usage(ctx, &pb.ReportUsageRequest{RequestId: id, PromptTokens: 40}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("reversing usage=%v", err)
			}
			for j := 0; j < 2; j++ {
				ok, err := c.reverse(ctx, id)
				if err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
		}
		live, err := c.balance(ctx, tenant.ID)
		ledger, e2 := store.LedgerBalance(ctx, db, tenant.ID)
		expected := int64(500)
		if winner == "forwarding" {
			expected = 460
		}
		if err != nil || e2 != nil || live != expected || ledger != expected {
			t.Fatal(live, ledger, err, e2)
		}
		for _, phase := range []string{"reserve", "usage", "settle", "reverse"} {
			want := int64(0)
			if phase == "reserve" || (winner == "forwarding" && (phase == "usage" || phase == "settle")) || (winner == "reversing" && phase == "reverse") {
				want = 1
			}
			var n int64
			if err := db.Model(&store.OutboxEvent{}).Where("event_key=?", id+":"+phase).Count(&n).Error; err != nil || n != want {
				t.Fatal(phase, n, want, err)
			}
		}
	}
	t.Logf("contests=12 forwarding=%d reversing=%d balance_and_events=consistent", wins["forwarding"], wins["reversing"])
}
