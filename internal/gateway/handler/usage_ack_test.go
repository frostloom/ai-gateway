package handler

import (
	"context"
	"log/slog"
	"testing"

	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type usageAckBilling struct {
	*fakeBilling
	reply *billingv1.ReportUsageResponse
}

func (b *usageAckBilling) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	if b.reply == nil || !b.reply.Accepted {
		if usage, ok := controlplane.SettleUsageFrom(ctx); ok {
			b.reports = append(b.reports, &billingv1.ReportUsageRequest{
				RequestId: req.RequestId, BillId: req.BillId,
				PromptTokens: usage.Prompt, CompletionTokens: usage.Completion, ProviderId: usage.Provider,
			})
		}
		return &billingv1.SettleResponse{}, nil
	}
	return b.fakeBilling.Settle(ctx, req)
}

func TestUsageMustBeAcknowledgedBeforeSettle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reply   *billingv1.ReportUsageResponse
		settles int
	}{
		{"accepted", &billingv1.ReportUsageResponse{Accepted: true}, 1},
		{"rejected", &billingv1.ReportUsageResponse{Accepted: false}, 0},
		{"missing", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &usageAckBilling{fakeBilling: &fakeBilling{}, reply: tc.reply}
			c, _ := newGinCtx()
			settleAndLog(c, b, slog.New(slog.DiscardHandler), "request", 1, 4, 2, 3)
			if len(b.reports) != 1 || len(b.settles) != tc.settles || len(b.reverses) != 0 {
				t.Fatalf("reports=%d settles=%d reverses=%d", len(b.reports), len(b.settles), len(b.reverses))
			}
		})
	}
}
