// Package server billing 计费服务的 gRPC 控制面实现。
package server

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type Service struct {
	billingv1.UnimplementedBillingServiceServer
	store *store.Store
	log   *slog.Logger
}

func NewService(st *store.Store, log *slog.Logger) *Service {
	return &Service{store: st, log: log}
}

// Reserve 预占额度（幂等）。
func (s *Service) Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	resp, err := s.store.Reserve(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log.Info("reserve",
		"request_id", req.RequestId,
		"tenant_id", req.TenantId,
		"model", req.Model,
		"code", resp.Code.String(),
		"balance_after", resp.BalanceAfter,
	)
	return resp, nil
}

// GetBalance 查询租户余额。
func (s *Service) GetBalance(ctx context.Context, req *billingv1.GetBalanceRequest) (*billingv1.GetBalanceResponse, error) {
	bal, err := s.store.GetBalance(ctx, uint64(req.TenantId))
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "get balance: %v", err)
	}
	return &billingv1.GetBalanceResponse{Balance: bal}, nil
}

// ReportUsage 上报实际用量（持久 marker，幂等）。
func (s *Service) ReportUsage(ctx context.Context, req *billingv1.ReportUsageRequest) (*billingv1.ReportUsageResponse, error) {
	if err := s.store.ReportUsage(ctx, req); err != nil {
		return nil, err
	}
	return &billingv1.ReportUsageResponse{Accepted: true}, nil
}

// Settle 结算（幂等）。
func (s *Service) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	resp, err := s.store.Settle(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log.Info("settle", "request_id", req.RequestId, "settled", resp.Settled, "delta_quota", resp.DeltaQuota)
	return resp, nil
}

// Reverse 冲正/退款（幂等）。
func (s *Service) Reverse(ctx context.Context, req *billingv1.ReverseRequest) (*billingv1.ReverseResponse, error) {
	resp, err := s.store.Reverse(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log.Info("reverse", "request_id", req.RequestId, "reversed", resp.Reversed, "refund_quota", resp.RefundQuota, "reason", req.Reason)
	return resp, nil
}

// RebuildBalance 从账本重建余额投影（M4 对账兜底）。
func (s *Service) RebuildBalance(ctx context.Context, req *billingv1.RebuildBalanceRequest) (*billingv1.RebuildBalanceResponse, error) {
	bal, err := s.store.RebuildBalance(ctx, uint64(req.TenantId))
	if err != nil {
		return nil, err
	}
	s.log.Info("rebuild balance", "tenant_id", req.TenantId, "balance", bal)
	return &billingv1.RebuildBalanceResponse{Balance: bal}, nil
}

// ValidateAPIKey 校验 API Key（gateway 不碰 MySQL，密钥验证统一走本服务）。
func (s *Service) ValidateAPIKey(ctx context.Context, req *billingv1.ValidateAPIKeyRequest) (*billingv1.ValidateAPIKeyResponse, error) {
	k, err := s.store.ValidateAPIKey(ctx, req.ApiKeyHash)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "validate key: %v", err)
	}
	if k == nil {
		return &billingv1.ValidateAPIKeyResponse{Valid: false}, nil
	}
	return &billingv1.ValidateAPIKeyResponse{
		Valid:    true,
		ApiKeyId: int64(k.ID),
		TenantId: int64(k.TenantID),
	}, nil
}
