package store

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func TestLedgerBatchDefersMySQLAndKeepsRedisDebit(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()

	first := reserveReq("batch-a", tenantID, 84)
	reserved, err := s.Reserve(ctx, first)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if reserved.Code != billingv1.ReserveCode_RESERVE_OK || reserved.BalanceAfter != 900 {
		t.Fatalf("reserve = %+v, want balance 900", reserved)
	}
	if countReserveRows(s) != 0 {
		t.Fatalf("mysql rows = %d, want 0 before flush", countReserveRows(s))
	}

	second := reserveReq("batch-b", tenantID, 84)
	again, err := s.Reserve(ctx, second)
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if again.BalanceAfter != 800 {
		t.Fatalf("second balance = %d, want 800; redis debit was rebuilt", again.BalanceAfter)
	}

	settleCtx := controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100})
	settled, err := s.Settle(settleCtx, &billingv1.SettleRequest{RequestId: first.RequestId, BillId: reserved.BillId})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if settled.DeltaQuota != 50 || settled.BalanceAfter != 750 {
		t.Fatalf("settle = %+v, want delta 50 balance 750", settled)
	}
	if countReserveRows(s) != 0 {
		t.Fatalf("mysql rows = %d, want 0 after settle before flush", countReserveRows(s))
	}

	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if countReserveRows(s) != 2 {
		t.Fatalf("mysql rows = %d, want 2", countReserveRows(s))
	}
	var column int64
	if err := s.db.Model(&Tenant{}).Where("id = ?", tenantID).Select("ledger_balance").Scan(&column).Error; err != nil {
		t.Fatalf("column: %v", err)
	}
	live, err := s.getBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	if column != 750 || live != 750 {
		t.Fatalf("column %d redis %d, want 750", column, live)
	}
	var row Bill
	if err := s.db.Where("request_id = ? AND phase = ?", first.RequestId, PhaseReserve).First(&row).Error; err != nil {
		t.Fatalf("flushed reserve: %v", err)
	}
	if row.Status != StatusSettled || row.UsageReportedAt == nil || row.PromptTokens == nil || *row.PromptTokens != 50 {
		t.Fatalf("flushed reserve = %+v, want settled usage", row)
	}
}

func TestLedgerBatchConcurrentNoOversell(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	seedMockModel(t, s)
	const balance = int64(1000)
	tenantID := seedTenant(t, s, balance)
	ctx := context.Background()

	var wg sync.WaitGroup
	var success, insufficient atomic.Int64
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			defer wg.Done()
			resp, err := s.Reserve(ctx, reserveReq(fmt.Sprintf("batch-race-%d", i), tenantID, 84))
			if err != nil {
				t.Errorf("reserve: %v", err)
				return
			}
			switch resp.Code {
			case billingv1.ReserveCode_RESERVE_OK:
				success.Add(1)
			case billingv1.ReserveCode_RESERVE_INSUFFICIENT:
				insufficient.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if success.Load() != 10 || insufficient.Load() != 90 {
		t.Fatalf("success %d insufficient %d, want 10 and 90", success.Load(), insufficient.Load())
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 0 {
		t.Fatalf("redis balance = %d, want 0", bal)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if n := countReserveRows(s); n != 10 {
		t.Fatalf("reserve rows = %d, want 10", n)
	}
}

func TestLedgerBatchReplayAfterFlushDoesNotDebitAgain(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	req := reserveReq("batch-replay", tenantID, 84)
	reserved, err := s.Reserve(ctx, req)
	if err != nil || reserved.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve: %+v %v", reserved, err)
	}
	settleCtx := controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100})
	if _, err := s.Settle(settleCtx, &billingv1.SettleRequest{RequestId: req.RequestId, BillId: reserved.BillId}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	again, err := s.Reserve(ctx, req)
	if err == nil {
		t.Fatalf("replay after flush returned %+v, want a rejection without another debit", again)
	}
	if bal, _ := s.getBalance(ctx, tenantID); bal != 850 {
		t.Fatalf("redis balance = %d, want 850", bal)
	}
}

type scriptSink struct {
	mu   sync.Mutex
	fail int
}

func (s *scriptSink) PublishLedger(context.Context, []*BillingEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail > 0 {
		s.fail--
		return fmt.Errorf("kafka down")
	}
	return nil
}

func TestLedgerBatchPublishFailureFallsBackToOutbox(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	sink := &scriptSink{fail: 8}
	s.AttachLedgerSink(sink)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	reserved, err := s.Reserve(ctx, reserveReq("batch-kafka-down", tenantID, 84))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := s.Settle(controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100}), &billingv1.SettleRequest{RequestId: "batch-kafka-down", BillId: reserved.BillId}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := s.batch.drainUnpublished(ctx, s.db); err != nil {
		t.Fatalf("drain: %v", err)
	}
	var outbox int64
	if err := s.db.Model(&OutboxEvent{}).Count(&outbox).Error; err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if outbox < 1 || countReserveRows(s) != 1 || countBillRows(s) != 2 {
		t.Fatalf("outbox %d reserve rows %d bill rows %d, want fallback events and both bills", outbox, countReserveRows(s), countBillRows(s))
	}
}

func TestLedgerBatchKafkaPublishSkipsOutbox(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	sink := &recordingSink{}
	s.AttachLedgerSink(sink)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	reserved, err := s.Reserve(ctx, reserveReq("batch-kafka-up", tenantID, 84))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := s.Settle(controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100}), &billingv1.SettleRequest{RequestId: "batch-kafka-up", BillId: reserved.BillId}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	waitRequestLogs(t, s, sink, 2)
	var outbox int64
	if err := s.db.Model(&OutboxEvent{}).Count(&outbox).Error; err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if outbox != 0 || len(sink.snapshot()) < 2 || countReserveRows(s) != 1 || countBillRows(s) != 2 {
		t.Fatalf("outbox %d events %d reserve rows %d bill rows %d, want kafka events and both bills", outbox, len(sink.snapshot()), countReserveRows(s), countBillRows(s))
	}
}

type recordingSink struct {
	mu     sync.Mutex
	events []*BillingEvent
}

func (s *recordingSink) PublishLedger(_ context.Context, events []*BillingEvent) error {
	s.mu.Lock()
	s.events = append(s.events, events...)
	s.mu.Unlock()
	return nil
}

func (s *recordingSink) snapshot() []*BillingEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*BillingEvent, len(s.events))
	copy(out, s.events)
	return out
}

func waitRequestLogs(t *testing.T, s *Store, sink *recordingSink, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := s.batch.drainUnpublished(context.Background(), s.db); err != nil {
			t.Fatalf("drain: %v", err)
		}
		if got := len(sink.snapshot()); got >= n || time.Now().After(deadline) {
			if got < n {
				t.Fatalf("events %d, want %d", got, n)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLedgerBatchLogReachesKafkaBeforeBalanceFlush(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	sink := &recordingSink{}
	s.AttachLedgerSink(sink)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	reserved, err := s.Reserve(ctx, reserveReq("batch-log-before-flush", tenantID, 84))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := s.Settle(controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100}), &billingv1.SettleRequest{RequestId: "batch-log-before-flush", BillId: reserved.BillId}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	waitRequestLogs(t, s, sink, 2)
	if len(sink.snapshot()) != 2 || countReserveRows(s) != 0 {
		t.Fatalf("events %d rows %d, want both logs before the balance flush", len(sink.snapshot()), countReserveRows(s))
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := s.batch.drainUnpublished(ctx, s.db); err != nil {
		t.Fatalf("drain after flush: %v", err)
	}
	var column int64
	if err := s.db.Model(&Tenant{}).Where("id = ?", tenantID).Select("ledger_balance").Scan(&column).Error; err != nil {
		t.Fatalf("column: %v", err)
	}
	if len(sink.snapshot()) != 2 || countReserveRows(s) != 1 || column != 850 {
		t.Fatalf("events %d reserve rows %d column %d, want one log pair and the 5s balance write", len(sink.snapshot()), countReserveRows(s), column)
	}
}

func TestLedgerBatchRestartKeepsLowerRedisBalance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	if err := s.rdb.Set(ctx, BalanceKey(tenantID), 850, 0).Err(); err != nil {
		t.Fatalf("set redis: %v", err)
	}
	if err := s.requireCashMatch(ctx, tenantID); err != nil {
		t.Fatalf("match: %v", err)
	}
	live, err := s.getBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	if live != 850 {
		t.Fatalf("redis = %d, want 850 kept after the unflushed window", live)
	}
}

func TestLedgerBatchKeepsHigherRedisBalance(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	if err := s.rdb.Set(ctx, BalanceKey(tenantID), 1100, 0).Err(); err != nil {
		t.Fatalf("set redis: %v", err)
	}
	if err := s.requireCashMatch(ctx, tenantID); err != nil {
		t.Fatalf("match: %v", err)
	}
	live, err := s.getBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	if live != 1100 {
		t.Fatalf("redis = %d, want 1100 kept", live)
	}
}

func TestLedgerQueueConsumerWritesMySQLOnce(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	s.EnableLedgerQueue()
	sink := &recordingSink{}
	s.AttachLedgerSink(sink)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	first := reserveReq("queue-a", tenantID, 84)
	reserved, err := s.Reserve(ctx, first)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	second := reserveReq("queue-b", tenantID, 84)
	if _, err := s.Reserve(ctx, second); err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if _, err := s.Settle(controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100}), &billingv1.SettleRequest{RequestId: first.RequestId, BillId: reserved.BillId}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if countReserveRows(s) != 0 {
		t.Fatalf("mysql rows = %d, want 0 until the consumer applies the queue", countReserveRows(s))
	}
	live, err := s.getBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	if live != 750 {
		t.Fatalf("redis = %d, want 750 before mysql apply", live)
	}
	if len(sink.events) < 3 {
		t.Fatalf("queued events = %d, want reserve, settle, and the second reserve", len(sink.events))
	}
	if err := s.ApplyLedgerEvents(ctx, sink.events); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := s.ApplyLedgerEvents(ctx, sink.events); err != nil {
		t.Fatalf("apply again: %v", err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("ack: %v", err)
	}
	var column int64
	if err := s.db.Model(&Tenant{}).Where("id = ?", tenantID).Select("ledger_balance").Scan(&column).Error; err != nil {
		t.Fatalf("column: %v", err)
	}
	if column != 750 || countReserveRows(s) != 2 {
		t.Fatalf("column %d rows %d, want 750 and 2", column, countReserveRows(s))
	}
	again, err := s.getBalance(ctx, tenantID)
	if err != nil || again != 750 {
		t.Fatalf("redis after apply = %d (%v), want 750", again, err)
	}
}

func TestLedgerQueueReverseRefundsRedisOnce(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	s.EnableLedgerQueue()
	sink := &recordingSink{}
	s.AttachLedgerSink(sink)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	reserved, err := s.Reserve(ctx, reserveReq("queue-reverse", tenantID, 84))
	if err != nil || reserved.Code != billingv1.ReserveCode_RESERVE_OK {
		t.Fatalf("reserve: %+v %v", reserved, err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	reversed, err := s.Reverse(ctx, &billingv1.ReverseRequest{RequestId: "queue-reverse", BillId: reserved.BillId, Reason: "timeout"})
	if err != nil || !reversed.Reversed || reversed.RefundQuota != 100 {
		t.Fatalf("reverse = %+v %v, want refund 100", reversed, err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush reverse: %v", err)
	}
	live, err := s.getBalance(ctx, tenantID)
	if err != nil || live != 900 {
		t.Fatalf("redis = %d (%v), want 900 until the consumer refunds", live, err)
	}
	forward := append([]*BillingEvent{}, sink.events...)
	backward := append([]*BillingEvent{}, sink.events...)
	for i, j := 0, len(backward)-1; i < j; i, j = i+1, j-1 {
		backward[i], backward[j] = backward[j], backward[i]
	}
	if err := s.ApplyLedgerEvents(ctx, backward); err != nil {
		t.Fatalf("apply reverse first: %v", err)
	}
	if err := s.ApplyLedgerEvents(ctx, forward); err != nil {
		t.Fatalf("apply again: %v", err)
	}
	var column int64
	if err := s.db.Model(&Tenant{}).Where("id = ?", tenantID).Select("ledger_balance").Scan(&column).Error; err != nil {
		t.Fatalf("column: %v", err)
	}
	if column != 1000 {
		t.Fatalf("column = %d, want 1000", column)
	}
	var row Bill
	if err := s.db.Where("request_id = ? AND phase = ?", "queue-reverse", PhaseReserve).First(&row).Error; err != nil {
		t.Fatalf("reserve row: %v", err)
	}
	if row.Status != StatusReversed {
		t.Fatalf("status = %s, want reversed", row.Status)
	}
	after, err := s.getBalance(ctx, tenantID)
	if err != nil || after != 1000 {
		t.Fatalf("redis = %d (%v), want 1000 after the consumer refund", after, err)
	}
	if err := s.ApplyLedgerEvents(ctx, forward); err != nil {
		t.Fatalf("apply third time: %v", err)
	}
	once, err := s.getBalance(ctx, tenantID)
	if err != nil || once != 1000 {
		t.Fatalf("redis = %d (%v), want 1000 after a repeated refund", once, err)
	}
}

func TestLedgerQueueSettleBeforeReserveDoesNotDoubleCount(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableLedgerBatch(time.Hour)
	s.EnableLedgerQueue()
	sink := &recordingSink{}
	s.AttachLedgerSink(sink)
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	ctx := context.Background()
	reserved, err := s.Reserve(ctx, reserveReq("queue-order", tenantID, 84))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := s.Settle(controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{Prompt: 50, Completion: 100}), &billingv1.SettleRequest{RequestId: "queue-order", BillId: reserved.BillId}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if err := s.FlushLedgerBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	var settleEv, reserveEv *BillingEvent
	for _, ev := range sink.events {
		switch ev.Phase {
		case PhaseSettle:
			settleEv = ev
		case PhaseReserve:
			reserveEv = ev
		}
	}
	if settleEv == nil || reserveEv == nil {
		t.Fatalf("events reserve=%v settle=%v", reserveEv != nil, settleEv != nil)
	}
	if err := s.ApplyLedgerEvents(ctx, []*BillingEvent{settleEv}); err != nil {
		t.Fatalf("settle first: %v", err)
	}
	if err := s.ApplyLedgerEvents(ctx, []*BillingEvent{reserveEv}); err != nil {
		t.Fatalf("reserve second: %v", err)
	}
	var row Bill
	if err := s.db.Where("request_id = ? AND phase = ?", "queue-order", PhaseReserve).First(&row).Error; err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if row.Status != StatusSettled {
		t.Fatalf("reserve status = %s, want settled", row.Status)
	}
	var column int64
	if err := s.db.Model(&Tenant{}).Where("id = ?", tenantID).Select("ledger_balance").Scan(&column).Error; err != nil {
		t.Fatalf("column: %v", err)
	}
	live, err := s.getBalance(ctx, tenantID)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	if column != live || live != 850 {
		t.Fatalf("column %d redis %d, want 850", column, live)
	}
}
