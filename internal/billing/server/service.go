// Package server billing 计费服务的 gRPC 控制面实现。
package server

import (
	"context"
	"encoding/hex"
	"log/slog"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/pkg/controlplane"
	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type Service struct {
	billingv1.UnimplementedBillingServiceServer
	store            *store.Store
	log              *slog.Logger
	requireReserveV2 bool
}

type Option func(*Service)

// RequireReserveV2 rejects legacy reservations before any store mutation.
func RequireReserveV2() Option {
	return func(s *Service) { s.requireReserveV2 = true }
}

func NewService(st *store.Store, log *slog.Logger, options ...Option) *Service {
	s := &Service{store: st, log: log}
	for _, option := range options {
		option(s)
	}
	return s
}

// Reserve 预占额度（幂等）。
func (s *Service) Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	modes := md.Get(controlplane.ReserveModeKey)
	strict := len(modes) == 1 && (modes[0] == controlplane.ReserveReplayV1 || modes[0] == controlplane.ReserveReplayV2)
	if len(modes) > 0 && !strict {
		return nil, status.Error(codes.InvalidArgument, "unsupported reserve contract")
	}
	if s.requireReserveV2 && (!strict || modes[0] != controlplane.ReserveReplayV2) {
		return nil, status.Error(codes.FailedPrecondition, "reserve content contract required")
	}
	if (!strict || modes[0] != controlplane.ReserveReplayV2) && len(md.Get(controlplane.ReserveContentKey)) > 0 {
		return nil, status.Error(codes.InvalidArgument, "unexpected reserve content digest")
	}
	if len(modes) == 1 && modes[0] == controlplane.ReserveReplayV2 {
		values := md.Get(controlplane.ReserveContentKey)
		if len(values) != 1 || len(values[0]) != 64 {
			return nil, status.Error(codes.InvalidArgument, "invalid reserve content digest")
		}
		decoded, err := hex.DecodeString(values[0])
		if err != nil || hex.EncodeToString(decoded) != values[0] {
			return nil, status.Error(codes.InvalidArgument, "invalid reserve content digest")
		}
		ctx = controlplane.WithReserveContent(ctx, values[0])
	}
	var resp *billingv1.ReserveResponse
	var err error
	if strict {
		if err = grpc.SetHeader(ctx, metadata.Pairs(controlplane.ReserveContractKey, modes[0])); err != nil {
			return nil, status.Error(codes.Unavailable, "reserve replay negotiation unavailable")
		}
		resp, err = s.store.ReserveWithReplaySignal(ctx, req)
	} else {
		resp, err = s.store.Reserve(ctx, req)
	}
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

// Settle 结算（幂等）。网关把实测用量放在 metadata 里，和结算同一次调用。
func (s *Service) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	usageCtx, err := settleUsageContext(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := s.store.Settle(usageCtx, req)
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

func settleUsageContext(ctx context.Context) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	prompt := md.Get(controlplane.SettlePromptKey)
	completion := md.Get(controlplane.SettleCompletionKey)
	provider := md.Get(controlplane.SettleProviderKey)
	if len(prompt) == 0 && len(completion) == 0 && len(provider) == 0 {
		return ctx, nil
	}
	if len(prompt) != 1 || len(completion) != 1 || len(provider) != 1 {
		return nil, status.Error(codes.InvalidArgument, "incomplete settle usage")
	}
	values := [3]int64{}
	for i, raw := range []string{prompt[0], completion[0], provider[0]} {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid settle usage")
		}
		values[i] = n
	}
	return controlplane.WithSettleUsage(ctx, controlplane.SettleUsage{
		Prompt: values[0], Completion: values[1], Provider: values[2],
	}), nil
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
