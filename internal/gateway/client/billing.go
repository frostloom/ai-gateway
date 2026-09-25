// Package client gateway 对上游的客户端（billing 控制面 gRPC + router 数据面 HTTP）。
package client

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	billingv1 "github.com/frostloom/ai-gateway/internal/proto/billing/v1"
)

type Billing struct {
	conn *grpc.ClientConn
	cli  billingv1.BillingServiceClient
}

func NewBilling(addr string) (*Billing, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Billing{conn: conn, cli: billingv1.NewBillingServiceClient(conn)}, nil
}

func (b *Billing) Close() error { return b.conn.Close() }

func (b *Billing) ValidateAPIKey(ctx context.Context, keyHash string) (*billingv1.ValidateAPIKeyResponse, error) {
	return b.cli.ValidateAPIKey(ctx, &billingv1.ValidateAPIKeyRequest{ApiKeyHash: keyHash})
}

func (b *Billing) Reserve(ctx context.Context, req *billingv1.ReserveRequest) (*billingv1.ReserveResponse, error) {
	return b.cli.Reserve(ctx, req)
}

func (b *Billing) ReportUsage(ctx context.Context, req *billingv1.ReportUsageRequest) (*billingv1.ReportUsageResponse, error) {
	return b.cli.ReportUsage(ctx, req)
}

func (b *Billing) Settle(ctx context.Context, req *billingv1.SettleRequest) (*billingv1.SettleResponse, error) {
	return b.cli.Settle(ctx, req)
}

func (b *Billing) Reverse(ctx context.Context, req *billingv1.ReverseRequest) (*billingv1.ReverseResponse, error) {
	return b.cli.Reverse(ctx, req)
}

// BackgroundCtx 生成「独立于请求」的超时 ctx：结算/退款绝不复用客户端请求 ctx，
// 否则客户端断连（context canceled）会让计费调用静默丢失。
func BackgroundCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
