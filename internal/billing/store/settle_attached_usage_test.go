package store

import (
	"context"
	"testing"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

func TestSettleWritesAttachedUsageWithoutUsageEvent(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := controlplane.WithSettleUsage(context.Background(), controlplane.SettleUsage{
		Prompt: 50, Completion: 100,
	})
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-attached", tenantID, 84)
	if _, err := s.Reserve(context.Background(), req); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	resp, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if resp.DeltaQuota != 50 || resp.BalanceAfter != 850 {
		t.Fatalf("settle = %+v, want delta 50 balance 850", resp)
	}
	var row Bill
	if err := s.db.Where("request_id = ? AND phase = ?", req.RequestId, PhaseReserve).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != StatusSettled || row.UsageReportedAt == nil || row.PromptTokens == nil || *row.PromptTokens != 50 || row.CompletionTokens == nil || *row.CompletionTokens != 100 {
		t.Fatalf("cash reserve row = %+v", row)
	}
	var settled Bill
	if err := s.db.Where("request_id = ? AND phase = ?", req.RequestId, PhaseSettle).First(&settled).Error; err != nil {
		t.Fatal(err)
	}
	if settled.PromptTokens == nil || *settled.PromptTokens != 50 || settled.CompletionTokens == nil || *settled.CompletionTokens != 100 || settled.ActualQuota == nil || *settled.ActualQuota != 150 {
		t.Fatalf("settle row = %+v", settled)
	}
	var keys []string
	if err := s.db.Model(&OutboxEvent{}).Where("event_key LIKE ?", req.RequestId+":%").Order("event_key").Pluck("event_key", &keys).Error; err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != req.RequestId+":reserve" || keys[1] != req.RequestId+":settle" {
		t.Fatalf("outbox keys = %v", keys)
	}
	var tenant Tenant
	if err := s.db.First(&tenant, tenantID).Error; err != nil {
		t.Fatal(err)
	}
	if tenant.LedgerBalance != 850 {
		t.Fatalf("ledger balance = %d, want 850", tenant.LedgerBalance)
	}
}

func TestSettleWithoutUsageStillFails(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	seedMockModel(t, s)
	tenantID := seedTenant(t, s, 1000)
	req := reserveReq("rid-no-usage", tenantID, 84)
	if _, err := s.Reserve(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(context.Background(), &billingv1.SettleRequest{RequestId: req.RequestId}); err == nil {
		t.Fatal("settle without usage succeeded")
	}
}
