package store

import (
	"context"
	"testing"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReportUsageAttributesSettlementToSuccessfulProvider(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	a := Provider{Name: "failed-original", BaseURL: "http://unused.invalid", Models: `["*"]`, CostInCent: 1_000_000, CostOutCent: 1_000_000}
	b := Provider{Name: "successful-fallback", BaseURL: "http://unused.invalid", Models: `["*"]`, CostInCent: 2_000_000, CostOutCent: 3_000_000}
	if err := s.UpsertProvider(ctx, &a); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProvider(ctx, &b); err != nil {
		t.Fatal(err)
	}
	req := reserveReq("provider-fallback", tenant, 84)
	req.ProviderId = int64(a.ID)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	usage := &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50, CompletionTokens: 100, ProviderId: int64(b.ID)}
	if err := s.ReportUsage(ctx, usage); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportUsage(ctx, usage); err != nil {
		t.Fatalf("identical usage retry is not idempotent: %v", err)
	}
	legacy := &billingv1.ReportUsageRequest{RequestId: usage.RequestId, PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens}
	if err := s.ReportUsage(ctx, legacy); err != nil {
		t.Fatalf("legacy retry must preserve successful provider: %v", err)
	}
	legacy.PromptTokens++
	if err := s.ReportUsage(ctx, legacy); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("recorded token counts may be overwritten: %v", err)
	}
	if _, err := s.Settle(ctx, &billingv1.SettleRequest{RequestId: req.RequestId}); err != nil {
		t.Fatal(err)
	}
	var row Bill
	if err := s.db.Where("request_id=? AND phase=?", req.RequestId, PhaseSettle).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ProviderID == nil || *row.ProviderID != b.ID {
		t.Fatalf("settlement still attributed to failed provider: %v", row.ProviderID)
	}
	if row.ActualQuota == nil || *row.ActualQuota != 150 {
		t.Fatalf("catalogue sell price changed: %v", row.ActualQuota)
	}
	margin, err := s.ListProvidersWithMargin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range margin {
		if p.ID == a.ID && (p.SellingCent != 0 || p.CostCent != 0) {
			t.Fatalf("failed provider got revenue/cost: %+v", p)
		}
		if p.ID == b.ID && (p.SellingCent != 150 || p.CostCent != 400) {
			t.Fatalf("successful provider margin wrong: %+v", p)
		}
	}
	usage.ProviderId = int64(a.ID)
	if err := s.ReportUsage(ctx, usage); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("settled attribution may be overwritten: %v", err)
	}
}

func TestReportUsageRejectsUnknownProviderWithoutWritingUsage(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	seedMockModel(t, s)
	tenant := seedTenant(t, s, 1000)
	req := reserveReq("unknown-provider", tenant, 84)
	if _, err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{-1, 999999} {
		err := s.ReportUsage(ctx, &billingv1.ReportUsageRequest{RequestId: req.RequestId, PromptTokens: 50, CompletionTokens: 100, ProviderId: id})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid provider %d accepted: %v", id, err)
		}
	}
	row, err := s.getReserve(ctx, req.RequestId)
	if err != nil {
		t.Fatal(err)
	}
	if row.UsageReportedAt != nil {
		t.Fatal("invalid provider created usage marker")
	}
}
