package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func countOutboxRows(s *Store, where string, args ...any) int64 {
	var n int64
	s.db.Model(&OutboxEvent{}).Where(where, args...).Count(&n)
	return n
}

// 生命周期：insert → poll → mark sent / bump attempts 超限 / DLQ，验证 PollOutbox 的过滤语义。
func TestOutboxLifecycle(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	insert := func(key, phase string, amount int64) {
		ev := &BillingEvent{
			EventID: key, TenantID: 1, RequestID: "req", Phase: phase,
			AmountCents: amount, Model: "mock-model", Funding: FundingBalance,
			CreatedAt: time.Now(),
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return insertOutbox(tx, ev)
		}); err != nil {
			t.Fatalf("insert outbox: %v", err)
		}
	}
	insert("r1:reserve", PhaseReserve, 100)
	insert("r2:reserve", PhaseReserve, 100)

	// 初始：两行都未投、可被 poll。
	rows, err := s.PollOutbox(ctx, 10, 5)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("polled = %d, want 2", len(rows))
	}

	// 投递成功 → sent_at 置位 → 不再被 poll。
	if err := s.MarkOutboxSent(ctx, rows[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	rows2, _ := s.PollOutbox(ctx, 10, 5)
	if len(rows2) != 1 {
		t.Fatalf("after sent polled = %d, want 1", len(rows2))
	}

	// 投递失败 bump 到上限（attempts >= max）→ 不再被 poll。
	for i := 0; i < 5; i++ {
		if err := s.BumpOutboxAttempt(ctx, rows[1].ID, "boom"); err != nil {
			t.Fatalf("bump: %v", err)
		}
	}
	rows3, _ := s.PollOutbox(ctx, 10, 5)
	if len(rows3) != 0 {
		t.Fatalf("after attempts max polled = %d, want 0", len(rows3))
	}

	// DLQ：topic 改 dlq + sent_at 置位。
	if err := s.MarkOutboxDLQ(ctx, rows[1].ID, "boom"); err != nil {
		t.Fatalf("mark dlq: %v", err)
	}
	var dlqRow OutboxEvent
	if err := s.db.First(&dlqRow, rows[1].ID).Error; err != nil {
		t.Fatalf("get dlq row: %v", err)
	}
	if dlqRow.Topic != TopicBillingDLQ {
		t.Fatalf("topic = %s, want %s", dlqRow.Topic, TopicBillingDLQ)
	}
	if dlqRow.SentAt == nil {
		t.Fatalf("sent_at should be set after DLQ mark")
	}
	if n := countOutboxRows(s, "topic = ?", TopicBillingDLQ); n != 1 {
		t.Fatalf("dlq rows = %d, want 1", n)
	}
}

// 幂等：同一 request_id 重放 Reserve 只落一行 outbox（uk_event_key 收敛，不重复发事件）。
func TestReserveOutboxIdempotent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-outbox-dup", tenantID, 84) // pre=100

	for i := 0; i < 3; i++ {
		if _, err := s.Reserve(ctx, req); err != nil {
			t.Fatalf("reserve#%d: %v", i+1, err)
		}
	}
	if n := countOutboxRows(s, "event_key = ?", "rid-outbox-dup:reserve"); n != 1 {
		t.Fatalf("outbox rows = %d, want 1（幂等重放只落一行事件）", n)
	}
	// payload 自包含且金额 = pre = 100。
	var row OutboxEvent
	if err := s.db.Where("event_key = ?", "rid-outbox-dup:reserve").First(&row).Error; err != nil {
		t.Fatalf("get outbox row: %v", err)
	}
	var ev BillingEvent
	if err := json.Unmarshal([]byte(row.Payload), &ev); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if ev.Phase != PhaseReserve || ev.AmountCents != 100 || ev.TenantID != tenantID || ev.Model != "mock-model" {
		t.Fatalf("payload wrong: %+v", ev)
	}
}

// 全流程：reserve + usage + settle → 3 个 outbox 事件，phase 正确、金额 = 预扣/0/delta。
func TestFullFlowOutboxEvents(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()

	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-outbox-flow", tenantID, 84) // pre=100
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{
		RequestId: req.RequestId, PromptTokens: 50, CompletionTokens: 100,
	}); err != nil {
		t.Fatalf("report usage: %v", err)
	}
	resp, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if resp.DeltaQuota != 50 {
		t.Fatalf("delta = %d, want 50", resp.DeltaQuota)
	}

	if n := countOutboxRows(s, "event_key LIKE ?", "rid-outbox-flow:%"); n != 3 {
		t.Fatalf("outbox rows = %d, want 3 (reserve+usage+settle)", n)
	}
	var rows []OutboxEvent
	if err := s.db.Where("event_key LIKE ?", "rid-outbox-flow:%").Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	got := map[string]int64{}
	for _, r := range rows {
		var ev BillingEvent
		if err := json.Unmarshal([]byte(r.Payload), &ev); err != nil {
			t.Fatalf("unmarshal %s: %v", r.EventKey, err)
		}
		got[ev.Phase] = ev.AmountCents
	}
	for phase, wantAmt := range map[string]int64{PhaseReserve: 100, PhaseUsage: 0, PhaseSettle: 50} {
		if got[phase] != wantAmt {
			t.Fatalf("phase %s amount = %d, want %d（全量 %+v）", phase, got[phase], wantAmt, got)
		}
	}
}
